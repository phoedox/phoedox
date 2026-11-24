package publish

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/dop251/goja"
)

// Rough.js 4.6.6 (MIT, roughjs/LICENSE) – the same build the web editor
// uses (web/package.json), so a whiteboard element drawn here with its
// stored seed gets exactly the strokes people see in the editor. Update
// both together.
//
//go:embed roughjs/rough.js
var roughJS string

// roughOp is one drawing step of Rough.js: move, lineTo or bcurveTo.
type roughOp struct {
	Op   string    `json:"op"`
	Data []float64 `json:"data"`
}

// roughSet is a part of a drawing: "path" (stroke), "fillPath" (solid fill)
// or "fillSketch" (hatching, not used by the editor).
type roughSet struct {
	Type string    `json:"type"`
	Ops  []roughOp `json:"ops"`
}

// roughVM is a JavaScript runtime with Rough.js loaded; a runtime is not
// safe for concurrent use, so they are pooled.
type roughVM struct {
	vm  *goja.Runtime
	gen goja.Value
}

var roughPool = sync.Pool{New: func() any {
	vm := goja.New()
	if _, err := vm.RunString(roughJS); err != nil {
		return err
	}
	gen, err := vm.RunString("rough.generator()")
	if err != nil {
		return err
	}
	return &roughVM{vm: vm, gen: gen}
}}

// roughShape asks Rough.js for a shape (rectangle, ellipse, polygon, line,
// linearPath, curve) with the editor's options and returns its sets.
func roughShape(shape string, opts map[string]any, args ...any) ([]roughSet, error) {
	got := roughPool.Get()
	r, ok := got.(*roughVM)
	if !ok {
		return nil, fmt.Errorf("rough.js: %v", got)
	}
	defer roughPool.Put(r)
	fn, ok := goja.AssertFunction(r.gen.ToObject(r.vm).Get(shape))
	if !ok {
		return nil, fmt.Errorf("rough.js: no shape %q", shape)
	}
	vals := make([]goja.Value, 0, len(args)+1)
	for _, a := range args {
		vals = append(vals, r.vm.ToValue(a))
	}
	vals = append(vals, r.vm.ToValue(opts))
	d, err := fn(r.gen, vals...)
	if err != nil {
		return nil, fmt.Errorf("rough.js %s: %w", shape, err)
	}
	b, err := json.Marshal(d.ToObject(r.vm).Get("sets").Export())
	if err != nil {
		return nil, err
	}
	var sets []roughSet
	return sets, json.Unmarshal(b, &sets)
}
