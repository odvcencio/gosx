package enginevm

import (
	"encoding/json"
	"reflect"
	"sort"

	"github.com/odvcencio/gosx/client/vm"
	rootengine "github.com/odvcencio/gosx/engine"
	islandprogram "github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/signal"
)

type resolvedNode struct {
	Kind     string
	Geometry string
	Material string
	Props    map[string]any
	Children []int
	Static   bool
}

// Runtime is a live engine-program instance backed by the shared expression VM.
type Runtime struct {
	program *rootengine.Program
	vm      *vm.VM
	prev    []resolvedNode
}

// New constructs a live engine runtime with props decoded from JSON.
func New(prog *rootengine.Program, propsJSON string) *Runtime {
	if prog == nil {
		prog = &rootengine.Program{}
	}
	vmProg := &islandprogram.Program{
		Exprs: prog.Exprs,
	}
	rt := &Runtime{
		program: prog,
		vm:      vm.NewVM(vmProg, parseProps(propsJSON)),
	}
	initSignals(rt.vm, prog)
	return rt
}

// SetSharedSignal replaces a runtime-local signal with a shared signal store entry.
func (rt *Runtime) SetSharedSignal(name string, sig *signal.Signal[vm.Value]) {
	rt.vm.SetSignal(name, sig)
}

// EvalExpr evaluates an expression in the engine runtime's VM.
func (rt *Runtime) EvalExpr(id islandprogram.ExprID) vm.Value {
	return rt.vm.Eval(id)
}

// Reconcile evaluates the current scene and produces incremental commands.
func (rt *Runtime) Reconcile() []rootengine.Command {
	next := rt.resolve()
	commands := reconcileScene(rt.prev, next)
	rt.prev = next
	return commands
}

// Dispose releases the retained scene snapshot.
func (rt *Runtime) Dispose() {
	rt.prev = nil
}

func parseProps(propsJSON string) map[string]vm.Value {
	if propsJSON == "" {
		return map[string]vm.Value{}
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(propsJSON), &raw); err != nil {
		return map[string]vm.Value{}
	}
	props := make(map[string]vm.Value, len(raw))
	for key, value := range raw {
		props[key] = vm.ValueFromAny(value)
	}
	return props
}

func initSignals(machine *vm.VM, prog *rootengine.Program) {
	for _, def := range prog.Signals {
		initVal := machine.Eval(def.Init)
		machine.SetSignal(def.Name, signal.New(initVal))
	}
}

func (rt *Runtime) resolve() []resolvedNode {
	if len(rt.program.Nodes) == 0 {
		return nil
	}
	out := make([]resolvedNode, len(rt.program.Nodes))
	for i, node := range rt.program.Nodes {
		out[i] = resolvedNode{
			Kind:     node.Kind,
			Geometry: node.Geometry,
			Material: node.Material,
			Props:    resolveProps(rt.vm, node.Props),
			Children: append([]int(nil), node.Children...),
			Static:   node.Static,
		}
	}
	return out
}

func resolveProps(machine *vm.VM, props map[string]islandprogram.ExprID) map[string]any {
	if len(props) == 0 {
		return nil
	}
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make(map[string]any, len(props))
	for _, key := range keys {
		out[key] = valueToAny(machine.Eval(props[key]))
	}
	return out
}

func valueToAny(value vm.Value) any {
	if value.Fields != nil {
		out := make(map[string]any, len(value.Fields))
		keys := make([]string, 0, len(value.Fields))
		for key := range value.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out[key] = valueToAny(value.Fields[key])
		}
		return out
	}
	if value.Items != nil {
		out := make([]any, len(value.Items))
		for i, item := range value.Items {
			out[i] = valueToAny(item)
		}
		return out
	}
	switch value.Type {
	case islandprogram.TypeString:
		return value.Str
	case islandprogram.TypeBool:
		return value.Bool
	case islandprogram.TypeInt:
		return int(value.Num)
	case islandprogram.TypeFloat:
		return value.Num
	default:
		if value.Str != "" {
			return value.Str
		}
		if value.Bool {
			return true
		}
		return value.Num
	}
}

func reconcileScene(prev, next []resolvedNode) []rootengine.Command {
	commands := make([]rootengine.Command, 0, len(next)+len(prev))

	maxLen := len(prev)
	if len(next) > maxLen {
		maxLen = len(next)
	}

	for i := 0; i < maxLen; i++ {
		switch {
		case i >= len(next):
			commands = append(commands, rootengine.Command{Kind: rootengine.CommandRemoveObject, ObjectID: i})
		case i >= len(prev):
			commands = append(commands, createObjectCommand(i, next[i]))
		default:
			commands = append(commands, diffNode(i, prev[i], next[i])...)
		}
	}

	return commands
}

func diffNode(index int, prev, next resolvedNode) []rootengine.Command {
	if prev.Kind != next.Kind || prev.Geometry != next.Geometry || prev.Material != next.Material || !reflect.DeepEqual(prev.Children, next.Children) || prev.Static != next.Static {
		return []rootengine.Command{createObjectCommand(index, next)}
	}

	switch next.Kind {
	case "camera":
		if reflect.DeepEqual(prev.Props, next.Props) {
			return nil
		}
		return []rootengine.Command{commandWithData(rootengine.CommandSetCamera, index, next.Props)}
	case "light":
		if reflect.DeepEqual(prev.Props, next.Props) {
			return nil
		}
		return []rootengine.Command{commandWithData(rootengine.CommandSetLight, index, next.Props)}
	}

	var commands []rootengine.Command
	if transform := changedSubset(prev.Props, next.Props, transformKeys); len(transform) > 0 {
		commands = append(commands, commandWithData(rootengine.CommandSetTransform, index, transform))
	}
	if material := changedSubset(prev.Props, next.Props, materialKeys); len(material) > 0 || prev.Material != next.Material {
		payload := map[string]any{}
		if next.Material != "" {
			payload["material"] = next.Material
		}
		for key, value := range material {
			payload[key] = value
		}
		commands = append(commands, commandWithData(rootengine.CommandSetMaterial, index, payload))
	}

	if hasNonCategorizedChanges(prev.Props, next.Props) {
		commands = append(commands, createObjectCommand(index, next))
	}

	return commands
}

func createObjectCommand(index int, node resolvedNode) rootengine.Command {
	return commandWithData(rootengine.CommandCreateObject, index, map[string]any{
		"kind":     node.Kind,
		"geometry": node.Geometry,
		"material": node.Material,
		"props":    node.Props,
		"children": node.Children,
		"static":   node.Static,
	})
}

func commandWithData(kind rootengine.CommandKind, index int, payload any) rootengine.Command {
	data, _ := json.Marshal(payload)
	return rootengine.Command{
		Kind:     kind,
		ObjectID: index,
		Data:     data,
	}
}

var transformKeys = map[string]struct{}{
	"x": {}, "y": {}, "z": {},
	"position": {},
	"rotation": {}, "rotationX": {}, "rotationY": {}, "rotationZ": {},
	"scale": {}, "scaleX": {}, "scaleY": {}, "scaleZ": {},
	"target": {}, "targetX": {}, "targetY": {}, "targetZ": {},
}

var materialKeys = map[string]struct{}{
	"color": {}, "wireframe": {}, "opacity": {}, "emissive": {},
}

func changedSubset(prev, next map[string]any, keys map[string]struct{}) map[string]any {
	out := map[string]any{}
	for key := range keys {
		prevValue, prevOK := prev[key]
		nextValue, nextOK := next[key]
		if !nextOK {
			continue
		}
		if !prevOK || !reflect.DeepEqual(prevValue, nextValue) {
			out[key] = nextValue
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hasNonCategorizedChanges(prev, next map[string]any) bool {
	keys := make(map[string]struct{}, len(prev)+len(next))
	for key := range prev {
		keys[key] = struct{}{}
	}
	for key := range next {
		keys[key] = struct{}{}
	}
	for key := range keys {
		if isCategorizedKey(key) {
			continue
		}
		if !reflect.DeepEqual(prev[key], next[key]) {
			return true
		}
	}
	return false
}

func isCategorizedKey(key string) bool {
	if _, ok := transformKeys[key]; ok {
		return true
	}
	if _, ok := materialKeys[key]; ok {
		return true
	}
	return false
}
