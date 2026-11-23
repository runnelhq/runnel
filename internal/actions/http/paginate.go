package http

import (
	"context"
	"log/slog"
	"net/url"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/pagination"
)

// pages reads all pages of a paged API (S63c) and returns their items together.
// Output: items, count, pages, and status and headers of the last answer.
func (c *call) pages(ctx context.Context, first *url.URL) (action.Result, error) {
	pager, next := pagination.New(*c.a.Paginate, first)
	start := time.Now()
	items := []any{}
	out := func(ans answer) map[string]any {
		return map[string]any{"status": int64(ans.status), "headers": headers(ans.header), "items": items,
			"count": int64(len(items)), "pages": int64(pager.Pages())}
	}
	for {
		ans, err := c.fetch(ctx, next)
		if err != nil {
			return action.Result{}, err
		}
		c.ec.Logf(slog.LevelDebug, "%s: %s (%d bytes in %s)", c.where(next), ans.text, len(ans.data), ans.duration.Round(time.Millisecond))
		if ans.status >= 400 {
			// Pages read so far stay in the output, e.g. with failOnError: false.
			return action.Result{Output: out(ans)}, c.a.failed(c.where(next), ans)
		}
		body := decodeBody(ans.header.Get("Content-Type"), ans.data)
		page, err := pager.Items(body)
		if err != nil {
			return action.Result{}, err
		}
		items = append(items, page...)
		if next, err = pager.Next(next, ans.header, body, page); err != nil {
			return action.Result{}, err
		}
		if next == nil {
			c.ec.Logf(slog.LevelInfo, "%s: %d items on %d %s in %s", c.where(first), len(items), pager.Pages(), plural(pager.Pages(), "page"), time.Since(start).Round(time.Millisecond))
			return action.Result{Output: out(ans)}, nil
		}
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
