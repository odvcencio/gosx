//go:build !js || !wasm

package scene

import (
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"m31labs.dev/selena"
	"m31labs.dev/selena/bindings"
	"m31labs.dev/selena/hir"
)

type selenaCacheKey struct {
	source            [32]byte
	material, targets string
	mrt               bool
}

type selenaCacheEntry struct {
	key    selenaCacheKey
	result selena.Result
	bytes  int
}

var compiledSelena = struct {
	sync.Mutex
	entries map[selenaCacheKey]*list.Element
	order   list.List
	bytes   int
}{entries: make(map[selenaCacheKey]*list.Element)}

func compileSelenaCached(source []byte, opts selena.CompileOptions) (selena.Result, error) {
	var targets strings.Builder
	for _, target := range opts.Targets {
		targets.WriteString(string(target))
		targets.WriteByte(0)
	}
	key := selenaCacheKey{sha256.Sum256(source), opts.Material, targets.String(), opts.SpecularMRT}
	compiledSelena.Lock()
	entry := compiledSelena.entries[key]
	if entry != nil {
		compiledSelena.order.MoveToFront(entry)
	}
	compiledSelena.Unlock()
	if entry != nil {
		result := entry.Value.(selenaCacheEntry).result
		result.Layout = cloneSelenaLayout(result.Layout)
		return result, nil
	}
	result, err := selena.Compile(source, opts)
	if err != nil {
		return result, err
	}
	// Retain the binding contract and shader strings; release parser and IR data.
	result = selena.Result{Material: hir.Material{Name: strings.Clone(result.Material.Name)}, Layout: result.Layout, Artifacts: result.Artifacts}
	encoded, _ := json.Marshal(result.Layout)
	size := 2048 + 2*len(encoded) + len(key.material) + len(key.targets)
	for _, a := range result.Artifacts {
		size += 2 * (len(a.Source) + len(a.Vertex) + len(a.Fragment))
		if a.SpecularMRT != nil {
			size += 2 * (len(a.SpecularMRT.Source) + len(a.SpecularMRT.Fragment))
		}
	}
	if size <= 8<<20 {
		compiledSelena.Lock()
		if compiledSelena.entries[key] == nil {
			for compiledSelena.order.Len() >= 64 || compiledSelena.bytes+size > 8<<20 {
				old := compiledSelena.order.Back()
				value := old.Value.(selenaCacheEntry)
				delete(compiledSelena.entries, value.key)
				compiledSelena.bytes -= value.bytes
				compiledSelena.order.Remove(old)
			}
			key.material = strings.Clone(key.material)
			compiledSelena.entries[key] = compiledSelena.order.PushFront(selenaCacheEntry{key, result, size})
			compiledSelena.bytes += size
		}
		compiledSelena.Unlock()
	}
	result.Layout = cloneSelenaLayout(result.Layout)
	return result, nil
}

func cloneSelenaLayout(layout bindings.Layout) bindings.Layout {
	layout.UniformBlock.Fields = slices.Clone(layout.UniformBlock.Fields)
	layout.UniformBlock.Defaults = slices.Clone(layout.UniformBlock.Defaults)
	for i := range layout.UniformBlock.Defaults {
		layout.UniformBlock.Defaults[i].Values = append([]float32(nil), layout.UniformBlock.Defaults[i].Values...)
	}
	layout.Attributes = slices.Clone(layout.Attributes)
	layout.Textures = slices.Clone(layout.Textures)
	layout.States = slices.Clone(layout.States)
	layout.Context = slices.Clone(layout.Context)
	layout.Requires.GLExtensions = slices.Clone(layout.Requires.GLExtensions)
	if layout.SpecularOutput != nil {
		copy := *layout.SpecularOutput
		copy.GLExtensions = slices.Clone(copy.GLExtensions)
		layout.SpecularOutput = &copy
	}
	if layout.Grid != nil {
		copy := *layout.Grid
		layout.Grid = &copy
	}
	return layout
}
