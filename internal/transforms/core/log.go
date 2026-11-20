package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/runnelhq/runnel/internal/expr"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// core.log writes records to the run log and passes them on unchanged.
var logDefinition = transform.Definition{
	Type:        "core.log",
	Category:    transform.CategoryOutput,
	Label:       "Log",
	Description: "Writes records to the run log and passes them on",
	Config:      logRecords{},
	New:         newLog,
}

type logRecords struct {
	Level string `json:"level" doc:"Log level of the messages" schema:"enum=debug|info|warn|error,default=info"`
	Every int64  `json:"every" doc:"Writes every n-th record, e.g. 1000 for large data" schema:"default=1"`
}

func newLog(with map[string]any) (transform.Transform, error) {
	t := &logRecords{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	if t.Every < 0 {
		return nil, fmt.Errorf("every must be at least 1")
	}
	if _, err := level(t.Level); err != nil {
		return nil, err
	}
	return t, nil
}

func level(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("invalid level %q (expected debug, info, warn or error)", s)
}

func (t *logRecords) Process(_ context.Context, in <-chan transform.Batch, out *transform.Output) error {
	lvl, _ := level(t.Level)
	every := max(t.Every, 1)
	var n int64
	for b := range in {
		for _, r := range b {
			n++
			if n%every == 0 {
				out.Logf(lvl, "#%d %s", n, Format(r))
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	return nil
}

// Format writes a record in one line: name: value, …; texts in quotes.
func Format(r record.Record) string {
	var b strings.Builder
	b.WriteString("{")
	for i, f := range r.Fields() {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.Name)
		b.WriteString(": ")
		if s, ok := f.Value.(string); ok {
			fmt.Fprintf(&b, "%q", s)
		} else if f.Value == nil {
			b.WriteString("null")
		} else {
			b.WriteString(expr.Format(f.Value))
		}
	}
	b.WriteString("}")
	return b.String()
}
