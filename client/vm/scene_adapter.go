package vm

import (
	"encoding/json"
	"reflect"
	"sort"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Compile-time assertion: *SceneAdapter satisfies the Reconciler interface.
// Phase 1c will move scene-specific code out of this file but the
// conformance must survive.
var _ Reconciler = (*SceneAdapter)(nil)

type resolvedNode struct {
	Kind     string
	Geometry string
	Material string
	Props    map[string]any
	Children []int
	Static   bool
}

// SceneAdapter is a live engine-program instance backed by the shared expression VM.
type SceneAdapter struct {
	program    *rootengine.Program
	props      map[string]any
	vm         *VM
	prev       []resolvedNode
	dirty      []bool
	signalDeps map[string][]int
	unsubs     map[string]func()

	// nodeGen[i] increments every time rt.prev[i] is (re)resolved, giving each
	// node a monotonic per-object change signal tied to the reconcile path. The
	// world-bake cache keys on (nodeGen) so any prop change that re-resolves a
	// node invalidates its cached WORLD positions/normals. Camera changes do NOT
	// re-resolve mesh nodes, so they never bump nodeGen — which is exactly why the
	// camera-independent world bake can be cached across an orbiting camera.
	nodeGen []uint64
	// worldBakeCache memoizes camera-independent baked WORLD geometry per node
	// across frames. It lives on the adapter so it persists across RenderBundle
	// calls; RenderBundle runs single-threaded per adapter (one adapter per
	// request in the native/SSR path), so the cache needs no locking.
	worldBakeCache map[int]*objectWorldBake
	bakeHits       uint64
	bakeMisses     uint64
}

// New constructs a live engine runtime with props decoded from JSON.
func NewSceneAdapter(prog *rootengine.Program, propsJSON string) *SceneAdapter {
	if prog == nil {
		prog = &rootengine.Program{}
	}
	rawProps := parseRawProps(propsJSON)
	vmProg := &islandprogram.Program{
		Exprs: prog.Exprs,
	}
	rt := &SceneAdapter{
		program:        prog,
		props:          rawProps,
		vm:             NewVM(vmProg, vmProps(rawProps)),
		dirty:          make([]bool, len(prog.EngineNodes)),
		signalDeps:     buildSignalDeps(prog),
		unsubs:         make(map[string]func()),
		nodeGen:        make([]uint64, len(prog.EngineNodes)),
		worldBakeCache: make(map[int]*objectWorldBake),
	}
	markAllDirty(rt.dirty)
	initSceneSignals(rt.vm, prog)
	return rt
}

// SetSharedSignal replaces a runtime-local signal with a shared signal store entry.
func (rt *SceneAdapter) SetSharedSignal(name string, sig *signal.Signal[Value]) {
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
func (rt *SceneAdapter) EvalExpr(id islandprogram.ExprID) Value {
	return rt.vm.Eval(id)
}

// Reconcile evaluates the current scene and produces incremental commands.
func (rt *SceneAdapter) Reconcile() []rootengine.Command {
	if rt == nil || len(rt.program.EngineNodes) == 0 {
		return nil
	}
	if len(rt.prev) == 0 {
		rt.prev = rt.resolveAll()
		clearDirty(rt.dirty)
		return createCommands(rt.prev)
	}
	return rt.syncDirty()
}

// Dispose releases the retained scene snapshot.
func (rt *SceneAdapter) Dispose() {
	for name, unsub := range rt.unsubs {
		unsub()
		delete(rt.unsubs, name)
	}
	rt.prev = nil
}

// RenderBundle builds a renderer-facing frame bundle from the current scene. It
// threads the per-node world-bake cache so static objects' camera-independent
// WORLD positions/normals are reused across frames (e.g. an orbiting camera over
// static geometry) instead of rebaked every call.
func (rt *SceneAdapter) RenderBundle(width, height int, timeSeconds float64) rootengine.RenderBundle {
	nodes := rt.snapshot()
	store := &worldBakeStore{
		cache:  rt.worldBakeCache,
		gen:    rt.nodeGen,
		hits:   &rt.bakeHits,
		misses: &rt.bakeMisses,
	}
	return buildRenderBundleCached(rt.props, nodes, width, height, timeSeconds, store)
}

func parseRawProps(propsJSON string) map[string]any {
	if propsJSON == "" {
		return map[string]any{}
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(propsJSON), &raw); err != nil {
		return map[string]any{}
	}
	if raw == nil {
		return map[string]any{}
	}
	return raw
}

func vmProps(raw map[string]any) map[string]Value {
	props := make(map[string]Value, len(raw))
	for key, value := range raw {
		props[key] = ValueFromAny(value)
	}
	return props
}

func initSceneSignals(machine *VM, prog *rootengine.Program) {
	for _, def := range prog.Signals {
		initVal := machine.Eval(def.Init)
		machine.SetSignal(def.Name, signal.New(initVal))
	}
}

func (rt *SceneAdapter) resolveAll() []resolvedNode {
	out := make([]resolvedNode, len(rt.program.EngineNodes))
	for i := range rt.program.EngineNodes {
		out[i] = rt.resolveNode(i)
		rt.bumpNodeGen(i)
	}
	return out
}

// bumpNodeGen advances a node's per-object change generation. It is called every
// time rt.prev[i] is (re)resolved — the single source of truth for "this node's
// resolved props changed", which is the world-bake cache's invalidation signal.
func (rt *SceneAdapter) bumpNodeGen(index int) {
	if index < 0 || index >= len(rt.nodeGen) {
		return
	}
	rt.nodeGen[index]++
}

func (rt *SceneAdapter) snapshot() []resolvedNode {
	if rt == nil || len(rt.program.EngineNodes) == 0 {
		return nil
	}
	if len(rt.prev) == 0 {
		rt.prev = rt.resolveAll()
		clearDirty(rt.dirty)
		return rt.prev
	}
	rt.syncDirty()
	return rt.prev
}

func (rt *SceneAdapter) syncDirty() []rootengine.Command {
	var commands []rootengine.Command
	for i := range rt.program.EngineNodes {
		if !rt.dirty[i] {
			continue
		}
		next := rt.resolveNode(i)
		commands = append(commands, diffNode(i, rt.prev[i], next)...)
		rt.prev[i] = next
		rt.bumpNodeGen(i)
		rt.dirty[i] = false
	}
	return commands
}

func (rt *SceneAdapter) resolveNode(index int) resolvedNode {
	node := rt.program.EngineNodes[index]
	return resolvedNode{
		Kind:     node.Kind,
		Geometry: node.Geometry,
		Material: node.Material,
		Props:    resolveProps(rt.vm, node.Props),
		Children: append([]int(nil), node.Children...),
		Static:   node.Static,
	}
}

func (rt *SceneAdapter) markSignalDirty(name string) {
	for _, index := range rt.signalDeps[name] {
		if index < 0 || index >= len(rt.dirty) {
			continue
		}
		rt.dirty[index] = true
	}
}

func resolveProps(machine *VM, props map[string]islandprogram.ExprID) map[string]any {
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

func valueToAny(value Value) any {
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
	if prog == nil || len(prog.EngineNodes) == 0 || len(prog.Exprs) == 0 {
		return map[string][]int{}
	}
	deps := make(map[string][]int)
	memo := make(map[islandprogram.ExprID]map[string]struct{}, len(prog.Exprs))
	visiting := make(map[islandprogram.ExprID]bool, len(prog.Exprs))

	for index, node := range prog.EngineNodes {
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
