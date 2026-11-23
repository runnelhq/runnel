package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
	"github.com/runnelhq/runnel/internal/record"
)

func TestRequestConfig(t *testing.T) {
	actiontest.ConfigError(t, requestDefinition, nil, `missing required field "url"`)
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "ftp://host/x"}, `url: "ftp://host/x" is not an address with http:// or https://`)
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "api.example.com"}, "is not an address")
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "method": "FETCH"}, `invalid method "FETCH"`)
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "timeout": "soon"}, "timeout:")
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "maxSize": "huge"}, `maxSize: "huge" is not a size`)
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "failOnError": "no"}, `field "failOnError"`)
	actiontest.New(t, requestDefinition, map[string]any{"url": "${API_URL}/orders", "method": "post", "timeout": "{{ params.T }}"})
}

func request(t *testing.T, with map[string]any) (map[string]any, []actiontest.Logged, error) {
	t.Helper()
	res, logs, err := actiontest.Execute(context.Background(), actiontest.New(t, requestDefinition, with))
	out, _ := res.Output.(map[string]any)
	return out, logs, err
}

func TestRequestJSON(t *testing.T) {
	var got *http.Request
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Total", "2")
		_, _ = fmt.Fprint(w, `{"items":[{"id":1,"amount":12.5},{"id":9007199254740993}],"next":null}`)
	}))
	defer srv.Close()

	out, logs, err := request(t, map[string]any{
		"method": "post", "url": srv.URL + "/orders?a=1",
		"query":   map[string]any{"status": "open & paid"},
		"headers": map[string]any{"Authorization": "Bearer secret-token"},
		"body":    map[string]any{"name": "Anna", "tags": []any{"a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || got.URL.RawQuery != "a=1&status=open+%26+paid" || got.Header.Get("Authorization") != "Bearer secret-token" ||
		got.Header.Get("Content-Type") != "application/json" || gotBody != `{"name":"Anna","tags":["a"]}` {
		t.Errorf("request %s %s %v %q", got.Method, got.URL, got.Header, gotBody)
	}
	if out["status"] != int64(200) || out["headers"].(map[string]any)["x-total"] != "2" {
		t.Errorf("output %v", out)
	}
	items := out["body"].(map[string]any)["items"].([]any)
	if items[0].(map[string]any)["amount"] != 12.5 || items[1].(map[string]any)["id"] != int64(9007199254740993) {
		t.Errorf("body %v", out["body"])
	}
	// The log names neither the query nor the headers.
	if len(logs) != 1 || !strings.HasPrefix(logs[0].Msg, "POST "+srv.URL+"/orders: 200 OK (") || strings.Contains(logs[0].Msg, "secret") {
		t.Errorf("logs %v", logs)
	}
}

// Values of the run in a JSON body keep their type: decimals and large whole numbers
// are exact numbers, dates ISO text.
func TestRequestBodyValues(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	}))
	defer srv.Close()
	day, _ := record.ParseDate("2026-11-23")
	for _, tt := range []struct {
		body any
		want string
	}{
		{map[string]any{"amount": decimal.RequireFromString("12.50"), "id": int64(9007199254740993), "day": day}, `{"amount":12.5,"day":"2026-11-23","id":9007199254740993}`},
		{[]any{decimal.RequireFromString("0.1"), decimal.RequireFromString("0.2")}, `[0.1,0.2]`},
		{decimal.RequireFromString("1.25"), `1.25`},
	} {
		if _, _, err := request(t, map[string]any{"method": "post", "url": srv.URL, "body": tt.body}); err != nil || gotBody != tt.want {
			t.Errorf("%v: got %s %v, want %s", tt.body, gotBody, err, tt.want)
		}
	}
}

func TestRequestText(t *testing.T) {
	var ct, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		ct, body = r.Header.Get("Content-Type"), string(b)
		_, _ = fmt.Fprint(w, "pong")
	}))
	defer srv.Close()
	for text, want := range map[string]string{"ping": "text/plain; charset=utf-8", ` {"a": 1}`: "application/json"} {
		out, _, err := request(t, map[string]any{"method": "PUT", "url": srv.URL, "body": text})
		if err != nil || out["body"] != "pong" || ct != want || body != text {
			t.Errorf("%q: %v %v, content type %q, body %q", text, out, err, ct, body)
		}
	}
	// Not JSON despite the header: the text stays.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, "{broken")
	}))
	defer srv2.Close()
	if out, _, err := request(t, map[string]any{"url": srv2.URL}); err != nil || out["body"] != "{broken" {
		t.Errorf("%v %v", out, err)
	}
}

func TestRequestErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, "  customer\n 4711 not found ")
	}))
	defer srv.Close()
	out, _, err := request(t, map[string]any{"url": srv.URL + "/customers/4711?key=abc"})
	want := "GET " + srv.URL + "/customers/4711 answered with status 404 Not Found: customer 4711 not found (set failOnError: false to go on)"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v\nwant %s", err, want)
	}
	if out["status"] != int64(404) {
		t.Errorf("output %v", out)
	}
	out, _, err = request(t, map[string]any{"url": srv.URL, "failOnError": false})
	if err != nil || out["status"] != int64(404) {
		t.Errorf("failOnError false: %v %v", out, err)
	}
}

func TestRequestLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", 3000)))
	}))
	defer srv.Close()
	if _, _, err := request(t, map[string]any{"url": srv.URL + "/big", "maxSize": "2KB"}); err == nil ||
		err.Error() != "GET "+srv.URL+"/big: the answer is larger than 2KB; set maxSize if this is intended" {
		t.Errorf("err = %v", err)
	}
	if out, _, err := request(t, map[string]any{"url": srv.URL + "/big", "maxSize": "3KB"}); err != nil || len(out["body"].(string)) != 3000 {
		t.Errorf("3KB: %v", err)
	}
	start := time.Now()
	if _, _, err := request(t, map[string]any{"url": srv.URL + "/slow", "timeout": "200ms"}); err == nil ||
		err.Error() != "GET "+srv.URL+"/slow: no answer within 200ms (timeout)" {
		t.Errorf("err = %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("timeout took %s", d)
	}
}

func TestRequestCancelledAndUnreachable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := actiontest.New(t, requestDefinition, map[string]any{"url": "http://127.0.0.1:1/x"})
	if _, _, err := actiontest.Execute(ctx, a); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, _, err := request(t, map[string]any{"url": url + "/x"})
	if err == nil || !strings.HasPrefix(err.Error(), "GET "+url+"/x: ") || strings.Contains(err.Error(), "Get \"") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestSizeAndHeaders(t *testing.T) {
	for text, want := range map[string]int64{"": defaultMaxSize, "500KB": 500 << 10, "1 mb": 1 << 20, "100": 100} {
		if got, err := parseSize(text); err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v", text, got, err)
		}
	}
	h := headers(http.Header{"Set-Cookie": {"a=1", "b=2"}, "Content-Type": {"text/plain"}})
	if h["set-cookie"] != "a=1, b=2" || h["content-type"] != "text/plain" {
		t.Errorf("headers %v", h)
	}
}

func BenchmarkRequest(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"items":[`+strings.Repeat(`{"id":1,"name":"x","amount":1.5},`, 99)+`{"id":1}]}`)
	}))
	defer srv.Close()
	a, err := requestDefinition.New(map[string]any{"url": srv.URL, "query": map[string]any{"page": "1"}})
	if err != nil {
		b.Fatal(err)
	}
	ec := &action.ExecContext{Log: func(_ slog.Level, _ string) {}}
	b.ReportAllocs()
	for range b.N {
		if _, err := a.Execute(context.Background(), ec); err != nil {
			b.Fatal(err)
		}
	}
}
