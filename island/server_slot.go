package island

import (
	"fmt"
	"reflect"
	"sort"

	"m31labs.dev/gosx"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/island/program"
)

// ServerSlots is server-only content, never client props or program data.
type ServerSlots map[string]gosx.Node

// RenderWithServerSlots renders an island with opaque caller-owned panel DOM.
// All validation/rendering finishes before the island is registered.
func (r *Renderer) RenderWithServerSlots(prog *program.Program, props any, slots ServerSlots) (gosx.Node, error) {
	if prog == nil || !prog.HasServerSlots() {
		return gosx.Node{}, fmt.Errorf("ServerSlot: program declares no outlets")
	}
	if err := prog.ValidateServerSlots(); err != nil {
		return gosx.Node{}, err
	}
	if containsNodeValue(reflect.ValueOf(props), 0) {
		return gosx.Node{}, fmt.Errorf("ServerSlot: Node values belong in ServerSlots, not props")
	}
	encoded, err := SerializeProps(props)
	if err != nil {
		return gosx.Node{}, err
	}
	names := make([]string, 0)
	for _, n := range prog.Nodes {
		if n.Kind == program.NodeServerSlot {
			names = append(names, n.Text)
		}
	}
	sort.Strings(names)
	if len(slots) != len(names) {
		return gosx.Node{}, fmt.Errorf("ServerSlot: expected exactly %d slots, got %d", len(names), len(slots))
	}
	for _, name := range names {
		if _, ok := slots[name]; !ok {
			return gosx.Node{}, fmt.Errorf("ServerSlot: missing %s", name)
		}
	}
	owner := vm.NewIsland(prog, string(encoded))
	defer owner.Dispose()
	if err := owner.ValidateServerSlotValues(); err != nil {
		return gosx.Node{}, err
	}
	resolved := owner.CurrentTree()
	content, err := serverSlotTreeNode(resolved, 0, "0", slots)
	if err != nil {
		return gosx.Node{}, err
	}
	before := len(r.manifest.Islands)
	// Use the already serialized snapshot so a custom marshaler cannot change
	// props between validation and manifest publication.
	node := r.RenderIslandWithEvents(prog.Name, encoded, extractEventSlots(prog), content)
	if len(r.manifest.Islands) != before+1 {
		return gosx.Node{}, fmt.Errorf("ServerSlot: manifest registration failed")
	}
	entry := &r.manifest.Islands[len(r.manifest.Islands)-1]
	entry.ServerSlots = append([]string(nil), names...)
	entry.Requires = program.ServerSlotCapability
	hash := runtimehost.ServerSlotRuntimeHash()
	r.manifest.ServerSlotHost = &hydrate.ServerSlotHostRef{Path: "/gosx/server-slots.js?v=" + hash, Hash: hash, Integrity: runtimehost.ServerSlotRuntimeIntegrity(), Version: 1}
	return node, nil
}

func containsNodeValue(v reflect.Value, depth int) bool {
	if !v.IsValid() {
		return false
	}
	if depth > 32 {
		return true
	}
	if v.Type() == reflect.TypeOf(gosx.Node{}) {
		return true
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		return !v.IsNil() && containsNodeValue(v.Elem(), depth+1)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() && containsNodeValue(v.Field(i), depth+1) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if containsNodeValue(v.Index(i), depth+1) {
				return true
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if containsNodeValue(iter.Value(), depth+1) {
				return true
			}
		}
	}
	return false
}

func serverSlotTreeNode(tree *vm.ResolvedTree, idx int, path string, slots ServerSlots) (gosx.Node, error) {
	if tree == nil || idx < 0 || idx >= len(tree.Nodes) {
		return gosx.Node{}, fmt.Errorf("ServerSlot: invalid resolved tree")
	}
	n := tree.Nodes[idx]
	var attrs []any
	for _, a := range renderResolvedAttrs(&n, path) {
		if n.ServerSlot != "" && a.Name == "hidden" {
			continue
		} // no-JS fallback is visible
		if a.Bool {
			attrs = append(attrs, gosx.BoolAttr(a.Name))
		} else {
			attrs = append(attrs, gosx.Attr(a.Name, a.Value))
		}
	}
	if n.ServerSlot != "" {
		attrs = append(attrs, gosx.Attr("data-gosx-server-slot", n.ServerSlot))
	}
	var children []gosx.Node
	if n.ServerSlot != "" {
		children = append(children, slots[n.ServerSlot])
	} else {
		if n.Text != "" {
			children = append(children, gosx.Text(n.Text))
		}
		for i, child := range n.Children {
			part, err := serverSlotTreeNode(tree, child, childProgramPath(path, i), slots)
			if err != nil {
				return gosx.Node{}, err
			}
			children = append(children, part)
		}
	}
	if n.Tag == "" {
		return gosx.Fragment(children...), nil
	}
	args := []any{gosx.Attrs(attrs...)}
	for _, child := range children {
		args = append(args, child)
	}
	return gosx.El(n.Tag, args...), nil
}
