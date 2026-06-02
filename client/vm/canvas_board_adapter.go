package vm

import (
	"reflect"
	"strings"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/signal"
)

// Compile-time assertion: *CanvasBoardAdapter satisfies the Reconciler
// interface, exactly like *SceneAdapter. Phase 2 keeps the lifecycle surface
// unified — surface-specific outputs (Command lists for both adapters,
// PatchOp lists for islands) live on the concrete types.
var _ Reconciler = (*CanvasBoardAdapter)(nil)

// CanvasBoardAdapter is a live engine-program instance for the Canvas2D
// surface kind. It mirrors SceneAdapter structurally — same VM glue, same
// signal-driven dirty tracking, same Reconcile/Dispose lifecycle — and only
// diverges in node kinds (rect/line/label/image vs mesh/light/camera/sprite)
// and the render-bundle assembly (OrthoCamera2D + Configure2DBundle).
//
// Per the Phase 2 plan, the adapter is declarative: pan/zoom/select/drag/drop
// are signal-driven board state, NOT user-authored Go. The shared expression
// VM evaluates board node props from prog.EngineNodes; this adapter never
// pulls in opcodes outside the existing set.
type CanvasBoardAdapter struct {
	program    *rootengine.Program
	props      map[string]any
	vm         *VM
	prev       []resolvedNode
	dirty      []bool
	signalDeps map[string][]int
	unsubs     map[string]func()
}

// NewCanvasBoardAdapter constructs the live runtime for a Canvas2D program.
// It is the structural analog of NewSceneAdapter; callers in the WASM bridge
// dispatch to one or the other based on Program.Surface.
//
// propsJSON is the wire-format JSON the .gsx renderer passed at mount time
// (the same shape the SceneAdapter consumes). Unknown fields are tolerated.
func NewCanvasBoardAdapter(prog *rootengine.Program, propsJSON string) *CanvasBoardAdapter {
	if prog == nil {
		prog = &rootengine.Program{}
	}
	// Mark the program as Canvas2D so downstream code paths can tell — even
	// if the decoder didn't set this (Phase 2 ships the decoder + this is
	// belt-and-suspenders for hand-built programs in tests).
	if prog.Surface == islandprogram.SurfaceDOM {
		prog.Surface = islandprogram.SurfaceCanvas2D
	}
	rawProps := parseRawProps(propsJSON)
	vmProg := &islandprogram.Program{Exprs: prog.Exprs}
	rt := &CanvasBoardAdapter{
		program:    prog,
		props:      rawProps,
		vm:         NewVM(vmProg, vmProps(rawProps)),
		dirty:      make([]bool, len(prog.EngineNodes)),
		signalDeps: buildSignalDeps(prog),
		unsubs:     make(map[string]func()),
	}
	markAllDirty(rt.dirty)
	initSceneSignals(rt.vm, prog) // shared with SceneAdapter; signals are namespace-agnostic
	return rt
}

// Surface reports the surface kind of the underlying program. CanvasBoardAdapter
// always reports SurfaceCanvas2D regardless of what the decoder set — the
// adapter is the source of truth for which pipeline runs.
func (rt *CanvasBoardAdapter) Surface() islandprogram.SurfaceKind {
	return islandprogram.SurfaceCanvas2D
}

// SetSharedSignal replaces a runtime-local signal with a shared signal store
// entry. Mirrors SceneAdapter.SetSharedSignal exactly — both adapters share
// the same VM signal mechanism. The only difference is which namespace the
// .gsx authors typically subscribe to ($surface.event.* per ADR 0007 vs the
// legacy $scene.event.* aliased on top).
func (rt *CanvasBoardAdapter) SetSharedSignal(name string, sig *signal.Signal[Value]) {
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

// EvalExpr evaluates an expression in the board runtime's VM. Used by the
// bridge for shared-signal initialization and ad-hoc evaluation.
func (rt *CanvasBoardAdapter) EvalExpr(id islandprogram.ExprID) Value {
	return rt.vm.Eval(id)
}

// Reconcile evaluates the current board state and produces incremental
// commands. Commands use the same rootengine.Command type that Scene3D uses
// (CommandCreateObject + CommandSetTransform + CommandSetMaterial); the
// renderer interprets them in 2D mode via the bundle's OrthoCamera2D camera.
//
// Canvas2D-specific node kinds (rect, line, label, image) are encoded as
// generic kinds in resolvedNode.Kind; the renderer dispatches on Kind in
// the bundle path, not here.
func (rt *CanvasBoardAdapter) Reconcile() []rootengine.Command {
	if rt == nil || len(rt.program.EngineNodes) == 0 {
		return nil
	}
	if len(rt.prev) == 0 {
		rt.prev = rt.resolveAll()
		clearDirty(rt.dirty)
		return canvasBoardCreateCommands(rt.prev)
	}
	return rt.syncDirty()
}

// Dispose releases the retained board snapshot and unsubscribes from every
// shared signal. After Dispose the adapter is inert.
func (rt *CanvasBoardAdapter) Dispose() {
	for name, unsub := range rt.unsubs {
		unsub()
		delete(rt.unsubs, name)
	}
	rt.prev = nil
}

// RenderBundle builds a renderer-facing frame bundle for the current board.
// The returned bundle always carries an OrthoCamera2D camera and has been
// passed through bundle.Configure2DBundle (lighting/post-FX stripped per
// ADR 0004's 2D-mode pipeline rules).
//
// Pan/zoom flow in through the special prop keys "pan.x", "pan.y", and
// "zoom" — set by user code via signal writes. Defaults are pan=(0,0)
// (board origin centered) and zoom=1 (1 world unit = 1 pixel).
func (rt *CanvasBoardAdapter) RenderBundle(width, height int, timeSeconds float64) rootengine.RenderBundle {
	nodes := rt.snapshot()
	// Static-CanvasBoard fallback: a Go-constructed gosx.CanvasBoard(props) is a
	// no-code primitive — it ships no compiled program, so the bridge hydrates it
	// with an empty {} program and prog.EngineNodes is nil (snapshot() returns
	// nil here). Such a board carries its content in the runtime props JSON under
	// props.nodes (the gosx.CanvasBoardNode shape). When there are no compiled
	// EngineNodes, project those props.nodes into the same internal resolvedNode
	// representation buildCanvasBoardRenderBundle consumes so rects/lines/labels/
	// sprites paint with their colors. Precedence is strict: if EngineNodes IS
	// present (the compiled-.gsx path) snapshot() is non-empty and this fallback
	// never fires — props.nodes is ignored and today's behavior is unchanged.
	if len(nodes) == 0 {
		nodes = canvasBoardNodesFromProps(rt.props)
	}
	return buildCanvasBoardRenderBundle(rt.props, nodes, width, height, timeSeconds)
}

// canvasBoardNodesFromProps parses the runtime-props "nodes" array a static
// gosx.CanvasBoard emits (a []CanvasBoardNode marshaled to JSON, decoded here
// as []any of map[string]any) into the resolvedNode representation the render
// path consumes. The JSON keys mirror canvasboard.go's CanvasBoardNode tags
// exactly — "kind", "id", "x", "y", "width", "height", "x1", "y1", "x2", "y2",
// "color", "text", "src" — so the existing canvasBoard{Rect,Line,Label,Sprite}
// helpers (which read those same prop keys) light up unchanged. Returns nil
// when props carries no usable nodes array.
func canvasBoardNodesFromProps(props map[string]any) []resolvedNode {
	raw, ok := props["nodes"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]resolvedNode, 0, len(raw))
	for _, entry := range raw {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := node["kind"].(string)
		if strings.TrimSpace(kind) == "" {
			continue
		}
		// Copy the node's JSON fields straight into Props. buildCanvasBoardRenderBundle
		// and its helpers read Kind plus the same x/y/width/height/x1..y2/color/text/
		// src/id keys, and numericProp already coerces the float64s json.Unmarshal
		// produces. The full map is forwarded so future CanvasBoardNode fields flow
		// through without touching this seam.
		resolved := resolvedNode{
			Kind:  kind,
			Props: node,
		}
		out = append(out, resolved)
	}
	return out
}

func (rt *CanvasBoardAdapter) resolveAll() []resolvedNode {
	out := make([]resolvedNode, len(rt.program.EngineNodes))
	for i := range rt.program.EngineNodes {
		out[i] = rt.resolveNode(i)
	}
	return out
}

func (rt *CanvasBoardAdapter) snapshot() []resolvedNode {
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

func (rt *CanvasBoardAdapter) syncDirty() []rootengine.Command {
	var commands []rootengine.Command
	for i := range rt.program.EngineNodes {
		if !rt.dirty[i] {
			continue
		}
		next := rt.resolveNode(i)
		commands = append(commands, canvasBoardDiffNode(i, rt.prev[i], next)...)
		rt.prev[i] = next
		rt.dirty[i] = false
	}
	return commands
}

func (rt *CanvasBoardAdapter) resolveNode(index int) resolvedNode {
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

func (rt *CanvasBoardAdapter) markSignalDirty(name string) {
	for _, index := range rt.signalDeps[name] {
		if index < 0 || index >= len(rt.dirty) {
			continue
		}
		rt.dirty[index] = true
	}
}

// canvasBoardCreateCommands builds the initial command stream for every board
// node. Distinct from createCommands (the 3D form) only in that it never
// emits CommandSetLight or CommandSetCamera — board cameras flow through the
// render bundle's OrthoCamera2D, not through scene commands.
func canvasBoardCreateCommands(nodes []resolvedNode) []rootengine.Command {
	commands := make([]rootengine.Command, 0, len(nodes))
	for i, node := range nodes {
		commands = append(commands, createObjectCommand(i, node))
	}
	return commands
}

// canvasBoardKindIsCamera reports whether kind names a board camera. Board
// programs typically don't carry an explicit camera node — pan/zoom live on
// the root props — but the diff path tolerates one for parity with Scene3D.
func canvasBoardKindIsCamera(kind string) bool {
	return strings.EqualFold(kind, "camera")
}

// canvasBoardDiffNode produces the incremental command set for a single board
// node. It mirrors SceneAdapter.diffNode but skips the SetLight branch (board
// programs have no lights) and recognizes the 2D-specific kinds (rect, line,
// label, image) as ordinary objects with transform + material updates.
func canvasBoardDiffNode(index int, prev, next resolvedNode) []rootengine.Command {
	if prev.Kind != next.Kind || prev.Geometry != next.Geometry || prev.Material != next.Material || !reflect.DeepEqual(prev.Children, next.Children) || prev.Static != next.Static {
		return []rootengine.Command{createObjectCommand(index, next)}
	}

	if canvasBoardKindIsCamera(next.Kind) {
		if reflect.DeepEqual(prev.Props, next.Props) {
			return nil
		}
		return []rootengine.Command{commandWithData(rootengine.CommandSetCamera, index, next.Props)}
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

// buildCanvasBoardRenderBundle assembles the 2D-mode RenderBundle for a board.
// Pulls pan/zoom from props (under "pan.x", "pan.y", "zoom" or the nested
// "board" key) and projects every node into an unlit instanced quad.
//
// Node kinds recognized:
//
//   - "rect"  : axis-aligned rectangle. Props: x, y, width, height, color
//   - "line"  : two-endpoint segment. Props: x1, y1, x2, y2, color, thickness
//   - "label" : text overlay. Props: x, y, text, color, fontSize
//   - "image" : sprite. Props: x, y, width, height, src
//
// Any other kind is silently dropped (forward-compat: future kinds can land
// without breaking older runtimes).
func buildCanvasBoardRenderBundle(props map[string]any, nodes []resolvedNode, width, height int, timeSeconds float64) rootengine.RenderBundle {
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
	zoom, panX, panY := canvasBoardCameraFromProps(props)
	cam := bundle.OrthoCamera2D(zoom, panX, panY, width, height)

	b := rootengine.RenderBundle{
		Background: canvasBoardBackground(props),
		Camera:     cam,
		Objects:    []rootengine.RenderObject{},
		Materials:  []rootengine.RenderMaterial{},
		Labels:     []rootengine.RenderLabel{},
		Sprites:    []rootengine.RenderSprite{},
		Lines:      []rootengine.RenderLine{},
	}

	for index, node := range nodes {
		switch strings.ToLower(strings.TrimSpace(node.Kind)) {
		case "rect":
			obj := canvasBoardRectObject(index, node)
			// Carry the rect's fill color through bundle.Materials so the
			// renderer (GPU path) and the JS canvas2d painter can both read it
			// via Objects[i].MaterialIndex → Materials[MaterialIndex].Color —
			// the same lookup render/bundle/object_mesh.go uses for 3D meshes.
			color, _ := node.Props["color"].(string)
			obj.MaterialIndex = ensureCanvasBoardMaterial(&b, color)
			b.Objects = append(b.Objects, obj)
		case "line":
			b.Lines = append(b.Lines, canvasBoardLine(index, node))
		case "label":
			b.Labels = append(b.Labels, canvasBoardLabel(index, node))
		case "image", "sprite":
			b.Sprites = append(b.Sprites, canvasBoardSprite(index, node))
		}
	}
	b.ObjectCount = len(b.Objects)

	// Apply ADR 0004's 2D-mode pipeline gate. Belt-and-suspenders: a 2D
	// bundle should never carry lights/post-FX in the first place, but the
	// gate makes the property explicit.
	bundle.Configure2DBundle(&b)
	_ = timeSeconds // reserved for time-based motion in future slices
	return b
}

func canvasBoardBackground(props map[string]any) string {
	if bg, ok := propValue(props, "background").(string); ok && bg != "" {
		return bg
	}
	if board := nestedProps(props, "board"); board != nil {
		if bg, ok := board["background"].(string); ok && bg != "" {
			return bg
		}
	}
	return ""
}

func canvasBoardCameraFromProps(props map[string]any) (zoom, panX, panY float64) {
	zoom = 1
	if board := nestedProps(props, "board"); board != nil {
		if z, ok := numericProp(board, "zoom"); ok {
			zoom = z
		}
		if pan := nestedProps(board, "pan"); pan != nil {
			if x, ok := numericProp(pan, "x"); ok {
				panX = x
			}
			if y, ok := numericProp(pan, "y"); ok {
				panY = y
			}
		}
	}
	if z, ok := numericProp(props, "zoom"); ok {
		zoom = z
	}
	if pan := nestedProps(props, "pan"); pan != nil {
		if x, ok := numericProp(pan, "x"); ok {
			panX = x
		}
		if y, ok := numericProp(pan, "y"); ok {
			panY = y
		}
	}
	if zoom <= 0 {
		zoom = 1
	}
	return zoom, panX, panY
}

func canvasBoardRectObject(index int, node resolvedNode) rootengine.RenderObject {
	x, _ := numericProp(node.Props, "x")
	y, _ := numericProp(node.Props, "y")
	w, _ := numericProp(node.Props, "width")
	h, _ := numericProp(node.Props, "height")
	if w == 0 {
		w = 1
	}
	if h == 0 {
		h = 1
	}
	obj := rootengine.RenderObject{
		ID:   canvasBoardNodeID(node, index),
		Kind: "rect",
		Bounds: rootengine.RenderBounds{
			MinX: x, MaxX: x + w,
			MinY: y, MaxY: y + h,
			MinZ: 0, MaxZ: 0,
		},
		VertexCount: 6, // two triangles per rect; renderer materializes the verts
	}
	// Board nodes are pickable by default — the whole point of a CanvasBoard
	// is direct manipulation. Authors can opt out by setting `pickable={false}`.
	pickable := true
	if explicit, ok := node.Props["pickable"].(bool); ok {
		pickable = explicit
	}
	obj.Pickable = &pickable
	return obj
}

// ensureCanvasBoardMaterial appends a deduplicated unlit material carrying the
// given fill color to b.Materials and returns its index. Empty colors collapse
// onto a single shared default-material slot. Mirrors the scene path's
// ensureRenderMaterial dedup so a 100-rect board with a small palette emits
// only a handful of materials, not one per node.
func ensureCanvasBoardMaterial(b *rootengine.RenderBundle, color string) int {
	color = strings.TrimSpace(color)
	for i, existing := range b.Materials {
		if existing.Color == color {
			return i
		}
	}
	b.Materials = append(b.Materials, rootengine.RenderMaterial{
		Kind:  "flat",
		Color: color,
		Unlit: true,
	})
	return len(b.Materials) - 1
}

func canvasBoardLine(index int, node resolvedNode) rootengine.RenderLine {
	x1, _ := numericProp(node.Props, "x1")
	y1, _ := numericProp(node.Props, "y1")
	x2, _ := numericProp(node.Props, "x2")
	y2, _ := numericProp(node.Props, "y2")
	color, _ := node.Props["color"].(string)
	thickness, _ := numericProp(node.Props, "thickness")
	if thickness == 0 {
		thickness = 1
	}
	_ = index // RenderLine has no ID field; index is captured via slice position
	return rootengine.RenderLine{
		From:      rootengine.RenderPoint{X: x1, Y: y1},
		To:        rootengine.RenderPoint{X: x2, Y: y2},
		Color:     color,
		LineWidth: thickness,
	}
}

func canvasBoardLabel(index int, node resolvedNode) rootengine.RenderLabel {
	x, _ := numericProp(node.Props, "x")
	y, _ := numericProp(node.Props, "y")
	text, _ := node.Props["text"].(string)
	color, _ := node.Props["color"].(string)
	fontSize, _ := numericProp(node.Props, "fontSize")
	if fontSize == 0 {
		fontSize = 14
	}
	return rootengine.RenderLabel{
		ID:       canvasBoardNodeID(node, index),
		Text:     text,
		Color:    color,
		Font:     canvasBoardFontFromSize(fontSize),
		Position: rootengine.RenderPoint{X: x, Y: y},
	}
}

func canvasBoardFontFromSize(fontSize float64) string {
	if fontSize <= 0 {
		fontSize = 14
	}
	// Pack the font size into a CSS shorthand so consumers can read it back
	// without inventing a new schema field. Renderers that ignore the Font
	// field still get a reasonable default.
	return canvasBoardIntToString(int(fontSize)) + "px system-ui, sans-serif"
}

func canvasBoardSprite(index int, node resolvedNode) rootengine.RenderSprite {
	x, _ := numericProp(node.Props, "x")
	y, _ := numericProp(node.Props, "y")
	w, _ := numericProp(node.Props, "width")
	h, _ := numericProp(node.Props, "height")
	src, _ := node.Props["src"].(string)
	return rootengine.RenderSprite{
		ID:       canvasBoardNodeID(node, index),
		Src:      src,
		Position: rootengine.RenderPoint{X: x, Y: y},
		Width:    w,
		Height:   h,
	}
}

func canvasBoardNodeID(node resolvedNode, index int) string {
	if id, ok := node.Props["id"].(string); ok && id != "" {
		return id
	}
	return canvasBoardIntToString(index)
}

func canvasBoardIntToString(n int) string {
	if n == 0 {
		return "0"
	}
	var (
		buf  [20]byte
		i    = len(buf)
		x    = n
		sign = false
	)
	if x < 0 {
		sign = true
		x = -x
	}
	for x > 0 {
		i--
		buf[i] = byte('0' + x%10)
		x /= 10
	}
	if sign {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func nestedProps(props map[string]any, key string) map[string]any {
	if v, ok := props[key]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return nil
}

func numericProp(props map[string]any, key string) (float64, bool) {
	v, ok := props[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
