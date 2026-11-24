package plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/transform"
)

// EmitWindow is how many transform.emit of a stream the host takes before the plugin
// waits for $/ack (ADR 0019): the records of a plugin in the memory of the host.
const EmitWindow = 4

// maxWindow limits the window a plugin asks for in its answer to transform.open.
const maxWindow = 64

// StreamContext is what a transform of a plugin learns about its run.
type StreamContext struct {
	RunID       string `json:"runId"`
	TransformID string `json:"transformId"`
	ProjectDir  string `json:"projectDir"`
	Environment string `json:"environment,omitempty"`
}

// stream is an open stream of a transform: what the plugin sends for it.
type stream struct {
	id      int64
	msgs    chan emitMsg  // transform.emit and transform.closed, at most EmitWindow+1
	credits atomic.Int64  // batches the host may still send
	emits   atomic.Int64  // transform.emit the plugin may still send
	more    chan struct{} // signals new credits
	broken  chan struct{} // closed when the plugin did not keep to the protocol
	once    sync.Once
	reason  error
	// fields are those of the last transform.emit, which apply to an emit without fields
	// (ADR 0019); only notify uses them, in the order of the messages.
	fields []record.FieldDef
}

// emitMsg is a transform.emit or (closed set) a transform.closed of the plugin.
type emitMsg struct {
	closed bool
	err    string
	to     string
	recs   []record.Record
}

// wire forms of the messages of a stream.
type (
	batchMsg struct {
		Stream int64             `json:"stream"`
		Fields []record.FieldDef `json:"fields,omitempty"`
		Rows   [][]any           `json:"rows"`
	}
	emitWire struct {
		Stream int64             `json:"stream"`
		To     string            `json:"to"`
		Fields []record.FieldDef `json:"fields"`
		Rows   [][]any           `json:"rows"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Batches int64 `json:"batches"`
	}
)

// notify receives the notifications of the plugin, in the order they come: the records
// and the end of streams and the credits for batches ($/ack).
func (pr *Process) notify(method string, params json.RawMessage) {
	switch method {
	case "transform.emit", "transform.closed", "$/ack":
	default:
		return // unknown notifications are ignored (ADR 0019)
	}
	var m emitWire
	dec := json.NewDecoder(bytes.NewReader(params))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return
	}
	pr.streamMu.Lock()
	s := pr.streams[m.Stream]
	pr.streamMu.Unlock()
	if s == nil {
		return // a stream that ended already
	}
	switch method {
	case "$/ack":
		if m.Batches > 0 {
			s.credits.Add(m.Batches)
			select {
			case s.more <- struct{}{}:
			default:
			}
		}
		return
	case "transform.closed":
		msg := emitMsg{closed: true}
		if m.Error != nil {
			msg.err = cmpOr(m.Error.Message, "the transform failed")
		}
		s.deliver(msg)
		return
	}
	if s.emits.Add(-1) < 0 {
		s.fail(fmt.Errorf("the plugin sent more than %d transform.emit without waiting for $/ack", EmitWindow))
		return
	}
	if m.Fields == nil {
		m.Fields = s.fields
	}
	s.fields = m.Fields
	recs, err := decodeRows(m.Fields, m.Rows)
	if err != nil {
		s.fail(fmt.Errorf("transform.emit: %w", err))
		return
	}
	s.deliver(emitMsg{to: cmpOr(m.To, "main"), recs: recs})
}

// deliver passes a message to the stream; it never waits, because the plugin keeps to
// its window (checked with emits) and the stream holds the window and transform.closed.
func (s *stream) deliver(m emitMsg) {
	select {
	case s.msgs <- m:
	default:
		s.fail(fmt.Errorf("the plugin sent more than %d transform.emit without waiting for $/ack", EmitWindow))
	}
}

// newStream returns a stream with the windows of both sides.
func newStream(id int64) *stream {
	s := &stream{id: id, msgs: make(chan emitMsg, EmitWindow+1), more: make(chan struct{}, 1), broken: make(chan struct{})}
	s.emits.Store(EmitWindow)
	return s
}

func (s *stream) fail(err error) {
	s.once.Do(func() {
		s.reason = err
		close(s.broken)
	})
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Stream runs a transform of the plugin (transform.open, .batch, .end): the batches of
// in go to the plugin as long as it grants credits, the records it emits go to out (to
// "main", "error" or one of targets). It returns when the plugin closes the stream.
func (pr *Process) Stream(ctx context.Context, typ string, config map[string]any, sc StreamContext, in <-chan transform.Batch, out *transform.Output, targets []string) error {
	pr.streamMu.Lock()
	pr.lastStream++
	s := newStream(pr.lastStream)
	pr.streams[s.id] = s
	pr.streamMu.Unlock()
	defer func() {
		pr.streamMu.Lock()
		delete(pr.streams, s.id)
		pr.streamMu.Unlock()
	}()

	octx, cancel := context.WithTimeout(ctx, StartTimeout)
	var open struct {
		Window int64 `json:"window"`
	}
	err := pr.call(octx, "transform.open", map[string]any{"stream": s.id, "type": typ, "config": config, "context": sc, "emitWindow": EmitWindow}, &open)
	cancel()
	if err != nil {
		return err
	}
	s.credits.Store(min(max(open.Window, 1), maxWindow))

	sctx, stop := context.WithCancel(ctx)
	defer stop()
	sent := make(chan error, 1)
	go func() { sent <- pr.send(sctx, s, in) }()
	for {
		select {
		case m := <-s.msgs:
			if m.closed {
				if m.err != "" {
					return errors.New(pr.mask(m.err))
				}
				return nil
			}
			if err := pass(m, out, targets); err != nil {
				return err
			}
			s.emits.Add(1)
			if err := pr.conn.Notify("$/ack", map[string]int64{"stream": s.id, "batches": 1}); err != nil {
				return pr.ended()
			}
		case err := <-sent:
			if err != nil {
				return err
			}
			sent = nil // all batches are sent; wait for transform.closed
		case <-s.broken:
			return s.reason
		case <-pr.conn.Done():
			return pr.ended()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// send passes the batches of in to the plugin, one transform.batch per credit, and
// transform.end when in is closed.
func (pr *Process) send(ctx context.Context, s *stream, in <-chan transform.Batch) error {
	var fields []string // the names of the fields last sent
	for {
		var b transform.Batch
		var ok bool
		select {
		case b, ok = <-in:
		case <-ctx.Done():
			return nil
		}
		if !ok {
			break
		}
		for len(b) > 0 {
			// The records up to the first with other fields share one header.
			n := 1
			names := b[0].Names()
			for n < len(b) && sameNames(b[n], names) {
				n++
			}
			msg := batchMsg{Stream: s.id, Rows: make([][]any, n)}
			if !slices.Equal(names, fields) {
				msg.Fields = header(b[0])
				fields = names
			}
			for i, rec := range b[:n] {
				msg.Rows[i] = encodeRow(rec)
			}
			for s.credits.Load() <= 0 {
				select {
				case <-s.more:
				case <-s.broken:
					return nil
				case <-ctx.Done():
					return nil
				}
			}
			s.credits.Add(-1)
			if err := pr.conn.Notify("transform.batch", msg); err != nil {
				return pr.ended()
			}
			b = b[n:]
		}
	}
	if err := pr.conn.Notify("transform.end", map[string]int64{"stream": s.id}); err != nil {
		return pr.ended()
	}
	return nil
}

// pass hands the records of a transform.emit to the links of the transform: "main" to
// the data links that do not lead to a target of the configuration (like core.switch).
func pass(m emitMsg, out *transform.Output, targets []string) error {
	var others []string // the data links of main when some lead to targets
	if m.to == "main" && len(targets) > 0 {
		for _, id := range out.TargetIDs() {
			if !slices.Contains(targets, id) {
				others = append(others, id)
			}
		}
	}
	for _, rec := range m.recs {
		var err error
		switch {
		case m.to == "main" && others != nil:
			for i, id := range others {
				r := rec
				if i > 0 {
					r = rec.Clone()
				}
				if err = out.EmitTo(id, r); err != nil {
					break
				}
			}
		case m.to == "main" && len(targets) > 0:
			// all data links lead to targets: main has none
		case m.to == "main":
			err = out.Emit(rec)
		case m.to == "error":
			msg, _ := rec.Get(transform.ErrorField)
			field, _ := rec.Get(transform.ErrorFieldField)
			rec.Delete(transform.ErrorField)
			rec.Delete(transform.ErrorFieldField)
			rec.Delete(transform.ErrorTransformField)
			text, _ := msg.(string)
			name, _ := field.(string)
			err = out.Fail(rec, name, errors.New(cmpOr(text, "the plugin reported an error")))
		case slices.Contains(targets, m.to):
			err = out.EmitTo(m.to, rec)
		default:
			return fmt.Errorf("the plugin emitted records to %q, which is not a target in the configuration", m.to)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func sameNames(r record.Record, names []string) bool {
	fs := r.Fields()
	if len(fs) != len(names) {
		return false
	}
	for i, f := range fs {
		if f.Name != names[i] {
			return false
		}
	}
	return true
}

// header is the names and types of the fields of a record.
func header(r record.Record) []record.FieldDef {
	fs := r.Fields()
	out := make([]record.FieldDef, len(fs))
	for i, f := range fs {
		out[i] = record.FieldDef{Name: f.Name, Type: record.TypeOf(f.Value)}
	}
	return out
}

// encodeRow writes the values of a record as JSON values (ADR 0019): decimal as text,
// date as "2026-11-21", datetime in RFC 3339, binary as Base64, json as it is.
func encodeRow(r record.Record) []any {
	fs := r.Fields()
	row := make([]any, len(fs))
	for i, f := range fs {
		switch v := f.Value.(type) {
		case decimal.Decimal:
			row[i] = v.String()
		case record.Date:
			row[i] = v.String()
		case time.Time:
			row[i] = v.Format(time.RFC3339Nano)
		case map[string]any, []any:
			row[i] = record.JSONValue(v)
		default:
			row[i] = v
		}
	}
	return row
}

// decodeRows makes records of the rows of a transform.emit with the fields of its header.
func decodeRows(fields []record.FieldDef, rows [][]any) ([]record.Record, error) {
	recs := make([]record.Record, len(rows))
	for i, row := range rows {
		if len(row) != len(fields) {
			return nil, fmt.Errorf("row %d has %d values for %d fields", i+1, len(row), len(fields))
		}
		fs := make([]record.Field, len(row))
		for k, v := range row {
			value, err := decodeValue(v, fields[k].Type)
			if err != nil {
				return nil, fmt.Errorf("row %d, field %q: %w", i+1, fields[k].Name, err)
			}
			fs[k] = record.Field{Name: fields[k].Name, Value: value}
		}
		recs[i] = record.FromFields(fs)
	}
	return recs, nil
}

// decodeValue reads a JSON value (numbers as json.Number) as a value of type t; without
// a type as the JSON value (numbers as int or float).
func decodeValue(v any, t record.Type) (any, error) {
	if v == nil {
		return nil, nil
	}
	wrong := func() (any, error) { return nil, fmt.Errorf("%v is not a value of type %s", v, t) }
	switch t {
	case record.String:
		if s, ok := v.(string); ok {
			return s, nil
		}
		return wrong()
	case record.Int:
		if n, ok := v.(json.Number); ok {
			if i, err := n.Int64(); err == nil {
				return i, nil
			}
		}
		return wrong()
	case record.Float:
		if n, ok := v.(json.Number); ok {
			if f, err := n.Float64(); err == nil {
				return f, nil
			}
		}
		return wrong()
	case record.Decimal:
		var text string
		switch n := v.(type) {
		case string:
			text = n
		case json.Number:
			text = n.String()
		}
		if d, err := decimal.NewFromString(text); err == nil && text != "" {
			return d, nil
		}
		return wrong()
	case record.Bool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return wrong()
	case record.DateType:
		if s, ok := v.(string); ok {
			if d, err := record.ParseDate(s); err == nil {
				return d, nil
			}
		}
		return wrong()
	case record.DateTime:
		if s, ok := v.(string); ok {
			if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return ts, nil
			}
		}
		return wrong()
	case record.Binary:
		if s, ok := v.(string); ok {
			if b, err := base64.StdEncoding.DecodeString(s); err == nil {
				return b, nil
			}
		}
		return wrong()
	}
	return record.Normalize(v) // json and unknown: maps, lists, texts and numbers as they are
}
