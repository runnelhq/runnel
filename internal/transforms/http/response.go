package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// http.response answers the waiting caller of an http trigger with response: run with
// the records it receives as JSON (ADR 0018): a list, or the first record as an object
// with single. It answers when its input has ended, if it received records; the first
// http.response that does so answers. One with whenEmpty is the fallback: its answer
// (with or without records) is sent when the run ends and no other one has answered –
// e.g. 404 next to the answer with the record that was looked for. Without a waiting
// caller it does nothing. The records are passed on.
var responseDefinition = transform.Definition{
	Type:        "http.response",
	Category:    transform.CategoryOutput,
	Label:       "HTTP Response",
	Description: "Answers the caller of an HTTP trigger with the records as JSON and passes them on",
	Config:      response{},
	New:         newResponse,
}

// DefaultMaxRows limits the records held in memory for an answer.
const DefaultMaxRows = 10_000

type response struct {
	Status    string            `json:"status" doc:"HTTP status code, e.g. 200 or 404" schema:"default=200,example=200"`
	Headers   map[string]string `json:"headers" doc:"Headers of the answer"`
	Single    bool              `json:"single" doc:"Answer with the first record as an object instead of a list" schema:"default=false"`
	WhenEmpty bool              `json:"whenEmpty" doc:"Fallback: answer also without records, but only if no other http.response answers (e.g. 404)" schema:"default=false"`
	MaxRows   string            `json:"maxRows" doc:"Most records in the answer; more stop the pipeline, as they are held in memory" schema:"default=10000"`
}

func newResponse(with map[string]any) (transform.Transform, error) {
	t := &response{}
	if err := transform.DecodeConfig(with, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *response) Process(ctx context.Context, in <-chan transform.Batch, out *transform.Output) error {
	status, err := parseStatus(t.Status)
	if err != nil {
		return err
	}
	limit := int64(DefaultMaxRows)
	if s := strings.TrimSpace(t.MaxRows); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 1 {
			return fmt.Errorf("maxRows: %q is not a whole number of 1 or more", t.MaxRows)
		}
		limit = n
	}
	caller := out.HasCaller()
	var records []record.Record
	var n int64
	for b := range in {
		for _, r := range b {
			n++
			if caller && (!t.Single || len(records) == 0) {
				if int64(len(records)) >= limit {
					return fmt.Errorf("more than %d records for the answer (maxRows); they are held in memory – raise maxRows or reduce the records", limit)
				}
				records = append(records, r.Clone())
			}
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case !caller:
		out.Logf(slog.LevelDebug, "no HTTP caller waits for an answer; %d records not sent", n)
		return nil
	case n == 0 && !t.WhenEmpty:
		out.Logf(slog.LevelDebug, "no records, no answer (whenEmpty: false)")
		return nil
	}
	body, err := encode(records, t.Single)
	if err != nil {
		return err
	}
	headers := map[string]string{"Content-Type": "application/json; charset=utf-8"}
	for k, v := range t.Headers {
		headers[k] = v
	}
	resp := action.Response{Status: status, Headers: headers, Body: json.RawMessage(body), Fallback: t.WhenEmpty}
	switch {
	case t.WhenEmpty:
		out.Respond(resp)
		out.Logf(slog.LevelDebug, "status %d with %d records is the answer unless another http.response answers (whenEmpty)", status, len(records))
	case out.Respond(resp):
		out.Logf(slog.LevelInfo, "answered the caller with status %d and %d records", status, len(records))
	default:
		out.Logf(slog.LevelWarn, "the caller has already been answered; status %d with %d records not sent", status, len(records))
	}
	if t.Single && n > 1 {
		out.Logf(slog.LevelWarn, "single: %d records, only the first was sent", n)
	}
	return nil
}

// Init passes the schema on: the records stay as they are.
func (t *response) Init(ic *transform.InitContext) (record.Schema, error) { return ic.In, nil }

func parseStatus(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 200, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 200 || n > 599 {
		return 0, fmt.Errorf("invalid status %q (expected a number from 200 to 599)", s)
	}
	return n, nil
}

// encode writes records as JSON objects with their fields in order: a list, or the
// first record (null without records) with single. Decimals are written as numbers,
// dates as yyyy-mm-dd, times in RFC 3339.
func encode(records []record.Record, single bool) ([]byte, error) {
	var b bytes.Buffer
	if single {
		if len(records) == 0 {
			return []byte("null"), nil
		}
		err := encodeRecord(&b, records[0])
		return b.Bytes(), err
	}
	b.WriteByte('[')
	for i, r := range records {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := encodeRecord(&b, r); err != nil {
			return nil, err
		}
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

func encodeRecord(b *bytes.Buffer, r record.Record) error {
	b.WriteByte('{')
	for i, f := range r.Fields() {
		if i > 0 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(f.Name)
		b.Write(name)
		b.WriteByte(':')
		if err := encodeValue(b, f.Value); err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
	}
	b.WriteByte('}')
	return nil
}

func encodeValue(b *bytes.Buffer, v any) error {
	switch v := v.(type) {
	case decimal.Decimal:
		b.WriteString(v.String())
		return nil
	case record.Date:
		v2, _ := json.Marshal(v.String())
		b.Write(v2)
		return nil
	case time.Time:
		v2, _ := json.Marshal(v.Format(time.RFC3339Nano))
		b.Write(v2)
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b.Write(data)
	return nil
}
