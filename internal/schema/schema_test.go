package schema

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

type target struct {
	Host string `json:"host" doc:"Server name" schema:"required"`
	Port uint16 `json:"port" doc:"TCP port" schema:"default=22"`
}

type Common struct {
	Comment string `json:"comment" doc:"Free text"`
}

type sample struct {
	Common
	Source    string            `json:"source" doc:"File to copy" schema:"required,example=C:\\in\\data.csv"`
	Timeout   string            `json:"timeout" doc:"Maximum time" schema:"format=runnel-duration,default=5m"`
	Mode      string            `json:"mode" doc:"What to do" schema:"enum=copy|move,default=copy"`
	Count     int               `json:"count,omitempty" doc:"Number of tries" schema:"enum=1|2|3"`
	Ratio     float64           `json:"ratio" doc:"Share" schema:"default=0.5"`
	Overwrite bool              `json:"overwrite" doc:"Replace existing files" schema:"default=false"`
	Patterns  []string          `json:"patterns" doc:"File patterns"`
	Targets   []target          `json:"targets" doc:"Where to copy to"`
	Headers   map[string]string `json:"headers" doc:"Extra headers"`
	Main      *target           `json:"main" doc:"Primary target"`
	Extra     any               `json:"extra" doc:"Anything"`
	Skipped   string            `json:"-"`
	NoName    string
	internal  string
}

func TestFor(t *testing.T) {
	_ = sample{}.internal
	s, err := For(&sample{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "sample.golden.json", append(got, '\n'))
}

func TestForNoConfiguration(t *testing.T) {
	s, err := For(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(s)
	want := `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}`
	if string(got) != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

func TestForErrors(t *testing.T) {
	type recursive struct {
		Next *recursive `json:"next"`
	}
	tests := []struct {
		config any
		want   string
	}{
		{"text", "configuration must be a struct, not string"},
		{struct {
			C chan int `json:"c"`
		}{}, `field "c": unsupported type chan int`},
		{struct {
			M map[int]string `json:"m"`
		}{}, `field "m": map keys must be strings`},
		{recursive{}, `field "next": recursive type`},
		{struct {
			N int `json:"n" schema:"default=many"`
		}{}, `field "n": default: strconv.ParseInt`},
		{struct {
			B bool `json:"b" schema:"enum=yes|no"`
		}{}, `field "b": enum:`},
		{struct {
			N int `json:"n" schema:"format=runnel-duration"`
		}{}, `field "n": format is only allowed for strings`},
		{struct {
			S string `json:"s" schema:"requird"`
		}{}, `field "s": unknown schema option "requird"`},
		{struct {
			L []string `json:"l" schema:"default=a"`
		}{}, `field "l": default: values are only supported`},
		{struct {
			Common
			Comment string `json:"comment"`
		}{}, `field "comment": field name used twice`},
	}
	for _, tt := range tests {
		if _, err := For(tt.config); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%T: error %v, want containing %q", tt.config, err, tt.want)
		}
	}
}

func TestMissingRequired(t *testing.T) {
	tests := []struct {
		data map[string]any
		want string
	}{
		{map[string]any{"source": "a"}, ""},
		{map[string]any{}, "source"},
		{map[string]any{"source": nil}, "source"},
		{map[string]any{"source": "a", "main": map[string]any{"port": 1}}, "main.host"},
		{map[string]any{"source": "a", "targets": []any{map[string]any{"host": "x"}, map[string]any{}}}, "targets[1].host"},
		{map[string]any{"source": "a", "main": "not an object"}, ""}, // type errors are reported by the decoder
	}
	for _, tt := range tests {
		if got := MissingRequired(sample{}, tt.data); got != tt.want {
			t.Errorf("%v: got %q, want %q", tt.data, got, tt.want)
		}
	}
	if got := MissingRequired(nil, map[string]any{"x": 1}); got != "" {
		t.Errorf("no configuration: %q", got)
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if string(got) != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("result differs from %s (run with -update if intended)\n--- got:\n%s", path, got)
	}
}
