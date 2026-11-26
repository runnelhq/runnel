package sqldb

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
)

func conn(typ string, with map[string]any) connection.Resolved {
	return connection.Resolved{Name: "db", Type: typ, With: with}
}

// The data sources of SQL Server and PostgreSQL: TLS on and checked by default, the
// password escaped, the timeout in seconds.
func TestSource(t *testing.T) {
	reg := connections.Builtin()
	for _, tt := range []struct {
		c      connection.Resolved
		driver string
		want   []string
	}{
		{conn(connections.TypeSQLServer, map[string]any{"host": "db01", "port": 1433, "database": "ERP", "user": "runnel", "password": "p@ss:/?"}),
			"sqlserver", []string{"sqlserver://runnel:p%40ss%3A%2F%3F@db01:1433?", "database=ERP", "encrypt=true", "TrustServerCertificate=false", "connection+timeout=30", "app+name=Runnel"}},
		{conn(connections.TypeSQLServer, map[string]any{"host": "db01", "instance": "SQLEXPRESS", "database": "ERP", "user": "u", "encrypt": "strict", "trustServerCertificate": true, "timeout": "5s"}),
			"sqlserver", []string{"sqlserver://u:@db01/SQLEXPRESS?", "encrypt=strict", "TrustServerCertificate=true", "connection+timeout=5"}},
		{conn(connections.TypePostgres, map[string]any{"host": "pg", "database": "shop", "user": "runnel", "password": "s3cret"}),
			"pgx", []string{"postgres://runnel:s3cret@pg:5432/shop?", "sslmode=verify-full", "connect_timeout=30", "application_name=Runnel"}},
		{conn(connections.TypePostgres, map[string]any{"host": "pg", "port": "6543", "database": "shop", "user": "u", "sslMode": "require"}),
			"pgx", []string{"@pg:6543/shop?", "sslmode=require"}},
	} {
		driver, dsn, err := source(context.Background(), reg, tt.c)
		if err != nil || driver != tt.driver {
			t.Errorf("%v: %s, %v", tt.c.With, driver, err)
			continue
		}
		for _, w := range tt.want {
			if !strings.Contains(dsn, w) {
				t.Errorf("%s: missing %q", dsn, w)
			}
		}
	}
	for c, want := range map[*connection.Resolved]string{
		new(conn(connections.TypeSQLServer, map[string]any{"host": "h", "port": "1433", "instance": "X", "database": "d", "user": "u"})): "give either port or instance, not both",
		new(conn(connections.TypePostgres, map[string]any{"host": "h", "port": "99999", "database": "d", "user": "u"})):                  `invalid port "99999"`,
		new(conn(connections.TypePostgres, map[string]any{"host": "h", "database": "d", "user": "u", "timeout": "10ms"})):                "timeout: invalid duration",
		new(conn(connections.TypePostgres, map[string]any{"host": "h", "database": "d"})):                                                `missing required field "user"`,
		// connectTimeout is timeout since R5b.
		new(conn(connections.TypeSQLServer, map[string]any{"host": "h", "database": "d", "connectTimeout": "5s"})): `field "connectTimeout" was renamed before 1.0: use timeout`,
	} {
		if _, _, err := source(context.Background(), reg, *c); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v, want %q", c.With, err, want)
		}
	}
	// Without a user: the Windows account Runnel runs as – only on Windows.
	_, dsn, err := source(context.Background(), reg, conn(connections.TypeSQLServer, map[string]any{"host": "h", "database": "d"}))
	if runtime.GOOS == "windows" {
		if err != nil || strings.Contains(dsn, "@") {
			t.Errorf("windows authentication: %s, %v", dsn, err)
		}
	} else if err == nil || !strings.Contains(err.Error(), "Windows authentication works only on Windows") {
		t.Errorf("windows authentication elsewhere: %v", err)
	}
}

// PostgreSQL has no named arguments: @name becomes $1, $2 …, the same name the same number.
func TestStatementPostgres(t *testing.T) {
	pg := conn(connections.TypePostgres, nil)
	stmt, args, err := Statement(pg, "SELECT * FROM t WHERE a = @a AND b = @b OR a2 = @a AND x = '@a' -- @b", map[string]string{"a": "1", "b": "2"})
	if err != nil || stmt != "SELECT * FROM t WHERE a = $1 AND b = $2 OR a2 = $1 AND x = '@a' -- @b" || len(args) != 2 || args[0] != "1" || args[1] != "2" {
		t.Errorf("%q %v %v", stmt, args, err)
	}
	// Other databases keep the statement and get named arguments.
	stmt, args, err = Statement(conn(connections.TypeSQLServer, nil), "SELECT @a", map[string]string{"a": "1"})
	if err != nil || stmt != "SELECT @a" || len(args) != 1 {
		t.Errorf("%q %v %v", stmt, args, err)
	}
	if _, _, err := Statement(pg, "SELECT @a", nil); err == nil {
		t.Error("missing parameter")
	}
}

// Values SQL Server returns as bytes: DECIMAL and MONEY as text, UNIQUEIDENTIFIER in
// its byte order.
func TestValueBytes(t *testing.T) {
	if v := Value([]byte("1234.5600"), "MONEY"); !v.(decimal.Decimal).Equal(decimal.RequireFromString("1234.56")) {
		t.Errorf("money = %#v", v)
	}
	g := []byte{0x3e, 0x1d, 0x1c, 0x6f, 0x0e, 0x6b, 0x5e, 0x4d, 0x9a, 0x3c, 0x2b, 0x1f, 0x0e, 0x9d, 0x8c, 0x7b}
	if v := Value(g, "UNIQUEIDENTIFIER"); v != "6F1C1D3E-6B0E-4D5E-9A3C-2B1F0E9D8C7B" {
		t.Errorf("guid = %#v", v)
	}
	if v := Value([]byte{1, 2}, "VARBINARY"); string(v.([]byte)) != "\x01\x02" {
		t.Errorf("binary = %#v", v)
	}
}
