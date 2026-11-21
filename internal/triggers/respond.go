package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/httpx"
)

// answerBox takes the answers a run gives its waiting caller (http.respond): the
// first one counts; a fallback (http.response with whenEmpty) only if no other comes.
// Once closed, it takes no more answers.
type answerBox struct {
	once    sync.Once
	answers chan action.Response

	mu       sync.Mutex
	fallback *action.Response
}

func newAnswerBox() *answerBox { return &answerBox{answers: make(chan action.Response, 1)} }

// respond is the action.Responder of the run; it reports whether the answer is taken.
func (b *answerBox) respond(resp action.Response) bool {
	if resp.Fallback {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.fallback == nil {
			b.fallback = &resp
		}
		return true
	}
	first := false
	b.once.Do(func() {
		first = true
		b.answers <- resp
	})
	return first
}

// close ends taking answers and returns the one given before, if any.
func (b *answerBox) close() (action.Response, bool) {
	b.once.Do(func() {})
	select {
	case resp := <-b.answers:
		return resp, true
	default:
		return action.Response{}, false
	}
}

// fallbackAnswer returns the fallback, if one was given.
func (b *answerBox) fallbackAnswer() (action.Response, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fallback == nil {
		return action.Response{}, false
	}
	return *b.fallback, true
}

// answer starts the run of a request with response: run and answers the caller with
// the first http.respond of the run, or with the default answer when the run has ended
// (outputs: 200, failed: 500), or with 504 after the timeout (S38b1). The run goes on
// after the answer and after the timeout.
func (h *httpTriggers) answer(w http.ResponseWriter, r *http.Request, e *entry, start StartRequest) {
	s := h.s
	box := newAnswerBox()
	start.Respond = box.respond
	id, herr := h.start(e, start)
	if herr != nil {
		box.close()
		herr.write(w)
		return
	}
	w.Header().Set("Runnel-Run-Id", id)
	timeout := e.t.ResponseTimeout()
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	ended := make(chan Outcome, 1)
	if s.opts.Wait != nil {
		go func() {
			if out, ok := s.opts.Wait(ctx, id); ok && out.Status != "running" {
				ended <- out
			}
		}()
	}
	select {
	case resp := <-box.answers:
		writeResponse(w, resp)
	case out := <-ended:
		if resp, ok := box.close(); ok {
			writeResponse(w, resp)
			return
		}
		if resp, ok := box.fallbackAnswer(); ok {
			writeResponse(w, resp)
			return
		}
		if out.Status == "succeeded" {
			body := out.Body
			if body == nil {
				body = map[string]any{}
			}
			reply(w, http.StatusOK, body)
			return
		}
		s.opts.Warn(fmt.Sprintf("trigger %q: run %s %s without an answer: %s", e.t.Name, id, out.Status, out.Error))
		reply(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("the run %s; see run %s", out.Status, id), "runId": id})
	case <-ctx.Done():
		if resp, ok := box.close(); ok {
			writeResponse(w, resp)
			return
		}
		if r.Context().Err() != nil {
			return // the caller has gone
		}
		s.opts.Warn(fmt.Sprintf("trigger %q: run %s has not answered within %s; it goes on", e.t.Name, id, timeout))
		reply(w, http.StatusGatewayTimeout, map[string]string{"error": fmt.Sprintf("no answer within %s; the run %s goes on", timeout, id), "runId": id})
	}
}

// writeResponse sends the answer of http.respond: a text as text, other values as
// JSON; headers of the action win.
func writeResponse(w http.ResponseWriter, resp action.Response) {
	var body []byte
	contentType := ""
	switch b := resp.Body.(type) {
	case nil:
	case json.RawMessage:
		body, contentType = append(b, '\n'), "application/json; charset=utf-8"
	case string:
		body, contentType = []byte(b), "text/plain; charset=utf-8"
	default:
		data, err := json.Marshal(b)
		if err != nil {
			reply(w, http.StatusInternalServerError, httpx.ErrorBody{Error: "the body of http.respond cannot be written as JSON: " + err.Error()})
			return
		}
		body, contentType = append(data, '\n'), "application/json; charset=utf-8"
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(resp.Status)
	_, _ = w.Write(body)
}

// requestOf describes a request for expressions (request.method, .path, .headers,
// .query, .body, .client).
func requestOf(r *http.Request, body any, client string) map[string]any {
	headers := map[string]any{}
	for k, v := range r.Header {
		if k == "Authorization" {
			continue // the token stays out of the run
		}
		headers[k] = strings.Join(v, ", ")
	}
	query := map[string]any{}
	for k, v := range r.URL.Query() {
		query[k] = v[0]
	}
	return map[string]any{"method": r.Method, "path": r.URL.Path, "headers": headers, "query": query, "body": body, "client": client}
}

// numbers turns the numbers of a JSON value read with UseNumber into int64 or float64.
func numbers(v any) any {
	switch v := v.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		f, _ := v.Float64()
		return f
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = numbers(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = numbers(x)
		}
		return out
	}
	return v
}
