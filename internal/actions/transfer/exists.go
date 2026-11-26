package transfer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/runnelhq/runnel/internal/action"
)

// transfer.exists succeeds if a file or folder exists on the server of a connection and
// fails otherwise, like file.exists, so that the workflow can branch with success and
// failure links. Links count as what they point to. Output: the matching paths.
var existsDefinition = action.Definition{
	Type:        "transfer.exists",
	Category:    action.CategoryNetwork,
	Label:       "File Exists on Server",
	Description: "Checks whether files or folders exist on a server (SFTP, FTPS or FTP); output: the matching paths",
	Example:     "connection: partner-sftp\npath: /outgoing/orders-*.csv\ninclude: files\n",
	Config:      existsAction{},
	New:         newExists,
	Reference: action.Reference{
		Outputs: []action.OutputField{{Name: "output", Type: "list", Description: "Paths of the matching files or folders on the server"}},
		Errors:  "Fails when nothing of the kind matches the path, so that success and failure links can branch, and when the connection fails (host key, certificate or sign-in refused, no answer within timeout).",
		Limits: []string{
			"A path with wildcards may match at most 10,000 entries.",
			"timeout limits the check (default 10m).",
		},
	},
}

type existsAction struct {
	Connection string `json:"connection" doc:"Connection of the server (a file in connections/)" schema:"required,connection=sftp|ftps|ftp,example=partner-sftp"`
	Path       string `json:"path" doc:"File or folder on the server, e.g. /outgoing/*.csv (relative: to the folder after the sign-in); wildcards are allowed in the name and match case-sensitively" schema:"required,example=/outgoing/*.csv"`
	Include    string `json:"include" doc:"What has to exist: files, folders or all (both)" schema:"enum=all|files|folders,default=all"`
	Timeout    string `json:"timeout" doc:"Longest time for the check, e.g. 1m; the timeout of the action limits all together" schema:"format=runnel-duration,default=10m"`
}

func newExists(with map[string]any) (action.Action, error) {
	a := &existsAction{}
	if err := action.Renamed(with, "kind", "include: all, files or folders (as in transfer.list)"); err != nil {
		return nil, err
	}
	if err := action.DecodeConfig(with, a); err != nil {
		return nil, err
	}
	return a, checkChoice("include", a.Include, "all", "files", "folders")
}

func (a *existsAction) Execute(ctx context.Context, ec *action.ExecContext) (action.Result, error) {
	if err := ctx.Err(); err != nil {
		return action.Result{}, err
	}
	timeout, err := timeoutOf(a.Timeout)
	if err != nil {
		return action.Result{}, err
	}
	kind, err := kindOf(a.Include)
	if err != nil {
		return action.Result{}, err
	}
	s, err := open(ctx, ec, a.Connection)
	if err != nil {
		return action.Result{}, err
	}
	defer s.close()
	var found []string
	err = s.within(ctx, timeout, "checking "+a.Path, func() (err error) {
		found, err = s.matchKind(a.Path, kind)
		return err
	})
	if err != nil {
		return action.Result{Output: []any{}}, err
	}
	if len(found) == 0 {
		return action.Result{Output: []any{}}, fmt.Errorf("no %s matches %q on the server", kindName(kind), a.Path)
	}
	ec.Logf(slog.LevelDebug, "found %d %s for %s on %s", len(found), kindNames(kind, len(found)), a.Path, s.addr)
	return action.Result{Output: list(found)}, nil
}

// kindOf checks the value of include when the action runs and returns the kind of
// matchKind: file, folder or any. include is called as in transfer.list (R5b, before:
// kind any|file|folder).
func kindOf(include string) (string, error) {
	switch include {
	case "", "all":
		return "any", nil
	case "files", "folders":
		return strings.TrimSuffix(include, "s"), nil
	}
	return "", fmt.Errorf("invalid include %q (expected all, files or folders)", include)
}

// matchKind matches a path on the server and keeps only files, folders or both (kind
// file, folder or any); at most maxEntries.
func (s *session) matchKind(p, kind string) ([]string, error) {
	found, err := s.find(p, true, maxEntries)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range found {
		if kind == "file" && e.info.IsDir() || kind == "folder" && !e.info.IsDir() {
			continue
		}
		out = append(out, e.path)
	}
	return out, nil
}

func kindName(kind string) string {
	switch kind {
	case "file":
		return "file"
	case "folder":
		return "folder"
	}
	return "file or folder"
}

// kindNames is kindName for a number of entries, e.g. "2 files or folders".
func kindNames(kind string, n int) string {
	if n == 1 {
		return kindName(kind)
	}
	if kind == "file" || kind == "folder" {
		return kind + "s"
	}
	return "files or folders"
}
