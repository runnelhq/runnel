package sqldb

import (
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

// Value converts a value read from a column into a value of the record types of ADR 0007,
// guided by the declared type of the column (e.g. DECIMAL(10,2), DATE): exact numbers
// stay exact (decimal, never float), dates without time become date, NULL becomes nil.
// A value that does not fit its declared type keeps the type the driver returned.
func Value(v any, declared string) any {
	kind := columnKind(declared)
	switch v := v.(type) {
	case nil:
		return nil
	case int64:
		switch kind {
		case kindBool:
			return v != 0
		case kindDecimal:
			return decimal.NewFromInt(v)
		}
		return v
	case float64:
		if kind == kindDecimal {
			if d, err := decimal.NewFromString(strconv.FormatFloat(v, 'f', -1, 64)); err == nil {
				return d
			}
		}
		return v
	case bool:
		return v
	case []byte:
		switch kind {
		case kindText:
			return string(v)
		case kindDecimal, kindDate, kindDateTime, kindBool:
			return fromText(string(v), kind) // e.g. DECIMAL and MONEY of SQL Server come as text
		case kindGUID:
			if len(v) == 16 {
				return guid(v)
			}
		}
		return append([]byte(nil), v...) // the driver may reuse its buffer
	case time.Time:
		if kind == kindDate {
			return record.DateOf(v)
		}
		return v
	case string:
		return fromText(v, kind)
	}
	return v
}

func fromText(s string, kind int) any {
	switch kind {
	case kindDecimal:
		if d, err := decimal.NewFromString(strings.TrimSpace(s)); err == nil {
			return d
		}
	case kindDate:
		if d, err := record.ParseDate(s); err == nil {
			return d
		}
	case kindDateTime:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
	case kindBool:
		switch strings.ToLower(s) {
		case "1", "true":
			return true
		case "0", "false":
			return false
		}
	}
	return s
}

const (
	kindOther = iota
	kindText
	kindDecimal
	kindDate
	kindDateTime
	kindBool
	kindGUID
)

// columnKind sorts a declared column type (as the database names it) into the kinds
// that change how a value is read.
func columnKind(declared string) int {
	t := strings.ToUpper(declared)
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = t[:i]
	}
	switch strings.TrimSpace(t) {
	case "DECIMAL", "NUMERIC", "MONEY", "SMALLMONEY", "NUMBER":
		return kindDecimal
	case "DATE":
		return kindDate
	case "DATETIME", "DATETIME2", "TIMESTAMP", "TIMESTAMPTZ", "SMALLDATETIME", "DATETIMEOFFSET":
		return kindDateTime
	case "BOOL", "BOOLEAN", "BIT":
		return kindBool
	case "UNIQUEIDENTIFIER":
		return kindGUID
	}
	if strings.Contains(t, "CHAR") || strings.Contains(t, "TEXT") || strings.Contains(t, "CLOB") {
		return kindText
	}
	return kindOther
}

// guid writes a UNIQUEIDENTIFIER of SQL Server as text; its first three groups are
// stored with the bytes in reverse order.
func guid(b []byte) string {
	order := []int{3, 2, 1, 0, -1, 5, 4, -1, 7, 6, -1, 8, 9, -1, 10, 11, 12, 13, 14, 15}
	var sb strings.Builder
	for _, i := range order {
		if i < 0 {
			sb.WriteByte('-')
			continue
		}
		sb.WriteString(strconv.FormatUint(uint64(b[i])|0x100, 16)[1:])
	}
	return strings.ToUpper(sb.String())
}
