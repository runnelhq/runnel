package record

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
)

// JSONValue makes a value of a run writable with encoding/json: exact numbers stay
// numbers (a decimal as json.Number, not as text in quotes), dates and times become
// text in ISO 8601. Objects and lists are converted item by item; other values stay as
// they are.
func JSONValue(v any) any {
	switch v := v.(type) {
	case decimal.Decimal:
		return json.Number(v.String())
	case Date:
		return v.String()
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = JSONValue(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = JSONValue(x)
		}
		return out
	case []map[string]any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = JSONValue(x)
		}
		return out
	}
	return v
}
