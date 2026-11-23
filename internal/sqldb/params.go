package sqldb

import (
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Placeholders returns the names of the parameters @name in a statement, in order of
// their first use; @ in strings ('…'), quoted names ("…", […], `…`) and comments
// (-- … and /* … */) does not count, nor does @@ (system variables of SQL Server).
func Placeholders(stmt string) []string {
	var names []string
	for _, p := range placeholders(stmt) {
		if !slices.Contains(names, p.name) {
			names = append(names, p.name)
		}
	}
	return names
}

// placeholder is a parameter @name in a statement: its name and where it stands.
type placeholder struct {
	name       string
	start, end int
}

// placeholders returns every use of a parameter in a statement, see Placeholders.
func placeholders(stmt string) []placeholder {
	var out []placeholder
	for i := 0; i < len(stmt); i++ {
		switch c := stmt[i]; {
		case c == '\'' || c == '"' || c == '`':
			i = skipQuoted(stmt, i, c)
		case c == '[':
			i = skipQuoted(stmt, i, ']')
		case c == '-' && i+1 < len(stmt) && stmt[i+1] == '-':
			if j := strings.IndexByte(stmt[i:], '\n'); j >= 0 {
				i += j
			} else {
				i = len(stmt)
			}
		case c == '/' && i+1 < len(stmt) && stmt[i+1] == '*':
			if j := strings.Index(stmt[i+2:], "*/"); j >= 0 {
				i += j + 3
			} else {
				i = len(stmt)
			}
		case c == '@' && i+1 < len(stmt) && stmt[i+1] == '@':
			i++
		case c == '@':
			j := i + 1
			for j < len(stmt) && (isLetter(stmt[j]) || (j > i+1 && stmt[j] >= '0' && stmt[j] <= '9')) {
				j++
			}
			if j > i+1 {
				out = append(out, placeholder{name: stmt[i+1 : j], start: i, end: j})
			}
			i = j - 1
		}
	}
	return out
}

func skipQuoted(s string, i int, end byte) int {
	for j := i + 1; j < len(s); j++ {
		if s[j] == end {
			if j+1 < len(s) && s[j+1] == end && end != ']' { // doubled quote inside
				j++
				continue
			}
			return j
		}
	}
	return len(s)
}

func isLetter(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// Args returns the values of the parameters of a statement as named arguments: every
// @name needs a value under params, and every value must be used.
func Args(stmt string, params map[string]string) ([]any, error) {
	used := Placeholders(stmt)
	var args []any
	for _, name := range used {
		v, ok := params[name]
		if !ok {
			return nil, fmt.Errorf("parameter @%s has no value under params", name)
		}
		args = append(args, sql.Named(name, v))
	}
	for _, name := range slices.Sorted(maps.Keys(params)) {
		if !slices.Contains(used, name) {
			return nil, fmt.Errorf("params: %s is not used in the statement (write @%s where the value goes)", name, name)
		}
	}
	return args, nil
}
