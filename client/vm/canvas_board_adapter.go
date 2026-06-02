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

	// camera holds the runtime-mutable camera override installed by SetCamera
	// (drag-pan / wheel-zoom). When cameraSet is false the camera is derived
	// from props (canvasBoardCameraFromProps) exactly as before — so a board
	// that never receives an interaction event renders at its authored camera.
	// Once an event lands, the override takes strict precedence and every
	// subsequent RenderBundle uses it.
	cameraPanX float64
	cameraPanY float64
	cameraZoom float64
	cameraSet  bool
}

// CanvasBoardMinZoom and CanvasBoardMaxZoom clamp the runtime zoom so a runaway
// wheel gesture can neither invert the orthographic projection (zoom ≤ 0) nor
// blow the world scale up past anything a board author would want. The range
// mirrors the Figma/Miro convention of ~10% to ~1000% effective scale.
const (
	CanvasBoardMinZoom = 0.1
	CanvasBoardMaxZoom = 10.0
)

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
	zoom, panX, panY := rt.cameraOrProps()
	return buildCanvasBoardRenderBundleWithCamera(rt.props, nodes, zoom, panX, panY, width, height, timeSeconds)
}

// SetCamera installs a runtime camera override (drag-pan / wheel-zoom) that the
// next RenderBundle uses in place of the props-derived camera. zoom is clamped
// to [CanvasBoardMinZoom, CanvasBoardMaxZoom]; a non-positive zoom collapses to
// the minimum (never inverts the projection). This is the mutable half of the
// otherwise-declarative board: the input loop calls SetCamera, the RAF
// re-renders, and the new view paints.
func (rt *CanvasBoardAdapter) SetCamera(panX, panY, zoom float64) {
	if rt == nil {
		return
	}
	rt.cameraPanX = panX
	rt.cameraPanY = panY
	rt.cameraZoom = ClampCanvasBoardZoom(zoom)
	rt.cameraSet = true
}

// Camera returns the current live camera (pan in world units, zoom as
// world→screen scale). When no override has been installed it reflects the
// props-derived camera, so the first pan/zoom delta accumulates from where the
// board actually sits rather than from the origin. The bridge reads this to
// turn screen-space deltas into world-space camera updates.
func (rt *CanvasBoardAdapter) Camera() (panX, panY, zoom float64) {
	if rt == nil {
		return 0, 0, 1
	}
	zoom, panX, panY = rt.cameraOrProps()
	return panX, panY, zoom
}

// cameraOrProps returns the override camera when set, else the props-derived
// camera. Centralizes the precedence rule (override beats props) shared by
// RenderBundle, Camera, and PickWorld so screen↔world math stays consistent.
func (rt *CanvasBoardAdapter) cameraOrProps() (zoom, panX, panY float64) {
	if rt.cameraSet {
		return rt.cameraZoom, rt.cameraPanX, rt.cameraPanY
	}
	return canvasBoardCameraFromProps(rt.props)
}

// PickWorld hit-tests a world-space point against the board's pickable objects
// and returns the id of the TOPMOST object whose bounds contain the point.
// "Topmost" follows painter's order: later nodes paint on top, so the last
// matching node wins (matching the JS canvas2d paint order). Objects with
// pickable={false} are transparent to picking. Returns ("", false) on a miss.
//
// PickWorld consumes the same resolved-node snapshot RenderBundle renders, so
// what the author sees is exactly what picks — including the static
// props.nodes path the Muddy site-map board uses (no compiled EngineNodes).
func (rt *CanvasBoardAdapter) PickWorld(worldX, worldY float64) (string, bool) {
	if rt == nil {
		return "", false
	}
	nodes := rt.snapshot()
	if len(nodes) == 0 {
		nodes = canvasBoardNodesFromProps(rt.props)
	}
	// Walk back-to-front: the last-painted (topmost) pickable rect under the
	// cursor wins.
	for i := len(nodes) - 1; i >= 0; i-- {
		node := nodes[i]
		if !canvasBoardNodeIsRect(node) {
			continue
		}
		if !canvasBoardNodePickable(node) {
			continue
		}
		x, y, w, h := canvasBoardNodeBounds(node)
		if worldX >= x && worldX <= x+w && worldY >= y && worldY <= y+h {
			return canvasBoardNodeID(node, i), true
		}
	}
	return "", false
}

// PickWorldRect returns the ids of every PICKABLE rect node whose world-space
// bounds intersect the given world rectangle, in painter (back-to-front, slice)
// order. It is the marquee analog of PickWorld: where PickWorld returns the one
// topmost rect under a point, PickWorldRect returns the whole set a drag-box
// covers. Like PickWorld it consumes the same resolved-node snapshot RenderBundle
// renders — including the static props.nodes path the Muddy site-map board uses
// — so what the author sees is exactly what the marquee selects.
//
// Corners are normalized (min/max are sorted) so a marquee dragged up-left works
// the same as one dragged down-right. An axis-aligned bounds intersection test
// (the standard separating-axis check on each axis) decides membership, so a
// rect partially clipped by the marquee edge is still included — matching the
// DOM board's rectsIntersect. Returns nil when nothing intersects (a drag in
// empty space clears the multi-selection).
func (rt *CanvasBoardAdapter) PickWorldRect(minX, minY, maxX, maxY float64) []string {
	if rt == nil {
		return nil
	}
	// Normalize so callers can pass either corner order.
	if maxX < minX {
		minX, maxX = maxX, minX
	}
	if maxY < minY {
		minY, maxY = maxY, minY
	}
	nodes := rt.snapshot()
	if len(nodes) == 0 {
		nodes = canvasBoardNodesFromProps(rt.props)
	}
	var ids []string
	// Front (slice) order: the returned list is back-to-front so the caller's
	// "first id = primary" rule stays consistent with paint order.
	for i := range nodes {
		node := nodes[i]
		if !canvasBoardNodeIsRect(node) || !canvasBoardNodePickable(node) {
			continue
		}
		nx, ny, nw, nh := canvasBoardNodeBounds(node)
		// Axis-aligned intersection: overlap on BOTH axes.
		if nx <= maxX && nx+nw >= minX && ny <= maxY && ny+nh >= minY {
			ids = append(ids, canvasBoardNodeID(node, i))
		}
	}
	return ids
}

// NavFrom returns the id of the pickable rect node spatially nearest to
// currentID in direction dir ("up" | "down" | "left" | "right"), mirroring the
// DOM board's nearestNodeKey (sitemapruntime/island_runtime.js): the cost of a
// candidate is its primary-axis distance plus 2× its perpendicular-axis
// distance, and candidates strictly behind the pressed direction are filtered
// out (the half-plane gate). The lowest-cost candidate wins.
//
// Orientation note: the canvas paints with world +Y UP (the OrthoCamera2D Y
// flip the JS painter applies), so "up" on screen is +Y in world. The direction
// mapping below accounts for that flip so arrow-key nav lands on the node the
// user visually sees in the pressed direction — the same felt behavior as the
// DOM board, whose getBoundingClientRect coordinates have +Y DOWN.
//
// When currentID is empty (nothing selected yet), NavFrom returns the
// topmost-leftmost node — largest world-Y, then smallest world-X — exactly the
// DOM board's firstNodeKey, again accounting for the Y flip. When currentID
// matches no node, or no candidate lies in the pressed direction, it returns ""
// (the caller leaves the selection unchanged).
func (rt *CanvasBoardAdapter) NavFrom(currentID, dir string) string {
	if rt == nil {
		return ""
	}
	nodes := rt.snapshot()
	if len(nodes) == 0 {
		nodes = canvasBoardNodesFromProps(rt.props)
	}
	if strings.TrimSpace(currentID) == "" {
		return canvasBoardTopmostLeftmost(nodes)
	}
	originX, originY, ok := canvasBoardNodeCenterByID(nodes, currentID)
	if !ok {
		return ""
	}
	dir = strings.ToLower(strings.TrimSpace(dir))
	best := ""
	bestCost := -1.0
	for i := range nodes {
		node := nodes[i]
		if !canvasBoardNodeIsRect(node) || !canvasBoardNodePickable(node) {
			continue
		}
		id := canvasBoardNodeID(node, i)
		if id == currentID {
			continue
		}
		cx, cy := canvasBoardNodeCenter(node)
		dx := cx - originX
		dy := cy - originY
		var primary, perpendicular float64
		switch dir {
		case "left":
			if dx >= 0 {
				continue
			}
			primary, perpendicular = -dx, abs64(dy)
		case "right":
			if dx <= 0 {
				continue
			}
			primary, perpendicular = dx, abs64(dy)
		case "up":
			// Screen-up = larger world-Y (Y flip).
			if dy <= 0 {
				continue
			}
			primary, perpendicular = dy, abs64(dx)
		case "down":
			// Screen-down = smaller world-Y.
			if dy >= 0 {
				continue
			}
			primary, perpendicular = -dy, abs64(dx)
		default:
			return ""
		}
		cost := primary + perpendicular*2
		if bestCost < 0 || cost < bestCost {
			bestCost = cost
			best = id
		}
	}
	return best
}

// canvasBoardTopmostLeftmost returns the id of the topmost-leftmost pickable
// rect: largest world-Y (the screen-Y flip makes that the visually-highest),
// ties broken by smallest world-X. Mirrors the DOM board's firstNodeKey. Returns
// "" when there are no pickable rects.
func canvasBoardTopmostLeftmost(nodes []resolvedNode) string {
	best := ""
	var bestY, bestX float64
	have := false
	for i := range nodes {
		node := nodes[i]
		if !canvasBoardNodeIsRect(node) || !canvasBoardNodePickable(node) {
			continue
		}
		cx, cy := canvasBoardNodeCenter(node)
		// Higher on screen = larger world-Y; tie → smaller world-X.
		if !have || cy > bestY+0.5 || (abs64(cy-bestY) <= 0.5 && cx < bestX) {
			best = canvasBoardNodeID(node, i)
			bestY, bestX = cy, cx
			have = true
		}
	}
	return best
}

// canvasBoardNodeCenterByID finds the center of the pickable rect with the given
// id, returning ok=false when no such node exists.
func canvasBoardNodeCenterByID(nodes []resolvedNode, id string) (cx, cy float64, ok bool) {
	for i := range nodes {
		node := nodes[i]
		if !canvasBoardNodeIsRect(node) {
			continue
		}
		if canvasBoardNodeID(node, i) != id {
			continue
		}
		x, y := canvasBoardNodeCenter(node)
		return x, y, true
	}
	return 0, 0, false
}

// canvasBoardNodeCenter returns the world-space center of a rect node.
func canvasBoardNodeCenter(node resolvedNode) (cx, cy float64) {
	x, y, w, h := canvasBoardNodeBounds(node)
	return x + w/2, y + h/2
}

// canvasBoardNodeBounds reads a rect node's world bounds, applying the same
// zero-extent → unit-extent fallback PickWorld and canvasBoardRectObject use so
// a degenerate rect is still hit-testable.
func canvasBoardNodeBounds(node resolvedNode) (x, y, w, h float64) {
	x, _ = numericProp(node.Props, "x")
	y, _ = numericProp(node.Props, "y")
	w, _ = numericProp(node.Props, "width")
	h, _ = numericProp(node.Props, "height")
	if w == 0 {
		w = 1
	}
	if h == 0 {
		h = 1
	}
	return x, y, w, h
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// canvasBoardNodeIsRect reports whether node is a pickable rect kind. Only
// rects carry axis-aligned world bounds the simple containment test uses;
// lines/labels/sprites are not pick targets in this slice.
func canvasBoardNodeIsRect(node resolvedNode) bool {
	return strings.EqualFold(strings.TrimSpace(node.Kind), "rect")
}

// canvasBoardNodePickable mirrors canvasBoardRectObject's pickable default:
// board nodes are pickable unless the author set pickable={false}.
func canvasBoardNodePickable(node resolvedNode) bool {
	if explicit, ok := node.Props["pickable"].(bool); ok {
		return explicit
	}
	return true
}

// ClampCanvasBoardZoom constrains zoom to the sane runtime range
// [CanvasBoardMinZoom, CanvasBoardMaxZoom]. A non-positive zoom (which would
// invert or zero the projection) collapses to the minimum. Exported so the
// bridge's zoom-toward-cursor math applies the same clamp before re-pinning
// the camera.
func ClampCanvasBoardZoom(zoom float64) float64 {
	if zoom <= 0 {
		return CanvasBoardMinZoom
	}
	if zoom < CanvasBoardMinZoom {
		return CanvasBoardMinZoom
	}
	if zoom > CanvasBoardMaxZoom {
		return CanvasBoardMaxZoom
	}
	return zoom
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
	zoom, panX, panY := canvasBoardCameraFromProps(props)
	return buildCanvasBoardRenderBundleWithCamera(props, nodes, zoom, panX, panY, width, height, timeSeconds)
}

// buildCanvasBoardRenderBundleWithCamera is buildCanvasBoardRenderBundle with
// the camera supplied by the caller rather than derived from props. The adapter
// uses it to inject the runtime camera override (SetCamera) so drag-pan /
// wheel-zoom show up in the very next painted frame, while the props-derived
// path (buildCanvasBoardRenderBundle above) stays the default for boards that
// never receive an interaction event.
func buildCanvasBoardRenderBundleWithCamera(props map[string]any, nodes []resolvedNode, zoom, panX, panY float64, width, height int, timeSeconds float64) rootengine.RenderBundle {
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
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
