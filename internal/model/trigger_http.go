package model

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/runnelhq/runnel/internal/vars"
)

// HTTP methods of http triggers (ADR 0018).
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// Responses of http triggers.
const (
	// ResponseAccepted answers 202 with the run id at once (S38a).
	ResponseAccepted = "accepted"
	// ResponseRun waits for the answer of the run (S38b).
	ResponseRun = "run"
)

var (
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
	tokenPattern   = regexp.MustCompile(`^\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}$`)
)

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
	for _, part := range strings.Split(strings.Trim(t.With.Path, "/"), "/") {
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
// trigger (auth: { token: ${env:NAME} }); empty with auth: none.
func (t *Trigger) TokenVariable() string {
	m, ok := t.With.Auth.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m["token"].(string)
	if g := tokenPattern.FindStringSubmatch(s); g != nil {
		return g[1]
	}
	return ""
}

// Token returns the token of an http trigger from its environment variable; false if
// the variable is not set or empty.
func (t *Trigger) Token() (string, bool) {
	name := t.TokenVariable()
	if name == "" {
		return "", false
	}
	v := os.Getenv(name)
	return v, v != ""
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
	case ResponseRun:
		errs = append(errs, s.errorf("$.with.response", "response: run is not supported yet (roadmap S38b); use accepted"))
	default:
		errs = append(errs, s.errorf("$.with.response", "invalid response %q (expected accepted)", w.Response))
	}
	if w.MaxConcurrent < 0 {
		errs = append(errs, s.errorf("$.with.maxConcurrent", "maxConcurrent must not be negative (0: no own limit)"))
	}
	for field, set := range map[string]bool{"timeout": w.Timeout != "", "history": w.History != ""} {
		if set {
			errs = append(errs, s.errorf("$.with."+field, "%s belongs to response: run and is not supported yet (roadmap S38b)", field))
		}
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
	for _, part := range strings.Split(p[1:], "/") {
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
	const hint = "auth: none, or auth: { token: ${env:NAME} } with the token in an environment variable"
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
		if !tokenPattern.MatchString(tok) {
			return Errors{s.errorf("$.with.auth.token", "the token must not be written into the file; put it into an environment variable and write token: ${env:NAME}")}
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
