package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"sync"
	"time"

	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/plugintrust"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/schema"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/version"
)

// DetectTimeout limits transform.detectFields, which reads the first rows of an input.
const DetectTimeout = time.Minute

// prober starts a trusted plugin for single requests outside a run: transform.init when
// a pipeline is checked or prepared, transform.detectFields for the editor (S50b).
type prober struct {
	projectDir string
	plugin     *model.Plugin
	store      plugintrust.Store
}

// call starts the plugin after checking its approval again, sends one request and ends it.
func (pb prober) call(ctx context.Context, timeout time.Duration, method string, params, result any) error {
	if err := trusted(ctx, pb.projectDir, pb.plugin, pb.store); err != nil {
		return err
	}
	pr, err := Start(ctx, pb.plugin, Options{ProjectDir: pb.projectDir, RunnelVersion: version.Version, Env: environ(pb.projectDir)})
	if err != nil {
		return err
	}
	defer func() { _ = pr.Close() }()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := pr.call(ctx, method, params, result); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("plugin %s: no answer to %s within %s", pb.plugin.Name, method, timeout)
		}
		return fmt.Errorf("plugin %s: %s: %w", pb.plugin.Name, method, err)
	}
	return nil
}

// initResult is the answer to transform.init.
type initResult struct {
	Output   *record.Schema `json:"output"`
	Problems []struct {
		Path    string `json:"path"`
		Message string `json:"message"`
		Warning bool   `json:"warning"`
	} `json:"problems"`
}

// inits caches the answers to transform.init by plugin, its files and the request, so
// that checking a pipeline again (editor, validate, run) does not start the plugin again.
var inits = struct {
	sync.Mutex
	m map[[32]byte]initResult
}{m: map[[32]byte]initResult{}}

const maxInits = 1024

// Init asks the plugin for the schema of the output (transform.init) if it offers it;
// otherwise the output is open, as for transforms without Init (ADR 0007). Fields with
// secrets are sent empty and connections by name: init checks, it does not connect.
func (t *pluginTransform) Init(ic *transform.InitContext) (record.Schema, error) {
	if !t.init {
		return record.OpenSchema(), nil
	}
	config := maps.Clone(t.with)
	for _, prop := range t.schema.Properties {
		if prop.Schema != nil && prop.Schema.Format == schema.FormatSecret && config[prop.Name] != nil {
			config[prop.Name] = ""
		}
	}
	params := map[string]any{"type": t.typ, "config": config, "input": ic.In, "targets": ic.Targets}
	raw, err := json.Marshal(params)
	if err != nil {
		return record.OpenSchema(), err
	}
	dir, _ := filepath.Abs(t.probe.plugin.Dir)
	key := sha256.Sum256(append([]byte(dir+"\x00"+fingerprint(t.probe.plugin.Dir, t.probe.store)+"\x00"), raw...))
	inits.Lock()
	res, ok := inits.m[key]
	inits.Unlock()
	if !ok {
		if err := t.probe.call(context.Background(), StartTimeout, "transform.init", params, &res); err != nil {
			return record.OpenSchema(), err
		}
		if err := checkSchema(res.Output); err != nil {
			return record.OpenSchema(), fmt.Errorf("plugin %s: transform.init: %w", t.plugin, err)
		}
		inits.Lock()
		if len(inits.m) >= maxInits {
			clear(inits.m)
		}
		inits.m[key] = res
		inits.Unlock()
	}
	for _, p := range res.Problems {
		if p.Warning {
			ic.Warn(p.Path, p.Message)
		} else {
			ic.Error(p.Path, p.Message)
		}
	}
	if res.Output == nil {
		return record.OpenSchema(), nil
	}
	return *res.Output, nil
}

// checkSchema checks a schema from a plugin: fields with names, each once, of known types.
func checkSchema(s *record.Schema) error {
	if s == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, f := range s.Fields {
		switch {
		case f.Name == "":
			return fmt.Errorf("a field of the output has no name")
		case seen[f.Name]:
			return fmt.Errorf("field %q of the output is listed twice", f.Name)
		case f.Type != record.Unknown && !f.Type.Valid():
			return fmt.Errorf("field %q of the output has the unknown type %q", f.Name, f.Type)
		}
		seen[f.Name] = true
	}
	return nil
}

// detectFields asks the plugin for the fields of an input (transform.detectFields), with
// the connections resolved by the resolver in ctx, and compares them with "fields:".
func (pb prober) detectFields(ctx context.Context, typ string, sc *schema.Schema, with map[string]any, limit int) (*transform.Detection, error) {
	config, err := withConnections(ctx, sc, with, connection.ResolverFrom(ctx))
	if err != nil {
		return nil, err
	}
	var d transform.Detection
	if err := pb.call(ctx, DetectTimeout, "transform.detectFields", map[string]any{"type": typ, "config": config, "limit": limit}, &d); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range d.Fields {
		switch {
		case f.Name == "" || seen[f.Name]:
			return nil, fmt.Errorf("plugin %s: transform.detectFields: field %q is empty or listed twice", pb.plugin.Name, f.Name)
		case f.Type != "" && !record.Type(f.Type).Valid():
			return nil, fmt.Errorf("plugin %s: transform.detectFields: field %q has the unknown type %q", pb.plugin.Name, f.Name, f.Type)
		}
		seen[f.Name] = true
	}
	if err := transform.MergeFields(&d, with["fields"]); err != nil {
		return nil, err
	}
	return &d, nil
}
