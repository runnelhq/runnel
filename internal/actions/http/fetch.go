package http

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/model"
)

// retryAfterConfig is the option retryAfter of http.request (S63b): an API that answers
// 429 (too many requests) or 503 (unavailable) with Retry-After is asked again after the
// wait it names.
type retryAfterConfig struct {
	MaxRetries *int   `json:"maxRetries" doc:"Most repeats of one request after 429 or 503 with Retry-After; 0: none" schema:"default=3"`
	MaxWait    string `json:"maxWait" doc:"Longest wait the API may ask for; a longer one fails the action" schema:"format=runnel-duration,default=1m"`
}

const (
	defaultMaxRetries = 3
	defaultMaxWait    = time.Minute
)

func (c *retryAfterConfig) limits() (int, time.Duration, error) {
	retries, wait := defaultMaxRetries, defaultMaxWait
	if c == nil {
		return retries, wait, nil
	}
	if c.MaxRetries != nil {
		if *c.MaxRetries < 0 {
			return 0, 0, fmt.Errorf("retryAfter: maxRetries must not be negative")
		}
		retries = *c.MaxRetries
	}
	if c.MaxWait != "" {
		d, err := model.Duration(c.MaxWait).Value()
		if err != nil {
			return 0, 0, fmt.Errorf("retryAfter: maxWait: %w", err)
		}
		wait = d
	}
	return retries, wait, nil
}

// answer is a read answer of the API.
type answer struct {
	status   int
	text     string // e.g. "200 OK"
	header   http.Header
	data     []byte
	duration time.Duration
	// file is the temporary file a successful answer was saved to (saveTo), size its length.
	file string
	size int64
}

// call is one request of the action with everything that stays the same between pages.
type call struct {
	a       *requestAction
	ec      *action.ExecContext
	api     *api
	method  string
	timeout time.Duration
	maxSize int64
	// saveIn is the folder of the file of saveTo: a successful answer is written to a
	// temporary file there instead of being read into memory.
	saveIn, saveName string
}

// where names a request in messages: only scheme, host and path, as the query may hold
// keys.
func (c *call) where(u *url.URL) string {
	return c.method + " " + u.Scheme + "://" + u.Host + u.Path
}

// fetch sends a request and reads its answer, each attempt within the timeout. It asks
// again once with a new token after 401 (oauth2) and after 429 or 503 with Retry-After
// as long as retryAfter allows.
func (c *call) fetch(ctx context.Context, u *url.URL) (answer, error) {
	maxRetries, maxWait, err := c.a.RetryAfter.limits()
	if err != nil {
		return answer{}, err
	}
	where := c.where(u)
	newToken := c.api != nil
	for retries := 0; ; {
		ans, err := c.once(ctx, u, where)
		if err != nil {
			return answer{}, err
		}
		if newToken && c.api.refused(ans.status) {
			// The token may have been revoked or expired early: once more with a new one.
			newToken = false
			c.ec.Logf(slog.LevelDebug, "%s: 401 with the token of connection %s, getting a new one", where, c.api.name)
			continue
		}
		if ans.status != http.StatusTooManyRequests && ans.status != http.StatusServiceUnavailable {
			return ans, nil
		}
		wait, ok := retryAfter(ans.header.Get("Retry-After"), time.Now())
		if !ok || retries >= maxRetries {
			return ans, nil
		}
		if wait > maxWait {
			return ans, fmt.Errorf("%s answered with status %s and asks to wait %s, longer than retryAfter.maxWait (%s)", where, ans.text, wait, maxWait)
		}
		retries++
		c.ec.Logf(slog.LevelInfo, "%s: %s, waiting %s (repeat %d of %d)", where, ans.text, wait, retries, maxRetries)
		select {
		case <-ctx.Done():
			return answer{}, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// once sends one request within the timeout and reads the answer up to maxSize.
func (c *call) once(ctx context.Context, u *url.URL, where string) (answer, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	start := time.Now()
	resp, err := c.a.send(ctx, c.api, c.method, u)
	if err != nil {
		return answer{}, requestError(ctx, where, c.timeout, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if c.saveIn != "" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		file, size, err := c.save(resp.Body)
		if err != nil {
			return answer{}, requestError(ctx, where, c.timeout, err)
		}
		return answer{status: resp.StatusCode, text: resp.Status, header: resp.Header, file: file, size: size, duration: time.Since(start)}, nil
	}
	limit := c.maxSize
	if c.saveIn != "" {
		limit = min(limit, defaultMaxSize) // an error answer instead of the file
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return answer{}, requestError(ctx, where, c.timeout, err)
	}
	if int64(len(data)) > limit {
		return answer{}, fmt.Errorf("%s: the answer is larger than %s; set maxSize if this is intended", where, sizeText(c.a.MaxSize))
	}
	return answer{status: resp.StatusCode, text: resp.Status, header: resp.Header, data: data, duration: time.Since(start)}, nil
}

// retryAfter reads the header Retry-After: seconds or an HTTP date (RFC 9110, 10.2.3).
func retryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if s, err := strconv.Atoi(value); err == nil {
		if s < 0 {
			return 0, false
		}
		return time.Duration(s) * time.Second, true
	}
	t, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	return max(t.Sub(now), 0).Round(time.Second), true
}
