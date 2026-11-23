package pagination

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLookup(t *testing.T) {
	body := map[string]any{"@odata.nextLink": "https://x/2", "data": map[string]any{"items": []any{1}, "meta.v": "a"}}
	for path, want := range map[string]any{"@odata.nextLink": "https://x/2", "data.meta.v": "a", "data.items": []any{1}} {
		got, ok := Lookup(body, path)
		if !ok || !equal(got, want) {
			t.Errorf("%s: %v %v", path, got, ok)
		}
	}
	for _, path := range []string{"data.missing", "data.items.x", "nope"} {
		if _, ok := Lookup(body, path); ok {
			t.Errorf("%s found", path)
		}
	}
}

func equal(a, b any) bool {
	if l, ok := a.([]any); ok {
		m, ok := b.([]any)
		return ok && len(l) == len(m)
	}
	return a == b
}

func TestNextLink(t *testing.T) {
	h := http.Header{}
	h.Add("Link", `<https://api.github.com/repos/a/b/issues?page=1>; rel="prev", <https://api.github.com/repos/a/b/issues?page=3&x=a,b>; rel="next last"`)
	if got := nextLink(h); got != "https://api.github.com/repos/a/b/issues?page=3&x=a,b" {
		t.Errorf("%q", got)
	}
	if got := nextLink(http.Header{"Link": {`<https://x/1>; rel="first"`}}); got != "" {
		t.Errorf("%q", got)
	}
}

// walk runs a pager over prepared pages: each page has its items and an optional body
// field next and a Link header.
func walk(t *testing.T, c Config, first string, pages []map[string]any) ([]string, error) {
	t.Helper()
	p, u := New(c, mustURL(t, first))
	var urls []string
	for i := 0; u != nil; i++ {
		urls = append(urls, u.String())
		if i >= len(pages) {
			t.Fatalf("asked for page %d: %v", i+1, urls)
		}
		page := pages[i]
		header := http.Header{}
		if l, ok := page["link"].(string); ok {
			header.Set("Link", "<"+l+`>; rel="next"`)
		}
		items, err := p.Items(page["body"])
		if err != nil {
			return urls, err
		}
		if u, err = p.Next(u, header, page["body"], items); err != nil {
			return urls, err
		}
	}
	return urls, nil
}

func list(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func TestStyles(t *testing.T) {
	start := 0
	cases := []struct {
		name  string
		c     Config
		pages []map[string]any
		want  string
	}{
		{"link", Config{Style: "link"}, []map[string]any{
			{"body": list(2), "link": "/api/items?page=2"},
			{"body": list(1)},
		}, "https://h/api/items?a=1 https://h/api/items?page=2"},
		{"url", Config{Style: "url", Items: "value", Next: "@odata.nextLink"}, []map[string]any{
			{"body": map[string]any{"value": list(2), "@odata.nextLink": "https://h/api/items?$skiptoken=X"}},
			{"body": map[string]any{"value": list(1), "@odata.nextLink": nil}},
		}, "https://h/api/items?a=1 https://h/api/items?$skiptoken=X"},
		{"cursor", Config{Style: "cursor", Items: "data", Next: "meta.next", Param: "after"}, []map[string]any{
			{"body": map[string]any{"data": list(2), "meta": map[string]any{"next": "c2"}}},
			{"body": map[string]any{"data": list(2), "meta": map[string]any{"next": ""}}},
		}, "https://h/api/items?a=1 https://h/api/items?a=1&after=c2"},
		{"page with size", Config{Style: "page", Size: 2, SizeParam: "per_page"}, []map[string]any{
			{"body": list(2)}, {"body": list(2)}, {"body": list(1)},
		}, "https://h/api/items?a=1&page=1&per_page=2 https://h/api/items?a=1&page=2&per_page=2 https://h/api/items?a=1&page=3&per_page=2"},
		{"page until empty", Config{Style: "page", Param: "p"}, []map[string]any{
			{"body": list(3)}, {"body": list(0)},
		}, "https://h/api/items?a=1&p=1 https://h/api/items?a=1&p=2"},
		{"offset", Config{Style: "offset", Param: "skip", Start: &start, Size: 3}, []map[string]any{
			{"body": list(3)}, {"body": list(2)},
		}, "https://h/api/items?a=1&skip=0 https://h/api/items?a=1&skip=3"},
	}
	for _, c := range cases {
		if err := c.c.Check(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		urls, err := walk(t, c.c, "https://h/api/items?a=1", c.pages)
		if err != nil || strings.Join(urls, " ") != c.want {
			t.Errorf("%s: %v %v\nwant %s", c.name, urls, err, c.want)
		}
	}
}

func TestRefusals(t *testing.T) {
	cases := []struct {
		name  string
		c     Config
		pages []map[string]any
		want  string
	}{
		{"other host", Config{Style: "link"}, []map[string]any{{"body": list(1), "link": "https://evil.example.com/steal"}}, "the next page is on https://evil.example.com, not on https://h"},
		{"other scheme", Config{Style: "url", Items: "d", Next: "next"}, []map[string]any{{"body": map[string]any{"d": list(1), "next": "http://h/2"}}}, "is on http://h"},
		{"loop", Config{Style: "cursor", Items: "d", Next: "c"}, []map[string]any{
			{"body": map[string]any{"d": list(1), "c": "x"}}, {"body": map[string]any{"d": list(1), "c": "x"}},
		}, "page 2 leads to a page read before"},
		{"max pages", Config{Style: "page", MaxPages: 2}, []map[string]any{{"body": list(1)}, {"body": list(1)}, {"body": list(1)}}, "more than 2 pages"},
		{"max items", Config{Style: "page", MaxItems: 3}, []map[string]any{{"body": list(2)}, {"body": list(2)}}, "more than 3 items"},
		{"no list", Config{Style: "link", Items: "data"}, []map[string]any{{"body": map[string]any{"data": "x"}}}, `"data" of page 1 is not a list`},
		{"missing items", Config{Style: "link", Items: "data"}, []map[string]any{{"body": map[string]any{}}}, `page 1 has no "data"`},
	}
	for _, c := range cases {
		if _, err := walk(t, c.c, "https://h/api", c.pages); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want %q", c.name, err, c.want)
		}
	}
}

func TestCheck(t *testing.T) {
	for _, c := range []struct {
		c    Config
		want string
	}{
		{Config{Style: "scroll"}, `invalid style "scroll"`},
		{Config{Style: "url"}, "style url needs next (the path to the address in the body)"},
		{Config{Style: "cursor"}, "style cursor needs next (the path to the cursor in the body)"},
		{Config{Style: "page", SizeParam: "limit"}, "sizeParam needs size"},
		{Config{Style: "page", MaxPages: -1}, "must not be negative"},
	} {
		if err := c.c.Check(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: error %v, want %q", c.c, err, c.want)
		}
	}
}
