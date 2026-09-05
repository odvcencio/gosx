package island

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/hydrate"
)

// WithRegistrations stages renderer-owned registrations without publishing
// them. It does not roll back effects performed by authored Go callbacks.
// Renderers, like PageRuntime, are request-local and not concurrency safe.
func (r *Renderer) WithRegistrations(render func(*Renderer) (gosx.Node, error)) (gosx.Node, error) {
	if r == nil || r.manifest == nil || render == nil {
		return gosx.Node{}, fmt.Errorf("island entry: missing renderer or callback")
	}
	before, err := json.Marshal(r.manifest)
	if err != nil {
		return gosx.Node{}, err
	}
	var manifest hydrate.Manifest
	if err := json.Unmarshal(before, &manifest); err != nil {
		return gosx.Node{}, err
	}
	staged := *r
	staged.runtimeVariants = maps.Clone(r.runtimeVariants)
	staged.runtimeAssets.WASMVariants = maps.Clone(r.runtimeAssets.WASMVariants)
	config := registrationConfig(r)
	staged.manifest = &manifest
	staged.programAssets = maps.Clone(r.programAssets)
	assets := maps.Clone(r.programAssets)
	counter := r.counter
	node, err := render(&staged)
	if err != nil {
		return gosx.Node{}, err
	}
	after, err := json.Marshal(r.manifest)
	if err != nil {
		return gosx.Node{}, err
	}
	if counter != r.counter || !bytes.Equal(before, after) || !reflect.DeepEqual(assets, r.programAssets) || !reflect.DeepEqual(config, registrationConfig(r)) {
		return gosx.Node{}, fmt.Errorf("island entry: renderer registrations changed during rendering")
	}
	// Commit renderer-owned configuration as well as registrations. Retain the
	// public manifest pointer. Stale external changes are never overwritten.
	manifestPointer := r.manifest
	*r = staged
	r.manifest = manifestPointer
	*r.manifest = manifest
	return node, nil
}

func registrationConfig(r *Renderer) Renderer {
	c := *r
	c.manifest = nil
	c.counter = 0
	c.programAssets = nil
	c.runtimeVariants = maps.Clone(r.runtimeVariants)
	c.runtimeAssets.WASMVariants = maps.Clone(r.runtimeAssets.WASMVariants)
	return c
}
