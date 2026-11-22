package connection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/runnelhq/runnel/internal/schema"
)

// newConfig returns a pointer to a new zero value of the configuration struct.
func newConfig(config any) any {
	if config == nil {
		return &struct{}{}
	}
	return reflect.New(reflect.TypeOf(config)).Interface()
}

// decode fills v from the fields under with: like action.DecodeConfig: unknown fields,
// wrong types and missing required fields are errors.
func decode(with map[string]any, v any) error {
	data, err := json.Marshal(with)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
			return fmt.Errorf("field %q: expected %s, got %s", typeErr.Field, typeErr.Type.Kind(), typeErr.Value)
		}
		if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
			return fmt.Errorf("unknown field %s", field)
		}
		return err
	}
	if field := schema.MissingRequired(v, with); field != "" {
		return fmt.Errorf("missing required field %q", field)
	}
	return nil
}
