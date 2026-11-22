package sqldb

import (
	"database/sql"
	"strings"
	"testing"
)

func TestPlaceholders(t *testing.T) {
	for stmt, want := range map[string]string{
		"SELECT * FROM t WHERE a = @a AND b > @b_2 OR a = @a":       "a,b_2",
		"SELECT '@not', \"@col\", [@col], `@col` FROM t WHERE x=@x": "x",
		"SELECT @@VERSION, @v -- @comment\n/* @block */ FROM t":     "v",
		"SELECT 'it''s @no' WHERE @yes = 1":                         "yes",
		"SELECT 1":                                                  "",
		"SELECT @ FROM t":                                           "",
	} {
		if got := strings.Join(Placeholders(stmt), ","); got != want {
			t.Errorf("Placeholders(%q) = %q, want %q", stmt, got, want)
		}
	}
}

func TestArgs(t *testing.T) {
	args, err := Args("UPDATE t SET a = @a WHERE id = @id", map[string]string{"id": "7", "a": "x"})
	if err != nil || len(args) != 2 || args[0] != sql.Named("a", "x") || args[1] != sql.Named("id", "7") {
		t.Errorf("%v, %v", args, err)
	}
	for _, tt := range []struct {
		stmt   string
		params map[string]string
		want   string
	}{
		{"DELETE FROM t WHERE id = @id", nil, "parameter @id has no value under params"},
		{"DELETE FROM t", map[string]string{"id": "1"}, "params: id is not used in the statement (write @id where the value goes)"},
	} {
		if _, err := Args(tt.stmt, tt.params); err == nil || err.Error() != tt.want {
			t.Errorf("Args(%q): %v, want %q", tt.stmt, err, tt.want)
		}
	}
}
