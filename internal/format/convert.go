package format

import (
	"fmt"
	"math"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

// Convert converts a value to the type of the codec (core.convert): text is read with
// the pattern, values are written as text with it, numbers convert into each other and
// dates into date and time. Null stays null. A value that does not fit is an error,
// e.g. 1.5 as int; nothing is rounded or cut off.
func (c Codec) Convert(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	if c.typ == record.String {
		return c.Format(v)
	}
	if s, ok := v.(string); ok {
		return c.Parse(s)
	}
	from := record.TypeOf(v)
	if from == c.typ {
		return v, nil
	}
	switch c.typ {
	case record.Int:
		switch v := v.(type) {
		case decimal.Decimal:
			if v.IsInteger() && v.Abs().LessThan(decimal.NewFromInt(math.MaxInt64)) {
				return v.IntPart(), nil
			}
			return nil, fmt.Errorf("%s is not a whole number", v)
		case float64:
			if v == math.Trunc(v) && math.Abs(v) < math.MaxInt64 {
				return int64(v), nil
			}
			return nil, fmt.Errorf("%v is not a whole number", v)
		}
	case record.Decimal:
		switch v := v.(type) {
		case int64:
			return decimal.NewFromInt(v), nil
		case float64:
			return decimal.NewFromFloat(v), nil
		}
	case record.Float:
		switch v := v.(type) {
		case int64:
			return float64(v), nil
		case decimal.Decimal:
			return v.InexactFloat64(), nil
		}
	case record.DateType:
		if t, ok := v.(time.Time); ok {
			return record.DateOf(t), nil // the day in the time zone of the value
		}
	case record.DateTime:
		if d, ok := v.(record.Date); ok {
			return d.In(time.Local), nil
		}
	}
	return nil, fmt.Errorf("a value of type %s cannot be converted to %s", from, c.typ)
}
