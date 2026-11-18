// Package version provides version and build information for Runnel.
//
// Version, Commit and Date are set at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/runnelhq/runnel/internal/version.Version=v0.1.0" ./cmd/runnel
//
// For builds without ldflags (e.g. plain "go build" or "go install"), commit and
// date are taken from the VCS information embedded by the Go toolchain.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Build-time variables, overridable via -ldflags "-X ...".
var (
	// Version is the release version, e.g. "v0.1.0". Defaults to "dev".
	Version = "dev"
	// Commit is the git commit hash the binary was built from.
	Commit = ""
	// Date is the build or commit date in RFC 3339 format.
	Date = ""
)

// Info describes the running binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	Date      string `json:"date,omitempty"`
	Modified  bool   `json:"modified,omitempty"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get returns the version information of the running binary.
func Get() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		applyBuildSettings(&info, bi.Settings)
	}
	return info
}

// applyBuildSettings fills commit and date from the embedded VCS settings,
// unless they were already set via ldflags.
func applyBuildSettings(info *Info, settings []debug.BuildSetting) {
	if info.Commit != "" {
		return
	}
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			info.Commit = s.Value
		case "vcs.time":
			if info.Date == "" {
				info.Date = s.Value
			}
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
}

// ShortCommit returns the first 12 characters of the commit hash.
func (i Info) ShortCommit() string {
	if len(i.Commit) > 12 {
		return i.Commit[:12]
	}
	return i.Commit
}

// String formats the information as a single line, e.g.
// "runnel v0.1.0 (commit 85d0221a1b2c, 2026-11-18T21:38:00Z, go1.24.7, windows/amd64)".
func (i Info) String() string {
	details := make([]string, 0, 4)
	if c := i.ShortCommit(); c != "" {
		if i.Modified {
			c += "-dirty"
		}
		details = append(details, "commit "+c)
	}
	if i.Date != "" {
		details = append(details, i.Date)
	}
	details = append(details, i.GoVersion, i.OS+"/"+i.Arch)
	return fmt.Sprintf("runnel %s (%s)", i.Version, strings.Join(details, ", "))
}
