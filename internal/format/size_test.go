package format

import "testing"

func TestParseSize(t *testing.T) {
	for text, want := range map[string]int64{"": 7, "1024": 1024, "2KB": 2048, "500 mb": 500 << 20, "10GB": 10 << 30, "3B": 3, " 1 MB ": 1 << 20} {
		if got, err := ParseSize("maxSize", text, 7); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"0", "-1MB", "1TB", "MB", "1.5MB", "99999999999999999GB"} {
		if _, err := ParseSize("maxOutput", text, 7); err == nil || err.Error() != `maxOutput: "`+text+`" is not a size like 500KB, 10MB or 2GB` {
			t.Errorf("ParseSize(%q): %v", text, err)
		}
	}
}
