package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/runnelhq/runnel/internal/action"
)

// Values of ifExists with saveTo (S63d).
const (
	ifExistsReplace = "replace"
	ifExistsFail    = "fail"
	ifExistsSkip    = "skip"
)

// download saves a successful answer as the file target (saveTo, S63d): streamed into a
// temporary file next to it and renamed once complete, so that a failed or cancelled
// download leaves no half file. Error answers are read like without saveTo and are not
// saved. Output: status, headers, path and size instead of body.
func (c *call) download(ctx context.Context, u *url.URL, target string) (action.Result, error) {
	where := c.where(u)
	_, err := os.Stat(target)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return action.Result{}, fmt.Errorf("saveTo: %w", err)
	}
	switch {
	case exists && c.a.IfExists == ifExistsSkip:
		c.ec.Logf(slog.LevelInfo, "%s: not requested, %s exists (ifExists: skip)", where, target)
		return action.Result{Output: map[string]any{"path": target, "skipped": true}}, nil
	case exists && c.a.IfExists == ifExistsFail:
		return action.Result{}, fmt.Errorf("saveTo: %s exists (ifExists: fail)", target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return action.Result{}, fmt.Errorf("saveTo: %w", err)
	}
	c.saveIn = filepath.Dir(target)
	c.saveName = filepath.Base(target)
	ans, err := c.fetch(ctx, u)
	if err != nil {
		return action.Result{}, err
	}
	out := map[string]any{"status": int64(ans.status), "headers": headers(ans.header)}
	if ans.file == "" {
		// Not saved: an answer other than 2xx, e.g. 404; like without saveTo.
		out["body"] = decodeBody(ans.header.Get("Content-Type"), ans.data)
		c.ec.Logf(slog.LevelInfo, "%s: %s, nothing saved", where, ans.text)
		if err := c.a.failed(where, ans); err != nil {
			return action.Result{Output: out}, err
		}
		return action.Result{Output: out}, nil
	}
	if err := os.Rename(ans.file, target); err != nil {
		_ = os.Remove(ans.file)
		return action.Result{}, fmt.Errorf("saveTo: %w", err)
	}
	out["path"] = target
	out["size"] = ans.size
	c.ec.Logf(slog.LevelInfo, "%s: %s, saved %d bytes to %s in %s", where, ans.text, ans.size, target, ans.duration.Round(time.Millisecond))
	return action.Result{Output: out}, nil
}

// save writes a body to a temporary file in saveIn, at most maxSize bytes; on an error
// the file is removed.
func (c *call) save(body io.Reader) (string, int64, error) {
	tmp, err := os.CreateTemp(c.saveIn, "."+c.saveName+".*.part")
	if err != nil {
		return "", 0, err
	}
	n, err := io.Copy(tmp, io.LimitReader(body, c.maxSize+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > c.maxSize {
		err = fmt.Errorf("the file is larger than %s; set maxSize if this is intended", fileSizeText(c.a.MaxSize))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return "", 0, err
	}
	return tmp.Name(), n, nil
}

func fileSizeText(s string) string {
	if s == "" {
		return "1GB"
	}
	return s
}
