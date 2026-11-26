package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/vars"
)

// HTTP methods of http triggers (ADR 0018).
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// Responses of http triggers.
const (
	// ResponseAccepted answers 202 with the run id at once (S38a).
	ResponseAccepted = "accepted"
	// ResponseRun waits for the answer of the run (S38b1).
	ResponseRun = "run"
)

// DefaultResponseTimeout is how long a caller waits for the answer of a run unless
// timeout is given (response: run).
const DefaultResponseTimeout = 30 * time.Second

// Values of history: which runs of a trigger (and of runnel run, S26b) are kept when
// they have ended (S38b2, S26c).
const (
	HistoryAll    = "all"
	HistoryErrors = "errors"
	HistoryNone   = "none"
)

// HistoryMode returns which runs of a trigger are kept: history, or all; for http
// triggers with response: run errors.
func (t *Trigger) HistoryMode() string {
	switch {
	case t.With.History != "":
		return t.With.History
	case t.WaitsForRun():
		return HistoryErrors
	}
	return HistoryAll
}

// WaitsForRun reports whether the caller of an http trigger waits for the answer of
// the run (response: run).
func (t *Trigger) WaitsForRun() bool {
	return t.With.Response == ResponseRun
}

// ResponseTimeout returns how long the caller waits for the answer (response: run).
func (t *Trigger) ResponseTimeout() time.Duration {
	if d, err := t.With.Timeout.Value(); err == nil && d > 0 {
		return d
	}
	return DefaultResponseTimeout
}

var (
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// tokenRef returns the reference that holds the token of an http trigger: exactly one
// ${env:NAME} or ${secret:[STORE/]NAME} and nothing else.
func tokenRef(s string) (vars.Ref, bool) {
	refs, err := vars.Parse(s)
	if err != nil || len(refs) != 1 || refs[0].String() != s {
		return vars.Ref{}, false
	}
	r := refs[0]
	switch {
	case r.Kind == vars.KindEnv && envNamePattern.MatchString(r.Name), r.Kind == vars.KindSecret:
		return r, true
	}
	return vars.Ref{}, false
}

func (t *Trigger) tokenText() string {
	m, ok := t.With.Auth.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m["token"].(string)
	return s
}

// HTTPMethod returns the method of an http trigger in capitals.
func (t *Trigger) HTTPMethod() string {
	return strings.ToUpper(t.With.Method)
}

// Segment is a part of the path of an http trigger: a fixed text or a placeholder.
type Segment struct {
	Text        string
	Placeholder bool // Text is the name of the parameter
}

// Segments returns the parts of the path of an http trigger; nil for "/".
func (t *Trigger) Segments() []Segment {
	var out []Segment
	for part := range strings.SplitSeq(strings.Trim(t.With.Path, "/"), "/") {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			out = append(out, Segment{Text: part[1 : len(part)-1], Placeholder: true})
		} else {
			out = append(out, Segment{Text: part})
		}
	}
	return out
}

// Placeholders returns the names of the placeholders in the path of an http trigger.
func (t *Trigger) Placeholders() []string {
	var names []string
	for _, s := range t.Segments() {
		if s.Placeholder {
			names = append(names, s.Text)
		}
	}
	return names
}

// AuthNone reports whether an http trigger accepts requests without a token.
func (t *Trigger) AuthNone() bool {
	s, ok := t.With.Auth.(string)
	return ok && s == "none"
}

// TokenVariable returns the environment variable that holds the token of an http
// trigger (auth: { token: ${env:NAME} }); empty with auth: none or a secret.
func (t *Trigger) TokenVariable() string {
	if r, ok := tokenRef(t.tokenText()); ok && r.Kind == vars.KindEnv {
		return r.Name
	}
	return ""
}

// HasToken reports whether an http trigger expects a token (not auth: none).
func (t *Trigger) HasToken() bool {
	_, ok := tokenRef(t.tokenText())
	return ok
}

// Token returns the token of an http trigger: from its environment variable
// (${env:NAME}) or its secret (${secret:…}, read with UseSecrets). The error says why
// there is none, without any value.
func (t *Trigger) Token() (string, error) {
	r, ok := tokenRef(t.tokenText())
	switch {
	case !ok:
		return "", errors.New("the trigger has no token")
	case r.Kind == vars.KindEnv:
		if v := os.Getenv(r.Name); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("environment variable %s with the token is not set; set it for the server (for a service: as a system variable, then restart it)", r.Name)
	case t.secrets == nil:
		return "", fmt.Errorf("secret %q: secrets are not available", r.Name)
	}
	v, err := t.secrets(r.Store, r.Name)
	if err == nil && v == "" {
		err = fmt.Errorf("secret %q is empty", r.Name)
	}
	return v, err
}

func (t *Trigger) checkHTTP() Errors {
	s, w := t.src, t.With
	var errs Errors
	switch {
	case w.Method == "":
		errs = append(errs, s.errorf("$.with", `missing required field "method" (GET, POST, PUT, PATCH or DELETE)`))
	case !slices.Contains(httpMethods, t.HTTPMethod()):
		errs = append(errs, s.errorf("$.with.method", "invalid method %q (expected GET, POST, PUT, PATCH or DELETE)", w.Method))
	}
	errs = append(errs, t.checkURLPath()...)
	errs = append(errs, t.checkAuth()...)
	switch w.Response {
	case "", ResponseAccepted:
		if w.Timeout != "" {
			errs = append(errs, s.errorf("$.with.timeout", "timeout belongs to response: run (with accepted the caller does not wait)"))
		}
	case ResponseRun:
		errs = append(errs, t.checkDuration("timeout", w.Timeout, false)...)
	default:
		errs = append(errs, s.errorf("$.with.response", "invalid response %q (expected accepted or run)", w.Response))
	}
	if w.MaxConcurrent != nil && *w.MaxConcurrent < 0 {
		errs = append(errs, s.errorf("$.with.maxConcurrent", "maxConcurrent must not be negative (0: no own limit)"))
	}
	return errs
}

func (t *Trigger) checkURLPath() Errors {
	s, p := t.src, t.With.Path
	switch {
	case p == "":
		return Errors{s.errorf("$.with", `missing required field "path" (the URL path, e.g. /hooks/import or /api/customers/{id})`)}
	case !strings.HasPrefix(p, "/"):
		return Errors{s.errorf("$.with.path", "path %q must start with /", p)}
	case p != "/" && strings.HasSuffix(p, "/"):
		return Errors{s.errorf("$.with.path", "path %q must not end with /", p)}
	}
	seen := map[string]bool{}
	for part := range strings.SplitSeq(p[1:], "/") {
		switch {
		case part == "" && p != "/":
			return Errors{s.errorf("$.with.path", "path %q has an empty part (//)", p)}
		case strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}"):
			name := part[1 : len(part)-1]
			if !vars.ValidName(name) {
				return Errors{s.errorf("$.with.path", "invalid placeholder %q in path %q (a parameter name in braces, e.g. {id})", part, p)}
			}
			if seen[name] {
				return Errors{s.errorf("$.with.path", "placeholder {%s} appears twice in path %q", name, p)}
			}
			seen[name] = true
		case part != "" && !segmentPattern.MatchString(part):
			return Errors{s.errorf("$.with.path", "invalid part %q in path %q (letters, digits, . _ ~ - or a whole part {name})", part, p)}
		}
	}
	return nil
}

func (t *Trigger) checkAuth() Errors {
	s := t.src
	const hint = "auth: none, or auth: { token: ${secret:NAME} } or { token: ${env:NAME} } with the token in a secret store or an environment variable"
	switch a := t.With.Auth.(type) {
	case nil:
		return Errors{s.errorf("$.with", `missing required field "auth" (%s)`, hint)}
	case string:
		if a != "none" {
			return Errors{s.errorf("$.with.auth", "invalid auth %q (expected %s)", a, hint)}
		}
	case map[string]any:
		tok, ok := a["token"].(string)
		if len(a) != 1 || !ok {
			return Errors{s.errorf("$.with.auth", "invalid auth (expected %s)", hint)}
		}
		if _, ok := tokenRef(tok); !ok {
			return Errors{s.errorf("$.with.auth.token", "the token must not be written into the file; write token: ${secret:NAME} (secret store) or token: ${env:NAME} (environment variable)")}
		}
	default:
		return Errors{s.errorf("$.with.auth", "invalid auth (expected %s)", hint)}
	}
	return nil
}

// routesConflict reports whether a request could match the routes of both http
// triggers: same method and number of parts, each part equal or a placeholder.
func routesConflict(a, b *Trigger) bool {
	if a.HTTPMethod() != b.HTTPMethod() {
		return false
	}
	sa, sb := a.Segments(), b.Segments()
	if len(sa) != len(sb) {
		return false
	}
	for i := range sa {
		if !sa[i].Placeholder && !sb[i].Placeholder && !strings.EqualFold(sa[i].Text, sb[i].Text) {
			return false
		}
	}
	return true
}

// checkRoutes leaves out http triggers whose routes conflict with an earlier one and
// reports them; disabled triggers count too, as enabling one must not break routing.
func checkRoutes(triggers []*Trigger) ([]*Trigger, Errors) {
	var errs Errors
	var http, out []*Trigger
	for _, t := range triggers {
		if t.Type == TriggerHTTP {
			if i := slices.IndexFunc(http, func(o *Trigger) bool { return routesConflict(t, o) }); i >= 0 {
				other := http[i]
				errs = append(errs, t.ErrorAt("$.with.path", "route %s conflicts with trigger %q (%s): %s; a request could match both",
					t.Describe(), other.Name, filepath.Base(other.File()), other.Describe()))
				continue
			}
			http = append(http, t)
		}
		out = append(out, t)
	}
	return out, errs
}
