package logfile

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

// Formats of the server log (S71).
const (
	FormatText = "text"
	FormatJSON = "json"
)

// Options configure a handler of the server log.
type Options struct {
	// Format is text (default) or json.
	Format string
	// Level is the least level written.
	Level slog.Level
	// TimeLayout formats the time in the text format, e.g. "15:04:05".
	TimeLayout string
}

// ParseLevel reads a level of --log-level: debug, info, warn or error.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("invalid log level %q (expected debug, info, warn or error)", s)
}

// CheckFormat checks a format of --log-format.
func CheckFormat(s string) error {
	if s == "" || s == FormatText || s == FormatJSON {
		return nil
	}
	return fmt.Errorf("invalid log format %q (expected text or json)", s)
}

// NewHandler returns a handler that writes one line per message to w: in the text
// format "<time> <LEVEL> <message> key=value …" as before (S41), in json one object
// with time, level, msg and the attributes (for Splunk, ELK or Graylog). Line breaks in
// messages and values never start a new line, so that a run cannot fake log lines.
func NewHandler(w io.Writer, o Options) slog.Handler {
	if o.Format == FormatJSON {
		return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: o.Level})
	}
	layout := o.TimeLayout
	if layout == "" {
		layout = time.DateTime
	}
	return &textHandler{w: w, level: o.Level, layout: layout, mu: &sync.Mutex{}}
}

// textHandler writes a message per line, like the server log before S71.
type textHandler struct {
	w      io.Writer
	level  slog.Level
	layout string
	attrs  []slog.Attr
	group  string
	mu     *sync.Mutex
}

func (h *textHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *textHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Time.Format(h.layout))
	fmt.Fprintf(&b, " %-5s ", levelName(r.Level))
	b.WriteString(oneLine(r.Message))
	write := func(a slog.Attr) {
		v := oneLine(a.Value.String())
		if v == "" || strings.ContainsAny(v, " \"=") {
			v = fmt.Sprintf("%q", v)
		}
		b.WriteString(" " + a.Key + "=" + v)
	}
	for _, a := range h.attrs { // qualified when added (WithAttrs)
		write(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		if a = h.qualify(a); !a.Equal(slog.Attr{}) {
			write(a)
		}
		return true
	})
	b.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = slices.Clip(h.attrs)
	for _, a := range attrs {
		if a = h.qualify(a); !a.Equal(slog.Attr{}) {
			c.attrs = append(c.attrs, a)
		}
	}
	return &c
}

// qualify resolves an attribute and puts the current group before its key.
func (h *textHandler) qualify(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return a
	}
	if h.group != "" {
		a.Key = h.group + "." + a.Key
	}
	return a
}

func (h *textHandler) WithGroup(name string) slog.Handler {
	c := *h
	if c.group != "" {
		name = c.group + "." + name
	}
	c.group = name
	return &c
}

// levelName is the level as before S71: INFO, WARN, ERROR, DEBUG.
func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	case l >= slog.LevelInfo:
		return "INFO"
	}
	return "DEBUG"
}

// oneLine replaces line breaks by \n and \r.
func oneLine(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(s)
}
