package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/format"
	"github.com/runnelhq/runnel/internal/httpx"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/pagination"
	"github.com/runnelhq/runnel/internal/paths"
	"github.com/runnelhq/runnel/internal/record"
)

// http.request sends an HTTP request, e.g. to a REST API (roadmap S10). A text body is
// sent as text, other values (maps, lists) as JSON. An answer in JSON is read into a
// value that later actions can use, e.g. {{ results.fetch.output.body.items }}.
// Status 400 and above fail the action unless failOnError is false. The answer is
// limited in size and time; headers are never written to the log, since they often
// hold tokens. Output: status, headers and body of the answer.
var requestDefinition = action.Definition{
	Type:        "http.request",
	Category:    action.CategoryNetwork,
	Label:       "HTTP Request",
	Description: "Sends an HTTP request, e.g. to a REST API; output: status, headers and body of the answer",
	Example:     "connection: shop-api\nurl: orders\nquery:\n  status: open\n",
	Config:      requestAction{},
	New:         newRequest,
	Reference: action.Reference{
		Outputs: []action.OutputField{
			{Name: "output.status", Type: "integer", Description: "HTTP status of the answer (the last one with paginate)"},
			{Name: "output.headers", Type: "object", Description: "Headers of the answer with lowercase names; several values joined with a comma"},
			{Name: "output.body", Type: "any", Description: "The answer: a value for JSON, else text; not with paginate or a saved file"},
			{Name: "output.items", Type: "list", Description: "With paginate, the items of all pages"},
			{Name: "output.count", Type: "integer", Description: "With paginate, the number of items"},
			{Name: "output.pages", Type: "integer", Description: "With paginate, the number of pages read"},
			{Name: "output.path", Type: "string", Description: "With saveTo, the path of the saved file"},
			{Name: "output.size", Type: "integer", Description: "With saveTo, the size of the saved file in bytes"},
			{Name: "output.skipped", Type: "boolean", Description: "With saveTo and ifExists: skip, true if the file existed and nothing was requested"},
		},
		Errors: "Fails on status 400 and above (unless failOnError: false; the message shows the beginning of the answer), when there is no answer within timeout, the answer is larger than maxSize, the API asks to wait longer than retryAfter.maxWait, or paginate exceeds its limits or leads to another host. With saveTo and ifExists: fail, fails before the request when the file exists; a failed download leaves no file.",
		Limits: []string{
			"timeout limits each request (default 60s; with paginate per page); the timeout of the action limits all together.",
			"maxSize limits the answer (default 10MB, per page with paginate) or with saveTo the file (default 1GB).",
			"After 429 or 503 with Retry-After, the request is repeated at most retryAfter.maxRetries times (default 3), waiting at most retryAfter.maxWait (default 1m).",
			"paginate reads at most maxPages pages (default 100) and maxItems items (default 10,000), all on the host of the first page.",
		},
	},
}

type requestAction struct {
	Method      string             `json:"method" doc:"HTTP method" schema:"enum=GET|POST|PUT|PATCH|DELETE|HEAD,default=GET"`
	Connection  string             `json:"connection" doc:"Connection of the API (a file in connections/) with base address and sign-in; empty: url is a full address" schema:"connection=http,example=shop-api"`
	URL         string             `json:"url" doc:"Address with http:// or https://, e.g. https://api.example.com/orders; with connection relative to its baseUrl, e.g. orders" schema:"example=https://api.example.com/orders"`
	Query       map[string]string  `json:"query" doc:"Query parameters, added to the address with encoding, e.g. status: open"`
	Headers     map[string]string  `json:"headers" doc:"Headers of the request, e.g. Accept; tokens belong into a connection, never into the file"`
	Body        any                `json:"body" doc:"Body: a text is sent as text, other values (maps, lists, numbers) as JSON"`
	Timeout     string             `json:"timeout" doc:"Longest time for one request and its answer (with paginate: per page), e.g. 30s or 5m; the timeout of the action limits all together" schema:"format=runnel-duration,default=60s"`
	FailOnError *bool              `json:"failOnError" doc:"Fail the action if the answer has status 400 or above; false: the status is only in the output" schema:"default=true"`
	MaxSize     string             `json:"maxSize" doc:"Largest answer to read, e.g. 10MB or 500KB; with paginate per page; with saveTo the largest file (default 1GB)" schema:"default=10MB"`
	SaveTo      string             `json:"saveTo" doc:"Saves a successful answer (status 2xx) as a file instead of reading it, e.g. a ZIP or PDF; relative to the project folder or absolute. Written to a temporary file first, so no half file remains; output: path and size instead of body" schema:"example=inbox/export.zip"`
	IfExists    string             `json:"ifExists" doc:"With saveTo, if the file exists: replace it once the download is complete, skip the request, or fail before the request" schema:"enum=replace|skip|fail,default=replace"`
	RetryAfter  *retryAfterConfig  `json:"retryAfter" doc:"Asking again when the API answers 429 or 503 with Retry-After (wait in seconds or a date); on by default"`
	Paginate    *pagination.Config `json:"paginate" doc:"Reading all pages of a paged API; output: items (all items), count and pages instead of body"`
}

const (
	defaultTimeout = 60 * time.Second
	defaultMaxSize = 10 << 20
	// defaultMaxFile is the largest download with saveTo unless maxSize says otherwise.
	defaultMaxFile = 1 << 30
)

// client sends the requests of all http.request actions; proxies come from the
// environment (HTTPS_PROXY), like in other programs.
var client = &http.Client{Transport: http.DefaultTransport}

func newRequest(with map[string]any) (action.Action, error) {
	a := &requestAction{}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	switch strings.ToUpper(a.Method) {
	case "", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
	default:
		return nil, fmt.Errorf("invalid method %q (expected GET, POST, PUT, PATCH, DELETE or HEAD)", a.Method)
	}
	switch {
	case a.Connection == "" && strings.TrimSpace(a.URL) == "":
		return nil, errors.New(`missing required field "url" (or a connection with its baseUrl)`)
	case a.Connection == "" && !dynamic(a.URL):
		if _, err := parseURL(a.URL); err != nil {
			return nil, err
		}
	}
	if !dynamic(a.Timeout) && a.Timeout != "" {
		if _, err := model.Duration(a.Timeout).Value(); err != nil {
			return nil, fmt.Errorf("timeout: %w", err)
		}
	}
	if a.RetryAfter != nil && !dynamic(a.RetryAfter.MaxWait) {
		if _, _, err := a.RetryAfter.limits(); err != nil {
			return nil, err
		}
	}
	if a.Paginate != nil {
		// A size given as ${…} or {{ … }} is checked when the action runs (R5b2).
		if err := a.Paginate.Check(); err != nil && (!errors.Is(err, pagination.ErrNeedsSize) || !sizeLater(with)) {
			return nil, err
		}
	}
	switch {
	case a.SaveTo != "" && a.Paginate != nil:
		return nil, errors.New("saveTo and paginate cannot be combined: paginate reads lists of items, saveTo saves one answer as a file")
	case a.SaveTo == "" && a.IfExists != "":
		return nil, errors.New("ifExists needs saveTo")
	}
	switch a.IfExists {
	case "", ifExistsReplace, ifExistsFail, ifExistsSkip:
	default:
		return nil, fmt.Errorf("invalid ifExists %q (expected replace, skip or fail)", a.IfExists)
	}
	if !dynamic(a.MaxSize) {
		if _, err := format.ParseSize("maxSize", a.MaxSize, defaultMaxSize); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// dynamic reports whether a value is only known when the action runs.
func dynamic(s string) bool {
	return strings.Contains(s, "{{") || strings.Contains(s, "${")
}

// sizeLater reports whether paginate.size is resolved only when the action runs.
func sizeLater(with map[string]any) bool {
	p, _ := with["paginate"].(map[string]any)
	s, ok := p["size"].(string)
	return ok && dynamic(s)
}

func parseURL(s string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("url: %q is not an address with http:// or https://", s)
	}
	return u, nil
}

func (a *requestAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	api, err := a.connect(ctx, ec)
	if err != nil {
		return action.Result{}, err
	}
	var u *url.URL
	if api != nil {
		u, err = api.resolve(a.URL)
	} else {
		u, err = parseURL(a.URL)
	}
	if err != nil {
		return action.Result{}, err
	}
	if len(a.Query) > 0 {
		q := u.Query()
		for k, v := range a.Query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}
	timeout := defaultTimeout
	if a.Timeout != "" {
		if timeout, err = model.Duration(a.Timeout).Value(); err != nil {
			return action.Result{}, fmt.Errorf("timeout: %w", err)
		}
	}
	maxSize, err := format.ParseSize("maxSize", a.MaxSize, defaultMaxSize)
	if err != nil {
		return action.Result{}, err
	}
	if a.SaveTo != "" && strings.TrimSpace(a.MaxSize) == "" {
		maxSize = defaultMaxFile
	}
	method := strings.ToUpper(a.Method)
	if method == "" {
		method = http.MethodGet
	}
	if _, _, err := encodeBody(a.Body); err != nil {
		return action.Result{}, err
	}

	c := &call{a: a, ec: ec, api: api, method: method, timeout: timeout, maxSize: maxSize}
	if a.Paginate != nil {
		return c.pages(ctx, u)
	}
	if a.SaveTo != "" {
		return c.download(ctx, u, paths.Resolve(ctx, a.SaveTo))
	}
	ans, err := c.fetch(ctx, u)
	if err != nil {
		return action.Result{}, err
	}
	out := map[string]any{"status": int64(ans.status), "headers": headers(ans.header), "body": decodeBody(ans.header.Get("Content-Type"), ans.data)}
	ec.Logf(slog.LevelInfo, "%s: %s (%d bytes in %s)", c.where(u), ans.text, len(ans.data), ans.duration.Round(time.Millisecond))
	if err := a.failed(c.where(u), ans); err != nil {
		return action.Result{Output: out}, err
	}
	return action.Result{Output: out}, nil
}

// failed returns the error of an answer with status 400 or above, unless failOnError
// is false.
func (a *requestAction) failed(where string, ans answer) error {
	if ans.status >= 400 && (a.FailOnError == nil || *a.FailOnError) {
		return fmt.Errorf("%s answered with status %s%s (set failOnError: false to go on)", where, ans.text, excerpt(ans.data))
	}
	return nil
}

// send sends one request: headers of the connection, of the action, then the sign-in.
func (a *requestAction) send(ctx context.Context, api *api, method string, u *url.URL) (*http.Response, error) {
	body, contentType, err := encodeBody(a.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("cannot send the request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("User-Agent", "Runnel")
	if api != nil {
		api.headers(req)
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}
	if api != nil {
		if err := api.signIn(ctx, req); err != nil {
			return nil, err
		}
	}
	return client.Do(req)
}

func sizeText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "10MB"
	}
	return s
}

// requestError explains a failed request without the details of Go's network errors.
func requestError(ctx context.Context, where string, timeout time.Duration, err error) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s: no answer within %s (timeout)", where, timeout)
	case ctx.Err() != nil:
		return ctx.Err()
	}
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}
	return fmt.Errorf("%s: %w", where, err)
}

// excerpt returns the beginning of an error answer for the message.
func excerpt(data []byte) string {
	s := strings.Join(strings.Fields(string(data)), " ")
	if s == "" {
		return ""
	}
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return ": " + s
}

// encodeBody returns the body to send and its content type.
func encodeBody(v any) (io.Reader, string, error) {
	switch v := v.(type) {
	case nil:
		return nil, "", nil
	case string:
		if v == "" {
			return nil, "", nil
		}
		ct := "text/plain; charset=utf-8"
		if t := strings.TrimSpace(v); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
			if json.Valid([]byte(t)) {
				ct = "application/json"
			}
		}
		return strings.NewReader(v), ct, nil
	}
	data, err := json.Marshal(record.JSONValue(v))
	if err != nil {
		return nil, "", fmt.Errorf("body: cannot write as JSON: %w", err)
	}
	return bytes.NewReader(data), "application/json", nil
}

// decodeBody reads an answer in JSON into a value; other answers stay text.
func decodeBody(contentType string, data []byte) any {
	media, _, _ := mime.ParseMediaType(contentType)
	if media == "application/json" || strings.HasSuffix(media, "+json") {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err == nil && !dec.More() {
			return httpx.Numbers(v)
		}
	}
	return string(data)
}

// headers returns the headers of the answer with lowercase names; several values are
// joined with ", ".
func headers(h http.Header) map[string]any {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(h))
	for _, k := range keys {
		out[strings.ToLower(k)] = strings.Join(h[k], ", ")
	}
	return out
}
