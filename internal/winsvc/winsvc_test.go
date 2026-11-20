package winsvc

import (
	"strings"
	"testing"
)

func TestAccounts(t *testing.T) {
	for _, c := range []struct {
		in, want string
		password bool
	}{
		{"", AccountLocalService, false},
		{"LocalService", AccountLocalService, false},
		{`NT AUTHORITY\LocalService`, AccountLocalService, false},
		{"networkservice", AccountNetworkService, false},
		{"LocalSystem", AccountLocalSystem, false},
		{`CORP\svc-runnel`, `CORP\svc-runnel`, true},
		{`.\runnel`, `.\runnel`, true},
		{`CORP\runnel$`, `CORP\runnel$`, false},
	} {
		if got := NormalizeAccount(c.in); got != c.want || NeedsPassword(c.in) != c.password {
			t.Errorf("%q: %q, password %v", c.in, got, NeedsPassword(c.in))
		}
	}
}

func TestChecks(t *testing.T) {
	for _, name := range []string{"runnel", "runnel-import", "R_2"} {
		if err := CheckName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", "2runnel", "run nel", `a\b`, strings.Repeat("a", 65)} {
		if err := CheckName(name); err == nil {
			t.Errorf("%q accepted", name)
		}
	}
	if CheckStart("delayed") != nil || CheckStart("boot") == nil {
		t.Error("CheckStart")
	}
}
