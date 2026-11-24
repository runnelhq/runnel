package expr

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"
	"sync"
)

// Regular expressions (S69): RE2 syntax of Go, matched in linear time (no catastrophic
// backtracking). Compiled patterns are kept, as a pattern usually stays the same for
// every record.

const (
	maxPatternLen = 4096 // characters of a pattern
	maxPatterns   = 256  // compiled patterns kept; then the cache starts anew
)

var patterns = struct {
	sync.Mutex
	m map[string]*regexp.Regexp
}{m: map[string]*regexp.Regexp{}}

// compilePattern returns the compiled pattern, from the cache if it was used before.
func compilePattern(s string) (*regexp.Regexp, error) {
	patterns.Lock()
	re, ok := patterns.m[s]
	patterns.Unlock()
	if ok {
		return re, nil
	}
	if len(s) > maxPatternLen {
		return nil, fmt.Errorf("replaceRegex: the pattern is longer than %d characters", maxPatternLen)
	}
	for _, look := range []string{"(?=", "(?!", "(?<=", "(?<!"} {
		if strings.Contains(s, look) {
			return nil, fmt.Errorf("replaceRegex: invalid pattern %q: lookahead and lookbehind like %s…) are not supported; use a group and $1 instead", s, look)
		}
	}
	re, err := regexp.Compile(s)
	if err != nil {
		if se, ok := errors.AsType[*syntax.Error](err); ok {
			return nil, fmt.Errorf("replaceRegex: invalid pattern %q: %s", s, se.Code)
		}
		return nil, fmt.Errorf("replaceRegex: invalid pattern %q", s)
	}
	patterns.Lock()
	if len(patterns.m) >= maxPatterns {
		patterns.m = map[string]*regexp.Regexp{}
	}
	patterns.m[s] = re
	patterns.Unlock()
	return re, nil
}

// replaceRegex replaces every match of pattern in text; the replacement may name groups
// with $1 or ${name} ($$ for a $).
func replaceRegex(p ...any) (any, error) {
	re, err := compilePattern(toString(p[1]))
	if err != nil {
		return nil, err
	}
	return re.ReplaceAllString(toString(p[0]), toString(p[2])), nil
}
