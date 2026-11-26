package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/script"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.script runs JavaScript for every record (ADR 0028), in a runtime without access
// to files, network or processes, one per copy. The code is the body of a function with
// record and params: an object it returns is the new record, a list several, null none.
// init runs before the first record at the top level (its variables are global), end
// after the last one and may return further records. An exception or a record that
// takes longer than maxRecordTime goes to the error link; init and end longer than
// timeout stop the pipeline (R5b).
var scriptDefinition = transform.Definition{
	Parallel:     true,
	Type:         "core.script",
	RecordErrors: true,
	Category:     transform.CategoryTransform,
	Label:        "Script",
	Description:  "Runs JavaScript for every record: return a new record, several or none",
	Example:      "fields:\n  - { name: name, type: string }\n  - { name: amount, type: decimal }\nscript: |\n  if (!record.name) return null; // drop the record\n  return { name: record.name.trim(), amount: record.amount };\n",
	Config:       scriptTransform{},
	New:          newScript,
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "<fields[].name>", Type: "any", Description: "A declared field, converted to its type; without fields the types follow the values"}},
		Records: "Passes on what the script returns for each record: an object as one record, a list as several, null as none; end may return further records after the last one.",
		Errors:  "A record whose script throws an exception, returns a value that cannot be converted or takes longer than maxRecordTime goes to the error link; without one, the pipeline stops. A syntax error, a missing script file and errors in init or end (also taking longer than timeout) stop the pipeline.",
		Limits: []string{
			"maxRecordTime limits the time per record (default 1s), timeout that of init and of end (default 1m each).",
			"The console keeps at most 1,000 lines per copy, each cut at 8 KiB; a script file or module may be at most 4 MiB.",
			"No access to files, network, processes or environment variables; require loads modules from scripts/ of the project only.",
		},
	},
}

type scriptTransform struct {
	Script        string         `json:"script" doc:"JavaScript run for every record, the body of a function: record holds its fields, params the values; return an object (the new record), a list of objects (several records) or null (none); ${…} and {{ … }} in it are plain text" schema:"format=runnel-script"`
	File          string         `json:"file" doc:"A .js file instead of script, relative to the project folder, e.g. scripts/clean.js" schema:"format=runnel-script"`
	InitCode      string         `json:"init" doc:"JavaScript run once before the first record, e.g. let count = 0; its variables and functions are seen by script and end" schema:"format=runnel-script"`
	End           string         `json:"end" doc:"JavaScript run once after the last record, the body of a function: return an object or a list of objects for further records, e.g. a total" schema:"format=runnel-script"`
	Params        map[string]any `json:"params" doc:"Values for the script as params.NAME; may use ${…}, a value of its own may be ${secret:…}" schema:"format=runnel-params"`
	Fields        []scriptField  `json:"fields" doc:"Fields of the returned records with their type, in this order: each value is converted to the type, other properties are dropped; without fields the types follow the values"`
	MaxRecordTime string         `json:"maxRecordTime" doc:"Longest time the script may take for one record; a record that takes longer goes to the error link" schema:"format=runnel-duration,default=1s"`
	Timeout       string         `json:"timeout" doc:"Longest time init and end may take, each; longer stops the pipeline" schema:"format=runnel-duration,default=1m"`

	// convert converts the values of the fields; declared tells the names of the fields.
	convert  []func(any) (any, error)
	declared map[string]bool
	params   map[string]any
}

type scriptField struct {
	Name string `json:"name" doc:"Name of the field" schema:"required,example=amount"`
	Type string `json:"type" doc:"Type of the field; a returned value is converted to it" schema:"required,enum=string|int|decimal|float|bool|date|datetime|binary|json"`
}

// Defaults of maxRecordTime (ADR 0028) and of timeout (init and end, R5b).
const (
	defaultRecordTime = time.Second
	defaultInitTime   = time.Minute
)

func newScript(with map[string]any) (transform.Transform, error) {
	t := &scriptTransform{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	switch {
	case t.Script == "" && t.File == "":
		return nil, errors.New("give the code in script or its file in file")
	case t.Script != "" && t.File != "":
		return nil, errors.New("give either script or file, not both")
	case t.File != "" && !strings.EqualFold(filepath.Ext(t.File), ".js"):
		return nil, fmt.Errorf("file %q: expected a .js file", t.File)
	}
	if t.Script != "" {
		if err := script.Check("script", t.Script, true, "record"); err != nil {
			return nil, err
		}
	}
	if err := script.Check("init", t.InitCode, false); err != nil {
		return nil, err
	}
	if err := script.Check("end", t.End, true); err != nil {
		return nil, err
	}
	if !strings.Contains(t.MaxRecordTime, "${") {
		if _, err := recordTime("maxRecordTime", t.MaxRecordTime, defaultRecordTime); err != nil {
			return nil, err
		}
	}
	if !strings.Contains(t.Timeout, "${") {
		if _, err := recordTime("timeout", t.Timeout, defaultInitTime); err != nil {
			return nil, err
		}
	}
	params, err := record.Normalize(t.Params)
	if err != nil {
		return nil, fmt.Errorf("params: %w", err)
	}
	t.params, _ = params.(map[string]any)
	if t.params == nil {
		t.params = map[string]any{}
	}
	t.declared = map[string]bool{}
	for i, f := range t.Fields {
		switch {
		case f.Name == "":
			return nil, fmt.Errorf("fields[%d]: every field needs a name", i)
		case t.declared[f.Name]:
			return nil, fmt.Errorf("fields[%d]: the field %q is listed twice", i, f.Name)
		}
		t.declared[f.Name] = true
		c, err := fieldConverter(record.Type(f.Type))
		if err != nil {
			return nil, fmt.Errorf("fields[%d] (%s): %w", i, f.Name, err)
		}
		t.convert = append(t.convert, c)
	}
	return t, nil
}

// fieldConverter returns the conversion of a returned value to the type of a field,
// like core.convert; binary data only from Uint8Array or ArrayBuffer.
func fieldConverter(typ record.Type) (func(any) (any, error), error) {
	if typ == record.Binary {
		return func(v any) (any, error) {
			if b, ok := v.([]byte); ok || v == nil {
				return b, nil
			}
			return nil, fmt.Errorf("a value of type %s cannot be converted to binary (return a Uint8Array)", record.TypeOf(v))
		}, nil
	}
	if typ == "" {
		return nil, fmt.Errorf("a type is required (%s, binary)", format.TypeNames())
	}
	c, err := format.New(typ, "")
	if err != nil {
		return nil, err
	}
	return c.Convert, nil
}

// phaseError names timeout in the error of init or end that took too long.
func phaseError(err error, limit time.Duration) error {
	if errors.Is(err, script.ErrTimeLimit) {
		return fmt.Errorf("%w (timeout %s)", err, limit)
	}
	return err
}

// recordTime reads maxRecordTime or timeout.
func recordTime(field, s string, fallback time.Duration) (time.Duration, error) {
	if s == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid %s %q (examples: 500ms, 5s)", field, s)
	}
	return d, nil
}

// Init returns a closed schema of the declared fields, without fields an open one: what
// a script returns is only known when it runs.
func (t *scriptTransform) Init(*transform.InitContext) (record.Schema, error) {
	if len(t.Fields) == 0 {
		return record.OpenSchema(), nil
	}
	s := record.Schema{Fields: make([]record.FieldDef, len(t.Fields))}
	for i, f := range t.Fields {
		s.Fields[i] = record.FieldDef{Name: f.Name, Type: record.Type(f.Type)}
	}
	return s, nil
}

func (t *scriptTransform) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	limit, err := recordTime("maxRecordTime", t.MaxRecordTime, defaultRecordTime)
	if err != nil {
		return err
	}
	initLimit, err := recordTime("timeout", t.Timeout, defaultInitTime)
	if err != nil {
		return err
	}
	name, code := "script", t.Script
	if t.File != "" {
		name = filepath.ToSlash(t.File)
		if code, err = script.ReadFile(paths.Resolve(ctx, t.File), t.File); err != nil {
			return err
		}
	}
	rt := script.New(script.Options{Dir: paths.Base(ctx), Log: func(l slog.Level, m string) { out.Logf(l, "%s", m) }})
	fn, err := rt.Compile(name, code, "record")
	if err != nil {
		return err
	}
	var end *script.Function
	if t.End != "" {
		if end, err = rt.Compile("end", t.End); err != nil {
			return err
		}
	}
	s := rt.Start(ctx, initLimit)
	defer s.Close()
	s.Set("params", t.params)
	if t.InitCode != "" {
		if err := s.Run("init", t.InitCode); err != nil {
			return phaseError(err, initLimit)
		}
	}
	s.SetLimit(limit)
	warned := map[string]bool{}
	var n, failed int64
	for b := range in {
		for _, r := range b {
			n++
			recs, err := s.Records(fn, true, r)
			field := ""
			for i := 0; err == nil && i < len(recs); i++ {
				recs[i], field, err = t.typed(recs[i], out, warned)
			}
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if errors.Is(err, script.ErrTimeLimit) {
					err = fmt.Errorf("%s: the script took longer than maxRecordTime (%s)", name, limit)
				}
				failed++
				if err := out.Fail(r, field, fmt.Errorf("record %d: %w", n, err)); err != nil {
					return err
				}
				continue
			}
			for _, x := range recs {
				if err := out.Emit(x); err != nil {
					return err
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if end != nil {
		s.SetLimit(initLimit)
		recs, err := s.Records(end, true)
		if err != nil {
			return phaseError(err, initLimit)
		}
		for i := 0; err == nil && i < len(recs); i++ {
			recs[i], _, err = t.typed(recs[i], out, warned)
		}
		if err != nil {
			return err
		}
		for _, x := range recs {
			if err := out.Emit(x); err != nil {
				return err
			}
		}
	}
	if failed > 0 {
		out.Logf(slog.LevelInfo, "%d of %d records with errors", failed, n)
	}
	return nil
}

// typed converts the values of a returned record to the declared fields, in their order;
// a missing field is null, other properties are dropped with a warning (once per name).
// Without fields the record stays as it is. On error it returns the field.
func (t *scriptTransform) typed(r record.Record, out *transform.Output, warned map[string]bool) (record.Record, string, error) {
	if len(t.Fields) == 0 {
		return r, "", nil
	}
	fields := make([]record.Field, len(t.Fields))
	present := 0
	for i, f := range t.Fields {
		v, ok := r.GetAt(i, f.Name)
		if ok {
			present++
		}
		c, err := t.convert[i](v)
		if err != nil {
			return r, f.Name, fmt.Errorf("field %q: %w", f.Name, err)
		}
		fields[i] = record.Field{Name: f.Name, Value: c}
	}
	if r.Len() > present {
		for _, f := range r.Fields() {
			if !t.declared[f.Name] && !warned[f.Name] {
				warned[f.Name] = true
				out.Logf(slog.LevelWarn, "the script returned the field %q, which is not listed in fields; it is dropped", f.Name)
			}
		}
	}
	return record.FromFields(fields), "", nil
}
