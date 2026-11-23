package http

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

// limited answers the first requests with status and Retry-After, then 200.
func limited(t *testing.T, refusals int32, status int, after string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= refusals {
			if after != "" {
				w.Header().Set("Retry-After", after)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		_, _ = w.Write([]byte("done"))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestRetryAfter(t *testing.T) {
	srv, calls := limited(t, 2, http.StatusTooManyRequests, "0")
	out, logs, err := request(t, map[string]any{"url": srv.URL})
	if err != nil || out["body"] != "done" || calls.Load() != 3 {
		t.Fatalf("%v %v, %d calls", out, err, calls.Load())
	}
	if !strings.Contains(logs[0].Msg, "429 Too Many Requests, waiting 0s (repeat 1 of 3)") {
		t.Errorf("%v", logs)
	}

	// A real wait of one second, and 503 with an HTTP date.
	srv, calls = limited(t, 1, http.StatusServiceUnavailable, "1")
	start := time.Now()
	if _, _, err := request(t, map[string]any{"url": srv.URL}); err != nil || calls.Load() != 2 || time.Since(start) < time.Second {
		t.Fatalf("%v, %d calls after %s", err, calls.Load(), time.Since(start))
	}
	srv, calls = limited(t, 1, http.StatusServiceUnavailable, time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	if _, _, err := request(t, map[string]any{"url": srv.URL}); err != nil || calls.Load() != 2 {
		t.Fatalf("date: %v, %d calls", err, calls.Load())
	}
}

func TestRetryAfterLimits(t *testing.T) {
	cases := []struct {
		name      string
		refusals  int32
		after     string
		with      map[string]any
		want      string
		wantCalls int32
	}{
		{"too many", 5, "0", nil, "answered with status 429 Too Many Requests: slow down", 4},
		{"switched off", 1, "0", map[string]any{"retryAfter": map[string]any{"maxRetries": 0}}, "status 429", 1},
		{"no header", 1, "", nil, "status 429", 1},
		{"too long", 1, "120", nil, "asks to wait 2m0s, longer than retryAfter.maxWait (1m0s)", 1},
		{"own limit", 1, "5", map[string]any{"retryAfter": map[string]any{"maxWait": "2s"}}, "asks to wait 5s, longer than retryAfter.maxWait (2s)", 1},
	}
	for _, c := range cases {
		srv, calls := limited(t, c.refusals, http.StatusTooManyRequests, c.after)
		with := map[string]any{"url": srv.URL}
		maps.Copy(with, c.with)
		if _, _, err := request(t, with); err == nil || !strings.Contains(err.Error(), c.want) || calls.Load() != c.wantCalls {
			t.Errorf("%s: error %v, %d calls; want %q, %d calls", c.name, err, calls.Load(), c.want, c.wantCalls)
		}
	}
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "retryAfter": map[string]any{"maxRetries": -1}}, "maxRetries must not be negative")
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "retryAfter": map[string]any{"maxWait": "later"}}, "retryAfter: maxWait")
}

func TestRetryAfterCancelled(t *testing.T) {
	srv, _ := limited(t, 1, http.StatusTooManyRequests, "30")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := actiontest.Execute(ctx, actiontest.New(t, requestDefinition, map[string]any{"url": srv.URL}))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("%v after %s", err, time.Since(start))
	}
}

func TestRetryAfterParse(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	for value, want := range map[string]time.Duration{"0": 0, " 12 ": 12 * time.Second, "Mon, 23 Nov 2026 12:00:30 GMT": 30 * time.Second, "Mon, 23 Nov 2026 11:00:00 GMT": 0} {
		if got, ok := retryAfter(value, now); !ok || got != want {
			t.Errorf("%q: %s %v, want %s", value, got, ok, want)
		}
	}
	for _, value := range []string{"", "-1", "soon", "1.5"} {
		if _, ok := retryAfter(value, now); ok {
			t.Errorf("%q accepted", value)
		}
	}
}

// pagedAPI serves 5 items with the given page size like GitHub (Link header) and
// answers the second request with 429 once.
func pagedAPI(t *testing.T) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		size, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		if page == 9 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var items []string
		for i := (page-1)*size + 1; i <= min(page*size, 5); i++ {
			items = append(items, fmt.Sprintf(`{"id":%d}`, i))
		}
		if page*size < 5 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/items?page=%d&per_page=%d>; rel="next"`, srv.URL, page+1, size))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPaginate(t *testing.T) {
	srv := pagedAPI(t)
	out, logs, err := request(t, map[string]any{"url": srv.URL + "/items", "paginate": map[string]any{"style": "link", "size": 2, "sizeParam": "per_page"}})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := out["items"].([]any)
	if out["count"] != int64(5) || out["pages"] != int64(3) || len(items) != 5 || items[4].(map[string]any)["id"] != int64(5) || out["body"] != nil {
		t.Fatalf("%v", out)
	}
	if last := logs[len(logs)-1].Msg; !strings.HasSuffix(strings.Split(last, " in ")[0], "/items: 5 items on 3 pages") {
		t.Errorf("%v", logs)
	}
}

func TestPaginateFailedPage(t *testing.T) {
	// Page 1 has one item, page 2 fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1}]`))
	}))
	t.Cleanup(srv.Close)
	paginate := map[string]any{"style": "page", "size": 1}
	out, _, err := request(t, map[string]any{"url": srv.URL, "failOnError": false, "paginate": paginate})
	if err != nil || out["status"] != int64(500) || out["count"] != int64(1) || out["pages"] != int64(1) {
		t.Fatalf("%v %v", out, err)
	}
	if _, _, err = request(t, map[string]any{"url": srv.URL, "paginate": paginate}); err == nil || !strings.Contains(err.Error(), "answered with status 500") {
		t.Fatalf("%v", err)
	}
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "paginate": map[string]any{"style": "cursor"}}, "style cursor needs next")
	actiontest.ConfigError(t, requestDefinition, map[string]any{"url": "https://h", "paginate": map[string]any{}}, `missing required field "paginate.style"`)
}
