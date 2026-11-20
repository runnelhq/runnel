package core

import (
	"strings"
	"testing"
)

func TestToRows(t *testing.T) {
	rows, err := toRows([]any{map[string]any{"path": "a.csv"}, "b.csv", int64(3)})
	if err != nil || len(rows) != 3 || rows[0]["path"] != "a.csv" || rows[1]["value"] != "b.csv" || rows[2]["value"] != int64(3) {
		t.Errorf("%v %v", rows, err)
	}
	if rows, err := toRows(nil); rows != nil || err != nil {
		t.Errorf("nil: %v %v", rows, err)
	}
	if _, err := toRows(map[string]any{"a": 1}); err == nil || !strings.Contains(err.Error(), "rows must be a list") {
		t.Errorf("object: %v", err)
	}
}
