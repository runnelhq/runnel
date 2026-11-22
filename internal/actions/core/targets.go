package core

import (
	"fmt"
	"path"
	"runtime"
	"strings"
)

// Targets restricts the workflows or pipelines a call may reach when its path is only
// known at run time (ADR 0008, Nachtrag S25b), e.g. [pipelines/import-*.pipeline.yaml].
// The patterns refer to the project folder, like the path; empty: every file of the
// project.

// checkTargets checks the patterns of allowedTargets; patterns with ${…} or {{ … }}
// are checked when the action runs.
func checkTargets(patterns []string) error {
	for _, p := range patterns {
		if strings.Contains(p, "${") || strings.Contains(p, "{{") {
			continue
		}
		if p == "" {
			return fmt.Errorf("allowedTargets: empty pattern")
		}
		if _, err := path.Match(slashed(p), ""); err != nil {
			return fmt.Errorf("allowedTargets: invalid pattern %q", p)
		}
	}
	return nil
}

// allowed checks a target against the patterns of allowedTargets; what is "workflow"
// or "pipeline".
func allowed(target string, patterns []string, what string) error {
	if len(patterns) == 0 {
		return nil
	}
	t := slashed(target)
	for _, p := range patterns {
		p = slashed(p)
		if runtime.GOOS == "windows" {
			t, p = strings.ToLower(t), strings.ToLower(p)
		}
		if ok, _ := path.Match(p, t); ok {
			return nil
		}
	}
	return fmt.Errorf("%s %s is not one of the allowed targets (%s)", what, target, strings.Join(patterns, ", "))
}

// slashed cleans a path for comparisons: / as separator, no ./ or ...
func slashed(p string) string {
	return path.Clean(strings.ReplaceAll(strings.TrimSpace(p), `\`, "/"))
}
