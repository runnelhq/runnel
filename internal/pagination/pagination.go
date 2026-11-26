// Package pagination reads the pages of a paged HTTP API (S63c): where the items of a
// page are and how the address of the next page is found. It holds no network code, so
// that the action http.request and later the transform http.input share it.
//
// Styles:
//   - link: the header Link with rel="next" (e.g. GitHub)
//   - url: the address of the next page in the body, e.g. @odata.nextLink (Microsoft Graph)
//   - cursor: a value in the body, sent as query parameter (e.g. Slack, Stripe)
//   - page: a page number as query parameter, counting up
//   - offset: the number of items read so far as query parameter
//
// Next addresses must stay on the scheme and host of the first request, so that the
// sign-in of a connection never goes elsewhere; an address or cursor seen before ends
// with an error instead of a loop.
package pagination

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Defaults of the limits.
const (
	DefaultMaxPages = 100
	DefaultMaxItems = 10000
)

// ErrNeedsSize is the error of Check for sizeParam without size.
var ErrNeedsSize = errors.New("paginate: sizeParam needs size")

// Config describes the paging of an API; it is the option paginate of http.request.
type Config struct {
	Style     string `json:"style" doc:"How the next page is found: link (header Link rel=next), url (address in the body), cursor (value in the body as query parameter), page (page number) or offset (items read so far)" schema:"required,enum=link|url|cursor|page|offset"`
	Items     string `json:"items" doc:"Path to the list of items in the body, e.g. data.items; empty: the body is the list" schema:"example=data"`
	Next      string `json:"next" doc:"Path in the body to the address of the next page (url) or to the cursor (cursor), e.g. @odata.nextLink or meta.next_cursor" schema:"example=meta.next_cursor,only=style:url|cursor"`
	Param     string `json:"param" doc:"Query parameter for the cursor, page number or offset; empty: cursor, page or offset" schema:"example=starting_after,only=style:cursor|page|offset"`
	Start     *int   `json:"start" doc:"First page number (page) or offset (offset); empty: 1 for page, 0 for offset" schema:"only=style:page|offset"`
	Size      int    `json:"size" doc:"Items per page; a shorter page is the last one; with sizeParam it is also sent" schema:"example=100"`
	SizeParam string `json:"sizeParam" doc:"Query parameter for the page size, e.g. per_page or limit" schema:"example=per_page"`
	MaxPages  int    `json:"maxPages" doc:"Most pages to read; more fail the action" schema:"default=100"`
	MaxItems  int    `json:"maxItems" doc:"Most items to read; more fail the action (use a pipeline for larger amounts)" schema:"default=10000"`
}

// Check reports options that do not fit together.
func (c *Config) Check() error {
	switch c.Style {
	case "link", "page", "offset":
	case "url", "cursor":
		if strings.TrimSpace(c.Next) == "" {
			return fmt.Errorf("paginate: style %s needs next (the path to the %s in the body)", c.Style, map[string]string{"url": "address", "cursor": "cursor"}[c.Style])
		}
	default:
		return fmt.Errorf("paginate: invalid style %q (expected link, url, cursor, page or offset)", c.Style)
	}
	if c.Size < 0 || c.MaxPages < 0 || c.MaxItems < 0 {
		return errors.New("paginate: size, maxPages and maxItems must not be negative")
	}
	if c.SizeParam != "" && c.Size == 0 {
		return ErrNeedsSize
	}
	return nil
}

// Pager walks through the pages of one call.
type Pager struct {
	c      Config
	first  *url.URL
	pages  int
	items  int
	number int // page number or offset of the current page
	seen   map[string]bool
}

// New starts paging at the address of the first request; it returns the address of
// the first page (with page number, offset and size set).
func New(c Config, first *url.URL) (*Pager, *url.URL) {
	if c.MaxPages == 0 {
		c.MaxPages = DefaultMaxPages
	}
	if c.MaxItems == 0 {
		c.MaxItems = DefaultMaxItems
	}
	p := &Pager{c: c, first: first, seen: map[string]bool{}}
	switch c.Style {
	case "page":
		p.number = 1
	case "offset":
		p.number = 0
	}
	if c.Start != nil {
		p.number = *c.Start
	}
	u := *first
	q := u.Query()
	if c.SizeParam != "" {
		q.Set(c.SizeParam, strconv.Itoa(c.Size))
	}
	if c.Style == "page" || c.Style == "offset" {
		q.Set(p.param(), strconv.Itoa(p.number))
	}
	u.RawQuery = q.Encode()
	p.seen[u.String()] = true
	return p, &u
}

func (p *Pager) param() string {
	if p.c.Param != "" {
		return p.c.Param
	}
	return p.c.Style
}

// Pages returns the number of pages read.
func (p *Pager) Pages() int { return p.pages }

// Items returns the items of a page. The limits of pages and items are checked here.
func (p *Pager) Items(body any) ([]any, error) {
	p.pages++
	if p.pages > p.c.MaxPages {
		return nil, fmt.Errorf("paginate: more than %d pages; raise maxPages if this is intended", p.c.MaxPages)
	}
	v := body
	if p.c.Items != "" {
		var ok bool
		if v, ok = Lookup(body, p.c.Items); !ok {
			return nil, fmt.Errorf("paginate: page %d has no %q", p.pages, p.c.Items)
		}
	}
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("paginate: %s of page %d is not a list", describe(p.c.Items), p.pages)
	}
	p.items += len(items)
	if p.items > p.c.MaxItems {
		return nil, fmt.Errorf("paginate: more than %d items; raise maxItems or use a pipeline for larger amounts", p.c.MaxItems)
	}
	return items, nil
}

func describe(path string) string {
	if path == "" {
		return "the body"
	}
	return fmt.Sprintf("%q", path)
}

// Next returns the address of the next page after one with the given address, headers,
// body and items; nil is the end.
func (p *Pager) Next(current *url.URL, header http.Header, body any, items []any) (*url.URL, error) {
	var next *url.URL
	switch p.c.Style {
	case "link":
		raw := nextLink(header)
		if raw == "" {
			return nil, nil
		}
		u, err := current.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("paginate: the header Link holds no valid address: %q", raw)
		}
		next = u
	case "url":
		raw := text(body, p.c.Next)
		if raw == "" {
			return nil, nil
		}
		u, err := current.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("paginate: %q holds no valid address", p.c.Next)
		}
		next = u
	case "cursor":
		cursor := text(body, p.c.Next)
		if cursor == "" || len(items) == 0 {
			return nil, nil
		}
		next = p.withParam(current, cursor)
	case "page", "offset":
		if len(items) == 0 || (p.c.Size > 0 && len(items) < p.c.Size) {
			return nil, nil
		}
		if p.c.Style == "page" {
			p.number++
		} else {
			p.number += len(items)
		}
		next = p.withParam(current, strconv.Itoa(p.number))
	}
	if !strings.EqualFold(next.Scheme, p.first.Scheme) || !strings.EqualFold(next.Host, p.first.Host) {
		return nil, fmt.Errorf("paginate: the next page is on %s://%s, not on %s://%s; it is not requested", next.Scheme, next.Host, p.first.Scheme, p.first.Host)
	}
	if p.seen[next.String()] {
		return nil, fmt.Errorf("paginate: page %d leads to a page read before; the API repeats itself", p.pages)
	}
	p.seen[next.String()] = true
	return next, nil
}

func (p *Pager) withParam(current *url.URL, value string) *url.URL {
	u := *current
	q := u.Query()
	q.Set(p.param(), value)
	u.RawQuery = q.Encode()
	return &u
}

// nextLink returns the address with rel="next" from Link headers (RFC 8288).
func nextLink(header http.Header) string {
	for _, h := range header.Values("Link") {
		for _, part := range splitLinks(h) {
			target, params, ok := strings.Cut(part, ";")
			target = strings.TrimSpace(target)
			if !ok || !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
				continue
			}
			for param := range strings.SplitSeq(params, ";") {
				name, value, _ := strings.Cut(strings.TrimSpace(param), "=")
				if strings.EqualFold(strings.TrimSpace(name), "rel") {
					for rel := range strings.FieldsSeq(strings.Trim(strings.TrimSpace(value), `"`)) {
						if strings.EqualFold(rel, "next") {
							return target[1 : len(target)-1]
						}
					}
				}
			}
		}
	}
	return ""
}

// splitLinks splits a Link header at the commas between links, not those in addresses.
func splitLinks(h string) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range h {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, h[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, h[start:])
}

// Lookup follows a path of names separated by dots in a JSON value. A name may itself
// contain dots (e.g. @odata.nextLink): the longest name present is taken first.
func Lookup(v any, path string) (any, bool) {
	for path != "" {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		found := false
		for end := len(path); end > 0; end = strings.LastIndex(path[:end], ".") {
			if x, ok := m[path[:end]]; ok {
				v, path, found = x, strings.TrimPrefix(path[end:], "."), true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return v, true
}

// text returns a value of the body as text: empty for missing, null and "".
func text(body any, path string) string {
	v, ok := Lookup(body, path)
	if !ok || v == nil {
		return ""
	}
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
