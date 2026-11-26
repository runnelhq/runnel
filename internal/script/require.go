package script

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"
)

// maxModuleBytes limits the size of a module file.
const maxModuleBytes = 4 << 20

// require loads a module from the folder scripts/ of the project (ADR 0028), e.g.
// require('./scripts/util.js'), and returns its module.exports. Each module runs once
// per runtime; later calls return the same exports.
func (r *Runtime) require(call goja.FunctionCall) goja.Value {
	name := call.Argument(0).String()
	exports, err := r.load(name)
	if err == nil {
		return exports
	}
	if uncatchable(err) {
		panic(err) // not catchable by the script
	}
	e, newErr := r.vm.New(r.vm.Get("Error"), r.vm.ToValue(err.Error()))
	if newErr != nil {
		panic(r.vm.NewGoError(err))
	}
	panic(e)
}

func (r *Runtime) load(name string) (goja.Value, error) {
	rel, file, err := modulePath(r.opts.Dir, name)
	if err != nil {
		return nil, err
	}
	key := strings.ToLower(rel)
	if m, ok := r.modules[key]; ok {
		return m.Get("exports"), nil
	}
	info, err := os.Stat(file)
	if err != nil {
		return nil, fmt.Errorf("require(%q): file not found", name)
	}
	if info.Size() > maxModuleBytes {
		return nil, fmt.Errorf("require(%q): the file is larger than %d MiB", name, maxModuleBytes>>20)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("require(%q): cannot read the file", name)
	}
	code := strings.TrimPrefix(string(data), "\uFEFF")
	prg, err := goja.Compile(rel, "(function(exports, require, module) {"+code+"\n})", false)
	if err != nil {
		return nil, syntaxError(rel, err)
	}
	wrapper, err := r.vm.RunProgram(prg)
	if err != nil {
		return nil, r.failure(rel, err)
	}
	fn, _ := goja.AssertFunction(wrapper)
	module, exports := r.vm.NewObject(), r.vm.NewObject()
	_ = module.Set("exports", exports)
	r.modules[key] = module // a module that requires itself sees its exports so far
	if _, err := fn(goja.Undefined(), exports, r.vm.Get("require"), module); err != nil {
		delete(r.modules, key)
		if uncatchable(err) {
			return nil, err
		}
		return nil, r.failure(rel, err)
	}
	return module.Get("exports"), nil
}

// modulePath checks the name of a module and returns it relative to the project
// ("scripts/util.js") and as path of its file. Only .js files below scripts/ of the
// project are allowed: no absolute paths, no "..", no links that lead out of scripts/.
func modulePath(dir, name string) (rel, file string, err error) {
	if dir == "" {
		return "", "", errors.New("require is not available without a project folder")
	}
	denied := fmt.Errorf("require(%q): only .js files in the folder scripts/ of the project can be loaded, e.g. require('./scripts/util.js')", name)
	p := strings.ReplaceAll(name, `\`, "/")
	if p == "" || path.IsAbs(p) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", "", denied
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == ".." {
			return "", "", denied
		}
	}
	p = path.Clean(p)
	first, _, _ := strings.Cut(p, "/")
	if !strings.EqualFold(first, "scripts") || first == p || !strings.EqualFold(path.Ext(p), ".js") {
		return "", "", denied
	}
	file = filepath.Join(dir, filepath.FromSlash(p))
	target, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", "", fmt.Errorf("require(%q): file not found", name)
	}
	root, err := filepath.EvalSymlinks(filepath.Join(dir, first))
	if err != nil || !within(target, root) {
		return "", "", fmt.Errorf("require(%q): the file is outside the folder scripts/ of the project", name)
	}
	return p, target, nil
}

// within reports whether path lies inside dir.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// uncatchable reports an interrupt or a stack overflow, which the script must not catch.
func uncatchable(err error) bool {
	_, interrupted := errors.AsType[*goja.InterruptedError](err)
	_, overflow := errors.AsType[*goja.StackOverflowError](err)
	return interrupted || overflow
}
