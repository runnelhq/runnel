package plugintest

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// The transforms of the test plugin (S50a), with credits like a real plugin:
//
//	demo.upper  writes the field "field" in capitals; the value "bad" goes to the error
//	            link, "route" to the target "to", "crash" ends the process
//	demo.count  an input: "count" records with a value of every type
//
// demo.upper names a category that runnel does not know: it is grouped by the plugin.
//
// Modes: "bad-to" emits to a target that is not in the configuration, "close-error"
// closes every stream with an error, "same-fields" leaves out the fields of an emit when
// they are those of the last emit.

func transformDescriptors() []any {
	return []any{
		map[string]any{
			"type": "demo.upper", "category": "strings", "label": "Upper", "description": "Writes a field in capitals",
			"recordErrors": true, "parallel": true, "init": true,
			"schema": map[string]any{"type": "object", "required": []string{"field"}, "additionalProperties": false, "properties": map[string]any{
				"field": map[string]any{"type": "string"},
				"to":    map[string]any{"type": "string", "format": "runnel-target"},
			}},
		},
		map[string]any{
			"type": "demo.count", "category": "input", "label": "Count", "description": "Emits numbered records",
			"init": true, "detectFields": true,
			"schema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"count":    map[string]any{"type": "integer"},
				"api":      map[string]any{"type": "string", "format": "runnel-connection", "x-runnel-connection": []string{"demo"}},
				"password": map[string]any{"type": "string", "format": "runnel-secret"},
				"fields":   map[string]any{"type": "array"},
			}},
		},
	}
}

// window is the number of batches the test plugin takes before it acknowledges them.
const window = 4

type pstream struct {
	typ     string
	config  map[string]any
	batches chan json.RawMessage // transform.batch and nil for transform.end; at most window
	credits int                  // emits the host still takes
	cond    *sync.Cond
	sent    string // fields of the last emit, as JSON (mode same-fields)
}

type streams struct {
	mode string
	send func(any)
	mu   sync.Mutex
	all  map[int64]*pstream
}

func (s *streams) notify(method string, params any) {
	s.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func streamOf(params json.RawMessage) int64 {
	var p struct {
		Stream int64 `json:"stream"`
	}
	_ = json.Unmarshal(params, &p)
	return p.Stream
}

// open starts a stream and answers with the window.
func (s *streams) open(params json.RawMessage) any {
	var p struct {
		Stream     int64          `json:"stream"`
		Type       string         `json:"type"`
		Config     map[string]any `json:"config"`
		EmitWindow int            `json:"emitWindow"`
	}
	_ = json.Unmarshal(params, &p)
	st := &pstream{typ: p.Type, config: p.Config, batches: make(chan json.RawMessage, window+1), credits: p.EmitWindow}
	st.cond = sync.NewCond(&s.mu)
	s.mu.Lock()
	s.all[p.Stream] = st
	s.mu.Unlock()
	go s.run(p.Stream, st)
	return map[string]any{"window": window}
}

// batch passes a transform.batch or (end) transform.end to its stream.
func (s *streams) batch(params json.RawMessage, end bool) {
	s.mu.Lock()
	st := s.all[streamOf(params)]
	s.mu.Unlock()
	if st == nil {
		return
	}
	if end {
		st.batches <- nil
		return
	}
	st.batches <- params
}

// ack gives a stream credits for more emits.
func (s *streams) ack(params json.RawMessage) {
	var p struct {
		Stream  int64 `json:"stream"`
		Batches int   `json:"batches"`
	}
	_ = json.Unmarshal(params, &p)
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.all[p.Stream]; st != nil {
		st.credits += p.Batches
		st.cond.Broadcast()
	}
}

// emit sends records to a target, waiting for a credit.
func (s *streams) emit(id int64, st *pstream, to string, fields []any, rows [][]any) {
	s.mu.Lock()
	for st.credits <= 0 {
		st.cond.Wait()
	}
	st.credits--
	msg := map[string]any{"stream": id, "to": to, "fields": fields, "rows": rows}
	if s.mode == "same-fields" {
		header, _ := json.Marshal(fields)
		if string(header) == st.sent {
			delete(msg, "fields") // the fields of the last emit apply
		}
		st.sent = string(header)
	}
	s.mu.Unlock()
	s.notify("transform.emit", msg)
}

func (s *streams) run(id int64, st *pstream) {
	closed := map[string]any{"stream": id}
	defer func() {
		s.mu.Lock()
		delete(s.all, id)
		s.mu.Unlock()
		s.notify("transform.closed", closed)
	}()
	if s.mode == "close-error" {
		closed["error"] = map[string]any{"message": "cannot reach the server with s3cret"}
		return
	}
	if st.typ == "demo.count" {
		s.count(id, st)
		for b := range st.batches { // an input gets transform.end at once
			if b == nil {
				return
			}
		}
		return
	}
	field, _ := st.config["field"].(string)
	target, _ := st.config["to"].(string)
	var fields []any
	for b := range st.batches {
		if b == nil {
			return
		}
		var m struct {
			Fields []map[string]any `json:"fields"`
			Rows   [][]any          `json:"rows"`
		}
		_ = json.Unmarshal(b, &m)
		if m.Fields != nil {
			fields = nil
			for _, f := range m.Fields {
				fields = append(fields, f)
			}
		}
		at := -1
		for i, f := range fields {
			if f.(map[string]any)["name"] == field {
				at = i
			}
		}
		var main, routed, errs [][]any
		for _, row := range m.Rows {
			var v string
			if at >= 0 {
				v, _ = row[at].(string)
			}
			switch v {
			case "crash":
				fmt.Fprintln(os.Stderr, "panic: crash in upper")
				os.Exit(3) //nolint:gocritic // the crash is the purpose: the deferred transform.closed must not be sent
			case "bad":
				errs = append(errs, append(row, "invalid value bad", field))
			case "route":
				routed = append(routed, row)
			default:
				if at >= 0 {
					row[at] = strings.ToUpper(v)
				}
				main = append(main, row)
			}
		}
		to := "main"
		if s.mode == "bad-to" {
			to = "nowhere"
		}
		if len(main) > 0 || len(m.Rows) == 0 {
			s.emit(id, st, to, fields, main)
		}
		if len(routed) > 0 {
			s.emit(id, st, target, fields, routed)
		}
		if len(errs) > 0 {
			ef := append(append([]any(nil), fields...), map[string]any{"name": "_error", "type": "string"}, map[string]any{"name": "_errorField", "type": "string"})
			s.emit(id, st, "error", ef, errs)
		}
		s.notify("$/ack", map[string]any{"stream": id, "batches": 1})
	}
}

// countFields are the fields of demo.count.
var countFields = []any{
	map[string]any{"name": "n", "type": "int"}, map[string]any{"name": "amount", "type": "decimal"},
	map[string]any{"name": "day", "type": "date"}, map[string]any{"name": "at", "type": "datetime"},
	map[string]any{"name": "raw", "type": "binary"}, map[string]any{"name": "doc", "type": "json"},
	map[string]any{"name": "ok", "type": "bool"}, map[string]any{"name": "ratio", "type": "float"},
	map[string]any{"name": "name", "type": "string"}, map[string]any{"name": "any"},
}

// initSchema answers transform.init: demo.count has its fields; demo.upper passes its
// input on and needs the field it writes (an error at "field" if a closed input lacks it).
// The configuration comes back in a warning, to show what init gets.
func initSchema(params json.RawMessage) any {
	var p struct {
		Type   string         `json:"type"`
		Config map[string]any `json:"config"`
		Input  struct {
			Fields []map[string]any `json:"fields"`
			Open   bool             `json:"open"`
		} `json:"input"`
		Targets []string `json:"targets"`
	}
	_ = json.Unmarshal(params, &p)
	config, _ := json.Marshal(p.Config)
	problems := []any{map[string]any{"path": "", "message": fmt.Sprintf("init got %s, targets %v", config, p.Targets), "warning": true}}
	// Lists come as [], never as null (ADR 0019, Nachtrag Rust-SDK): a strict SDK refuses null.
	var raw struct {
		Input   struct{ Fields json.RawMessage } `json:"input"`
		Targets json.RawMessage                  `json:"targets"`
	}
	_ = json.Unmarshal(params, &raw)
	if string(raw.Input.Fields) == "null" || string(raw.Targets) == "null" {
		problems = append(problems, map[string]any{"path": "", "message": "init got null instead of a list"})
	}
	if p.Type == "demo.count" {
		return map[string]any{"output": map[string]any{"fields": countFields}, "problems": problems}
	}
	field, _ := p.Config["field"].(string)
	found := false
	for _, f := range p.Input.Fields {
		found = found || f["name"] == field
	}
	if !found && !p.Input.Open {
		problems = append(problems, map[string]any{"path": "field", "message": fmt.Sprintf("field %q is not in the input", field)})
	}
	return map[string]any{"output": map[string]any{"fields": p.Input.Fields, "open": p.Input.Open}, "problems": problems}
}

// detect answers transform.detectFields of demo.count with the connection it got.
func detect(params json.RawMessage) any {
	var p struct {
		Config map[string]any `json:"config"`
		Limit  int            `json:"limit"`
	}
	_ = json.Unmarshal(params, &p)
	api, _ := p.Config["api"].(map[string]any)
	return map[string]any{
		"source": []any{map[string]any{"name": "Connection", "value": fmt.Sprintf("%v %v %v", api["name"], api["host"], api["password"])}},
		"rows":   3, "complete": true,
		"fields": []any{
			map[string]any{"name": "n", "type": "int", "values": []string{"1", "2", "3"}},
			map[string]any{"name": "name", "type": "string", "values": []string{"row 1", "row 2", "row 3"}, "note": fmt.Sprintf("limit %d", p.Limit)},
		},
	}
}

// count emits the records of demo.count in batches of 100, with a value of every type.
func (s *streams) count(id int64, st *pstream) {
	n, _ := st.config["count"].(float64)
	fields := countFields
	var rows [][]any
	for i := 1; i <= int(n); i++ {
		rows = append(rows, []any{i, "12.50", "2026-11-24", "2026-11-24T10:30:00+01:00", "aGk=",
			map[string]any{"k": i}, i%2 == 0, 0.5, fmt.Sprintf("row %d", i), nil})
		if len(rows) == 100 || i == int(n) {
			s.emit(id, st, "main", fields, rows)
			rows = nil
		}
	}
}
