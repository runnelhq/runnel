package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestIsLoopback(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "LOCALHOST:7300": true, "127.0.0.1": true, "127.0.0.1:7301": true, "127.1.2.3": true,
		"[::1]:7300": true, "::1": true,
		"example.com": false, "evil.example:7300": false, "192.0.2.10": false, "0.0.0.0:7301": false, "": false,
	} {
		if got := IsLoopback(host); got != want {
			t.Errorf("%q: %v", host, got)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, 418, ErrorBody{"short"}, false)
	if w.Code != 418 || w.Body.String() != "{\"error\":\"short\"}\n" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("%d %q", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	WriteJSON(w, 200, map[string]int{"a": 1}, true)
	if w.Body.String() != "{\n  \"a\": 1\n}\n" {
		t.Errorf("indented %q", w.Body)
	}
}
