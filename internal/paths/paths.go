// Package paths resolves relative file paths of actions and transforms against the base
// folder of a run (ADR 0020): the project folder, or without a project the folder of
// the workflow or pipeline file. The engines put the base into the context of a run.
package paths

import (
	"context"
	"path/filepath"
)

type baseKey struct{}

// WithBase returns a context in which relative paths refer to dir; an empty dir keeps ctx.
func WithBase(ctx context.Context, dir string) context.Context {
	if dir == "" {
		return ctx
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return context.WithValue(ctx, baseKey{}, dir)
}

// Base returns the base folder of ctx, or "" if none is set.
func Base(ctx context.Context) string {
	dir, _ := ctx.Value(baseKey{}).(string)
	return dir
}

// Resolve returns p as it is if it is absolute (also "C:\…", "\\server\share\…" and
// "\…" on Windows) or empty, and otherwise joined with the base folder of ctx. Without
// a base, p stays relative (to the working directory of runnel).
func Resolve(ctx context.Context, p string) string {
	base := Base(ctx)
	if p == "" || base == "" || filepath.IsAbs(p) || filepath.VolumeName(p) != "" || rooted(p) {
		return p
	}
	return filepath.Join(base, p)
}

// rooted reports a path starting with a separator: absolute on Linux, relative to the
// current drive on Windows ("\data"); both stay as they are.
func rooted(p string) bool {
	return p[0] == '/' || p[0] == '\\'
}
