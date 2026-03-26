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
	program    *rootengine.Program
	vm         *vm.VM
	prev       []resolvedNode
	dirty      []bool
	signalDeps map[string][]int
	unsubs     map[string]func()
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
		program:    prog,
		vm:         vm.NewVM(vmProg, parseProps(propsJSON)),
		dirty:      make([]bool, len(prog.Nodes)),
		signalDeps: buildSignalDeps(prog),
		unsubs:     make(map[string]func()),
	}
	markAllDirty(rt.dirty)
	initSignals(rt.vm, prog)
	return rt
}

// SetSharedSignal replaces a runtime-local signal with a shared signal store entry.
func (rt *Runtime) SetSharedSignal(name string, sig *signal.Signal[vm.Value]) {
	if rt == nil {
		return
	}
	if unsub, ok := rt.unsubs[name]; ok {
		unsub()
		delete(rt.unsubs, name)
	}
	rt.vm.SetSignal(name, sig)
	if sig != nil {
		rt.unsubs[name] = sig.Subscribe(func() {
			rt.markSignalDirty(name)
		})
	}
	rt.markSignalDirty(name)
}

// EvalExpr evaluates an expression in the engine runtime's VM.
func (rt *Runtime) EvalExpr(id islandprogram.ExprID) vm.Value {
	return rt.vm.Eval(id)
}

// Reconcile evaluates the current scene and produces incremental commands.
func (rt *Runtime) Reconcile() []rootengine.Command {
	if rt == nil || len(rt.program.Nodes) == 0 {
		return nil
	}
	if len(rt.prev) == 0 {
		rt.prev = rt.resolveAll()
		clearDirty(rt.dirty)
		return createCommands(rt.prev)
	}

	var commands []rootengine.Command
	for i := range rt.program.Nodes {
		if !rt.dirty[i] {
			continue
		}
		next := rt.resolveNode(i)
		commands = append(commands, diffNode(i, rt.prev[i], next)...)
		rt.prev[i] = next
		rt.dirty[i] = false
	}
	return commands
}

// Dispose releases the retained scene snapshot.
func (rt *Runtime) Dispose() {
	for name, unsub := range rt.unsubs {
		unsub()
		delete(rt.unsubs, name)
	}
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

func (rt *Runtime) resolveAll() []resolvedNode {
	out := make([]resolvedNode, len(rt.program.Nodes))
	for i := range rt.program.Nodes {
		out[i] = rt.resolveNode(i)
	}
	return out
}

func (rt *Runtime) resolveNode(index int) resolvedNode {
	node := rt.program.Nodes[index]
	return resolvedNode{
		Kind:     node.Kind,
		Geometry: node.Geometry,
		Material: node.Material,
		Props:    resolveProps(rt.vm, node.Props),
		Children: append([]int(nil), node.Children...),
		Static:   node.Static,
	}
}

func (rt *Runtime) markSignalDirty(name string) {
	for _, index := range rt.signalDeps[name] {
		if index < 0 || index >= len(rt.dirty) {
			continue
		}
		rt.dirty[index] = true
	}
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

func createCommands(nodes []resolvedNode) []rootengine.Command {
	commands := make([]rootengine.Command, 0, len(nodes))
	for i, node := range nodes {
		commands = append(commands, createObjectCommand(i, node))
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

func markAllDirty(flags []bool) {
	for i := range flags {
		flags[i] = true
	}
}

func clearDirty(flags []bool) {
	for i := range flags {
		flags[i] = false
	}
}

func buildSignalDeps(prog *rootengine.Program) map[string][]int {
	if prog == nil || len(prog.Nodes) == 0 || len(prog.Exprs) == 0 {
		return map[string][]int{}
	}
	deps := make(map[string][]int)
	memo := make(map[islandprogram.ExprID]map[string]struct{}, len(prog.Exprs))
	visiting := make(map[islandprogram.ExprID]bool, len(prog.Exprs))

	for index, node := range prog.Nodes {
		if node.Static {
			continue
		}
		nodeSignals := make(map[string]struct{})
		for _, exprID := range node.Props {
			for name := range collectExprSignals(prog.Exprs, exprID, memo, visiting) {
				nodeSignals[name] = struct{}{}
			}
		}
		for name := range nodeSignals {
			deps[name] = append(deps[name], index)
		}
	}

	return deps
}

func collectExprSignals(
	exprs []islandprogram.Expr,
	id islandprogram.ExprID,
	memo map[islandprogram.ExprID]map[string]struct{},
	visiting map[islandprogram.ExprID]bool,
) map[string]struct{} {
	if deps, ok := memo[id]; ok {
		return deps
	}
	if int(id) < 0 || int(id) >= len(exprs) {
		return nil
	}
	if visiting[id] {
		return nil
	}
	visiting[id] = true
	defer delete(visiting, id)

	expr := exprs[id]
	deps := make(map[string]struct{})
	switch expr.Op {
	case islandprogram.OpSignalGet, islandprogram.OpSignalSet, islandprogram.OpSignalUpdate:
		if expr.Value != "" {
			deps[expr.Value] = struct{}{}
		}
	}
	for _, operand := range expr.Operands {
		for name := range collectExprSignals(exprs, operand, memo, visiting) {
			deps[name] = struct{}{}
		}
	}
	memo[id] = deps
	return deps
}
