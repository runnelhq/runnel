package version

import (
	"runtime"
	"runtime/debug"
	"testing"
)

func TestInfoString(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{
			name: "full",
			info: Info{Version: "v0.1.0", Commit: "85d0221a1b2c3d4e5f60", Date: "2026-11-18T21:38:00Z", GoVersion: "go1.24.7", OS: "windows", Arch: "amd64"},
			want: "runnel v0.1.0 (commit 85d0221a1b2c, 2026-11-18T21:38:00Z, go1.24.7, windows/amd64)",
		},
		{
			name: "dirty",
			info: Info{Version: "dev", Commit: "85d0221", Modified: true, GoVersion: "go1.24.7", OS: "linux", Arch: "amd64"},
			want: "runnel dev (commit 85d0221-dirty, go1.24.7, linux/amd64)",
		},
		{
			name: "no vcs info",
			info: Info{Version: "dev", GoVersion: "go1.24.7", OS: "linux", Arch: "arm64"},
			want: "runnel dev (go1.24.7, linux/arm64)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyBuildSettings(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.time", Value: "2026-11-18T21:38:00Z"},
		{Key: "vcs.modified", Value: "true"},
	}

	t.Run("fills empty fields", func(t *testing.T) {
		info := Info{Version: "dev"}
		applyBuildSettings(&info, settings)
		if info.Commit != "abc123" || info.Date != "2026-11-18T21:38:00Z" || !info.Modified {
			t.Errorf("unexpected info: %+v", info)
		}
	})

	t.Run("ldflags take precedence", func(t *testing.T) {
		info := Info{Version: "v0.1.0", Commit: "fromldflags", Date: "2026-01-01T00:00:00Z"}
		applyBuildSettings(&info, settings)
		if info.Commit != "fromldflags" || info.Date != "2026-01-01T00:00:00Z" || info.Modified {
			t.Errorf("ldflags values were overwritten: %+v", info)
		}
	})
}

func TestGet(t *testing.T) {
	info := Get()
	if info.Version != Version {
		t.Errorf("Version = %q, want %q", info.Version, Version)
	}
	if info.GoVersion != runtime.Version() || info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Errorf("runtime fields not set: %+v", info)
	}
}
