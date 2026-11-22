package json

import (
	"bytes"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
)

// field is a field of json.input (fields:): its name in the record, where it comes from
// in the JSON object, and its type.
type field struct {
	Name string `json:"name" doc:"Name of the field" schema:"required,example=amount"`
	// From is a path into nested objects, e.g. address.city.
	From   string `json:"from" doc:"Where the value is in the object, with dots for nested objects, e.g. address.city; empty: the key of the same name"`
	Type   string `json:"type" doc:"Type of the value; empty: as in the file (numbers, text, true/false, lists and objects)" schema:"enum=string|int|decimal|float|bool|date|datetime|json"`
	Format string `json:"format" doc:"For values given as text: number pattern like #.##0,00 or date pattern like dd.MM.yyyy" schema:"example=dd.MM.yyyy"`
}

// readObject reads the next value of dec as a record: an object keeps the order of its
// keys; any other value becomes the field "value". Numbers become int or, with a
// fraction, an exact decimal; nested objects and lists stay as they are.
func readObject(dec *stdjson.Decoder) (record.Record, error) {
	tok, err := dec.Token()
	if err != nil {
		return record.Record{}, err
	}
	return objectFrom(dec, tok)
}

// rest reads the rest of a value whose first token was read already.
func rest(dec *stdjson.Decoder, tok stdjson.Token) (any, error) {
	d, ok := tok.(stdjson.Delim)
	if !ok {
		return value(tok)
	}
	switch d {
	case '[':
		var list []any
		for dec.More() {
			var raw any
			if err := dec.Decode(&raw); err != nil {
				return nil, err
			}
			list = append(list, raw)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return value(list)
	case '{':
		m := map[string]any{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			var raw any
			if err := dec.Decode(&raw); err != nil {
				return nil, err
			}
			m[fmt.Sprint(k)] = raw
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return value(m)
	}
	return nil, fmt.Errorf("unexpected %v", d)
}

// value converts a decoded JSON value (with json.Number) into a record value.
func value(v any) (any, error) {
	switch v := v.(type) {
	case stdjson.Number:
		return number(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			n, err := value(e)
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			n, err := value(e)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	}
	return record.Normalize(v)
}

// number keeps whole numbers as int and others exact, so that 19.99 stays 19.99.
func number(n stdjson.Number) (any, error) {
	if i, err := n.Int64(); err == nil {
		return i, nil
	}
	d, err := decimal.NewFromString(n.String())
	if err != nil {
		return nil, fmt.Errorf("%s is not a number", n)
	}
	return d, nil
}

// lookup returns the value at a path of keys in nested objects (address.city).
func lookup(r record.Record, path string) (any, bool) {
	first, more, nested := strings.Cut(path, ".")
	v, ok := r.Get(first)
	for ok && nested {
		m, isMap := v.(map[string]any)
		if !isMap {
			return nil, false
		}
		first, more, nested = strings.Cut(more, ".")
		v, ok = m[first]
	}
	return v, ok
}

// position returns line and column of a byte offset in a file, for messages.
func position(data []byte, offset int64) (line, col int) {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	before := data[:offset]
	line = bytes.Count(before, []byte{'\n'}) + 1
	col = int(offset) - (bytes.LastIndexByte(before, '\n') + 1) + 1
	return line, col
}

// syntaxMessage turns errors of the JSON decoder into a sentence without Go types.
func syntaxMessage(err error) string {
	var se *stdjson.SyntaxError
	switch {
	case errors.As(err, &se):
		return se.Error()
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "the file ends in the middle of a value"
	}
	if ue, ok := errors.AsType[*stdjson.UnmarshalTypeError](err); ok {
		return "unexpected " + ue.Value
	}
	return err.Error()
}
