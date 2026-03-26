package enginevm

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

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
	props      map[string]any
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
	rawProps := parseRawProps(propsJSON)
	vmProg := &islandprogram.Program{
		Exprs: prog.Exprs,
	}
	rt := &Runtime{
		program:    prog,
		props:      rawProps,
		vm:         vm.NewVM(vmProg, vmProps(rawProps)),
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
	return rt.syncDirty()
}

// Dispose releases the retained scene snapshot.
func (rt *Runtime) Dispose() {
	for name, unsub := range rt.unsubs {
		unsub()
		delete(rt.unsubs, name)
	}
	rt.prev = nil
}

// RenderBundle builds a renderer-facing frame bundle from the current scene.
func (rt *Runtime) RenderBundle(width, height int, timeSeconds float64) rootengine.RenderBundle {
	nodes := rt.snapshot()
	return buildRenderBundle(rt.props, nodes, width, height, timeSeconds)
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

func vmProps(raw map[string]any) map[string]vm.Value {
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

func (rt *Runtime) snapshot() []resolvedNode {
	if rt == nil || len(rt.program.Nodes) == 0 {
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

func (rt *Runtime) syncDirty() []rootengine.Command {
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

type sceneCamera struct {
	X   float64
	Y   float64
	Z   float64
	FOV float64
}

type sceneObject struct {
	ID           string
	Kind         string
	Material     string
	Size         float64
	Width        float64
	Height       float64
	Depth        float64
	Radius       float64
	Segments     int
	X            float64
	Y            float64
	Z            float64
	Color        string
	RotationX    float64
	RotationY    float64
	RotationZ    float64
	SpinX        float64
	SpinY        float64
	SpinZ        float64
	Opacity      float64
	Wireframe    bool
	BlendMode    string
	Emissive     float64
	HasOpacity   bool
	HasWireframe bool
	HasBlendMode bool
	HasEmissive  bool
	Static       bool
}

type point3 struct {
	X float64
	Y float64
	Z float64
}

type projectedLine struct {
	From rootengine.RenderPoint
	To   rootengine.RenderPoint
}

func buildRenderBundle(props map[string]any, nodes []resolvedNode, width, height int, timeSeconds float64) rootengine.RenderBundle {
	if width <= 0 {
		width = 720
	}
	if height <= 0 {
		height = 420
	}

	bundle := rootengine.RenderBundle{
		Background:     sceneBackground(props),
		Materials:      []rootengine.RenderMaterial{},
		Objects:        []rootengine.RenderObject{},
		Lines:          []rootengine.RenderLine{},
		Positions:      []float64{},
		Colors:         []float64{},
		WorldPositions: []float64{},
		WorldColors:    []float64{},
	}

	camera := sceneCameraFromProps(props)
	objects := make([]sceneObject, 0, len(nodes))
	for index, node := range nodes {
		switch strings.TrimSpace(strings.ToLower(node.Kind)) {
		case "camera":
			camera = normalizeSceneCameraMap(node.Props, camera)
		case "mesh":
			objects = append(objects, sceneObjectFromResolvedNode(index, node))
		}
	}
	bundle.Camera = rootengine.RenderCamera{
		X:   camera.X,
		Y:   camera.Y,
		Z:   camera.Z,
		FOV: camera.FOV,
	}
	appendSceneGrid(&bundle, width, height)
	for _, object := range objects {
		vertexOffset := len(bundle.WorldPositions) / 3
		materialIndex := ensureRenderMaterial(&bundle, object)
		appendSceneObject(&bundle, camera, width, height, object, timeSeconds)
		vertexCount := (len(bundle.WorldPositions) / 3) - vertexOffset
		if vertexCount > 0 {
			bundle.Objects = append(bundle.Objects, rootengine.RenderObject{
				ID:            object.ID,
				Kind:          object.Kind,
				MaterialIndex: materialIndex,
				VertexOffset:  vertexOffset,
				VertexCount:   vertexCount,
				Static:        object.Static,
			})
		}
	}
	bundle.ObjectCount = len(bundle.Objects)
	bundle.VertexCount = len(bundle.Positions) / 2
	bundle.WorldVertexCount = len(bundle.WorldPositions) / 3
	return bundle
}

func sceneBackground(props map[string]any) string {
	if props != nil {
		if value, ok := props["background"].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "#08151f"
}

func sceneCameraFromProps(props map[string]any) sceneCamera {
	camera := sceneCamera{Z: 6, FOV: 75}
	if props == nil {
		return camera
	}
	raw, _ := props["camera"].(map[string]any)
	return normalizeSceneCameraMap(raw, camera)
}

func normalizeSceneCameraMap(raw map[string]any, fallback sceneCamera) sceneCamera {
	return sceneCamera{
		X:   numberFromAny(rawValue(raw, "x"), fallback.X),
		Y:   numberFromAny(rawValue(raw, "y"), fallback.Y),
		Z:   numberFromAny(rawValue(raw, "z"), fallback.Z),
		FOV: numberFromAny(rawValue(raw, "fov"), fallback.FOV),
	}
}

func sceneObjectFromResolvedNode(index int, node resolvedNode) sceneObject {
	size := numberFromAny(propValue(node.Props, "size"), 1.2)
	rawOpacity := propValue(node.Props, "opacity")
	rawWireframe := propValue(node.Props, "wireframe")
	rawBlendMode := propValue(node.Props, "blendMode")
	if rawBlendMode == nil {
		rawBlendMode = propValue(node.Props, "blend")
	}
	rawEmissive := propValue(node.Props, "emissive")
	return sceneObject{
		ID:           stringFromAny(propValue(node.Props, "id"), "scene-object-"+strconv.Itoa(index)),
		Kind:         normalizeSceneKind(stringFromAny(propValue(node.Props, "kind"), node.Geometry)),
		Material:     stringFromAny(node.Material, "flat"),
		Size:         size,
		Width:        numberFromAny(propValue(node.Props, "width"), size),
		Height:       numberFromAny(propValue(node.Props, "height"), size),
		Depth:        numberFromAny(propValue(node.Props, "depth"), size),
		Radius:       numberFromAny(propValue(node.Props, "radius"), size/2),
		Segments:     sceneSegmentResolution(propValue(node.Props, "segments")),
		X:            numberFromAny(propValue(node.Props, "x"), 0),
		Y:            numberFromAny(propValue(node.Props, "y"), 0),
		Z:            numberFromAny(propValue(node.Props, "z"), 0),
		Color:        stringFromAny(propValue(node.Props, "color"), "#8de1ff"),
		RotationX:    numberFromAny(propValue(node.Props, "rotationX"), 0),
		RotationY:    numberFromAny(propValue(node.Props, "rotationY"), 0),
		RotationZ:    numberFromAny(propValue(node.Props, "rotationZ"), 0),
		SpinX:        numberFromAny(propValue(node.Props, "spinX"), 0),
		SpinY:        numberFromAny(propValue(node.Props, "spinY"), 0),
		SpinZ:        numberFromAny(propValue(node.Props, "spinZ"), 0),
		Opacity:      clamp(numberFromAny(rawOpacity, 1), 0, 1),
		Wireframe:    boolFromAny(rawWireframe, true),
		BlendMode:    normalizeBlendMode(stringFromAny(rawBlendMode, "")),
		Emissive:     clamp(numberFromAny(rawEmissive, 0), 0, 1),
		HasOpacity:   rawOpacity != nil,
		HasWireframe: rawWireframe != nil,
		HasBlendMode: rawBlendMode != nil,
		HasEmissive:  rawEmissive != nil,
		Static:       node.Static,
	}
}

func appendSceneGrid(bundle *rootengine.RenderBundle, width, height int) {
	for x := 0; x <= width; x += 48 {
		appendSceneLine(bundle, width, height, rootengine.RenderPoint{X: float64(x), Y: 0}, rootengine.RenderPoint{X: float64(x), Y: float64(height)}, "rgba(141, 225, 255, 0.14)", 1)
	}
	for y := 0; y <= height; y += 48 {
		appendSceneLine(bundle, width, height, rootengine.RenderPoint{X: 0, Y: float64(y)}, rootengine.RenderPoint{X: float64(width), Y: float64(y)}, "rgba(141, 225, 255, 0.14)", 1)
	}
}

func appendSceneObject(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, object sceneObject, timeSeconds float64) {
	for _, segment := range sceneObjectSegments(object) {
		worldFrom := translatePoint(segment[0], object, timeSeconds)
		worldTo := translatePoint(segment[1], object, timeSeconds)
		appendWorldSceneLine(bundle, worldFrom, worldTo, object.Color)
		from := projectPoint(worldFrom, camera, width, height)
		to := projectPoint(worldTo, camera, width, height)
		if from == nil || to == nil {
			continue
		}
		appendSceneLine(bundle, width, height, *from, *to, object.Color, 1.8)
	}
}

func appendWorldSceneLine(bundle *rootengine.RenderBundle, from, to point3, color string) {
	rgba := sceneColorRGBA(color, [4]float64{0.55, 0.88, 1, 1})
	bundle.WorldPositions = append(bundle.WorldPositions,
		from.X, from.Y, from.Z,
		to.X, to.Y, to.Z,
	)
	bundle.WorldColors = append(bundle.WorldColors,
		rgba[0], rgba[1], rgba[2], rgba[3],
		rgba[0], rgba[1], rgba[2], rgba[3],
	)
}

func ensureRenderMaterial(bundle *rootengine.RenderBundle, object sceneObject) int {
	profile := resolveRenderMaterial(object)
	for index, existing := range bundle.Materials {
		if existing == profile {
			return index
		}
	}
	bundle.Materials = append(bundle.Materials, profile)
	return len(bundle.Materials) - 1
}

func resolveRenderMaterial(object sceneObject) rootengine.RenderMaterial {
	profile := rootengine.RenderMaterial{
		Kind:      stringFromAny(object.Material, "flat"),
		Color:     object.Color,
		Opacity:   1,
		Wireframe: true,
		BlendMode: "opaque",
		Emissive:  0,
	}

	switch strings.ToLower(strings.TrimSpace(profile.Kind)) {
	case "ghost":
		profile.Opacity = 0.42
		profile.BlendMode = "alpha"
		profile.Emissive = 0.12
	case "glass":
		profile.Opacity = 0.28
		profile.BlendMode = "alpha"
		profile.Emissive = 0.08
	case "glow":
		profile.Opacity = 0.92
		profile.BlendMode = "additive"
		profile.Emissive = 0.42
	case "matte":
		profile.Wireframe = true
	case "flat":
	}

	if object.HasOpacity {
		profile.Opacity = object.Opacity
	}
	if object.HasWireframe {
		profile.Wireframe = object.Wireframe
	}
	if object.HasBlendMode && object.BlendMode != "" {
		profile.BlendMode = object.BlendMode
	}
	if object.HasEmissive {
		profile.Emissive = object.Emissive
	}
	if profile.Opacity < 0.999 && profile.BlendMode == "opaque" {
		profile.BlendMode = "alpha"
	}
	return profile
}

func appendSceneLine(bundle *rootengine.RenderBundle, width, height int, from, to rootengine.RenderPoint, color string, lineWidth float64) {
	rgba := sceneColorRGBA(color, [4]float64{0.55, 0.88, 1, 1})
	bundle.Lines = append(bundle.Lines, rootengine.RenderLine{
		From:      from,
		To:        to,
		Color:     color,
		LineWidth: lineWidth,
	})
	fromX, fromY := sceneClipPoint(from, width, height)
	toX, toY := sceneClipPoint(to, width, height)
	bundle.Positions = append(bundle.Positions, fromX, fromY, toX, toY)
	bundle.Colors = append(bundle.Colors,
		rgba[0], rgba[1], rgba[2], rgba[3],
		rgba[0], rgba[1], rgba[2], rgba[3],
	)
}

func sceneClipPoint(point rootengine.RenderPoint, width, height int) (float64, float64) {
	return (point.X/float64(width))*2 - 1, 1 - (point.Y/float64(height))*2
}

func sceneObjectSegments(object sceneObject) [][2]point3 {
	switch object.Kind {
	case "box", "cube":
		return boxSegments(object)
	case "plane":
		return planeSegments(object)
	case "pyramid":
		return pyramidSegments(object)
	case "sphere":
		return sphereSegments(object)
	default:
		return boxSegments(object)
	}
}

func boxSegments(object sceneObject) [][2]point3 {
	return indexSegments(boxVertices(object.Width, object.Height, object.Depth), [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{4, 5}, {5, 6}, {6, 7}, {7, 4},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
	})
}

func planeSegments(object sceneObject) [][2]point3 {
	vertices := boxVertices(object.Width, 0, object.Depth)
	return indexSegments(vertices[:4], [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
	})
}

func pyramidSegments(object sceneObject) [][2]point3 {
	halfWidth := object.Width / 2
	halfDepth := object.Depth / 2
	halfHeight := object.Height / 2
	vertices := []point3{
		{X: -halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: -halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: 0, Y: halfHeight, Z: 0},
	}
	return indexSegments(vertices, [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{0, 4}, {1, 4}, {2, 4}, {3, 4},
	})
}

func sphereSegments(object sceneObject) [][2]point3 {
	out := circleSegments(object.Radius, "xy", object.Segments)
	out = append(out, circleSegments(object.Radius, "xz", object.Segments)...)
	out = append(out, circleSegments(object.Radius, "yz", object.Segments)...)
	return out
}

func boxVertices(width, height, depth float64) []point3 {
	halfWidth := width / 2
	halfHeight := height / 2
	halfDepth := depth / 2
	return []point3{
		{X: -halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: halfHeight, Z: -halfDepth},
		{X: -halfWidth, Y: halfHeight, Z: -halfDepth},
		{X: -halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: halfWidth, Y: halfHeight, Z: halfDepth},
		{X: -halfWidth, Y: halfHeight, Z: halfDepth},
	}
}

func indexSegments(points []point3, edgePairs [][2]int) [][2]point3 {
	out := make([][2]point3, 0, len(edgePairs))
	for _, edge := range edgePairs {
		out = append(out, [2]point3{points[edge[0]], points[edge[1]]})
	}
	return out
}

func circleSegments(radius float64, axis string, segments int) [][2]point3 {
	points := make([]point3, 0, segments)
	for i := 0; i < segments; i++ {
		angle := (math.Pi * 2 * float64(i)) / float64(segments)
		points = append(points, circlePoint(radius, axis, angle))
	}
	out := make([][2]point3, 0, len(points))
	for i := range points {
		out = append(out, [2]point3{points[i], points[(i+1)%len(points)]})
	}
	return out
}

func circlePoint(radius float64, axis string, angle float64) point3 {
	sin := math.Sin(angle) * radius
	cos := math.Cos(angle) * radius
	switch axis {
	case "xy":
		return point3{X: cos, Y: sin, Z: 0}
	case "yz":
		return point3{X: 0, Y: cos, Z: sin}
	default:
		return point3{X: cos, Y: 0, Z: sin}
	}
}

func translatePoint(point point3, object sceneObject, timeSeconds float64) point3 {
	rotated := rotatePoint(point,
		object.RotationX+object.SpinX*timeSeconds,
		object.RotationY+object.SpinY*timeSeconds,
		object.RotationZ+object.SpinZ*timeSeconds,
	)
	return point3{
		X: rotated.X + object.X,
		Y: rotated.Y + object.Y,
		Z: rotated.Z + object.Z,
	}
}

func rotatePoint(point point3, rotationX, rotationY, rotationZ float64) point3 {
	x := point.X
	y := point.Y
	z := point.Z

	sinX, cosX := math.Sin(rotationX), math.Cos(rotationX)
	nextY := y*cosX - z*sinX
	nextZ := y*sinX + z*cosX
	y, z = nextY, nextZ

	sinY, cosY := math.Sin(rotationY), math.Cos(rotationY)
	nextX := x*cosY + z*sinY
	nextZ = -x*sinY + z*cosY
	x, z = nextX, nextZ

	sinZ, cosZ := math.Sin(rotationZ), math.Cos(rotationZ)
	nextX = x*cosZ - y*sinZ
	nextY = x*sinZ + y*cosZ

	return point3{X: nextX, Y: nextY, Z: z}
}

func projectPoint(point point3, camera sceneCamera, width, height int) *rootengine.RenderPoint {
	depth := point.Z + camera.Z
	if depth <= 0.15 {
		return nil
	}
	focal := (math.Min(float64(width), float64(height)) / 2) / math.Tan((camera.FOV*math.Pi)/360)
	return &rootengine.RenderPoint{
		X: float64(width)/2 + ((point.X-camera.X)*focal)/depth,
		Y: float64(height)/2 - ((point.Y-camera.Y)*focal)/depth,
	}
}

func normalizeSceneKind(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "box", "plane", "pyramid", "sphere":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "cube"
	}
}

func sceneSegmentResolution(value any) int {
	segments := int(math.Round(numberFromAny(value, 12)))
	if segments < 6 {
		return 6
	}
	if segments > 24 {
		return 24
	}
	return segments
}

func sceneColorRGBA(value string, fallback [4]float64) [4]float64 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	if strings.HasPrefix(trimmed, "#") {
		hex := strings.TrimPrefix(trimmed, "#")
		if len(hex) == 3 {
			return [4]float64{
				float64(parseHexByte(string([]byte{hex[0], hex[0]}))) / 255,
				float64(parseHexByte(string([]byte{hex[1], hex[1]}))) / 255,
				float64(parseHexByte(string([]byte{hex[2], hex[2]}))) / 255,
				1,
			}
		}
		if len(hex) == 6 {
			return [4]float64{
				float64(parseHexByte(hex[0:2])) / 255,
				float64(parseHexByte(hex[2:4])) / 255,
				float64(parseHexByte(hex[4:6])) / 255,
				1,
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "rgb(") || strings.HasPrefix(strings.ToLower(trimmed), "rgba(") {
		start := strings.Index(trimmed, "(")
		end := strings.LastIndex(trimmed, ")")
		if start >= 0 && end > start {
			parts := strings.Split(trimmed[start+1:end], ",")
			if len(parts) >= 3 {
				rgba := fallback
				rgba[0] = clamp(numberFromAny(strings.TrimSpace(parts[0]), fallback[0]*255), 0, 255) / 255
				rgba[1] = clamp(numberFromAny(strings.TrimSpace(parts[1]), fallback[1]*255), 0, 255) / 255
				rgba[2] = clamp(numberFromAny(strings.TrimSpace(parts[2]), fallback[2]*255), 0, 255) / 255
				if len(parts) > 3 {
					rgba[3] = clamp(numberFromAny(strings.TrimSpace(parts[3]), fallback[3]), 0, 1)
				}
				return rgba
			}
		}
	}
	return fallback
}

func rawValue(values map[string]any, key string) any {
	if values == nil {
		return nil
	}
	return values[key]
}

func propValue(values map[string]any, key string) any {
	return rawValue(values, key)
}

func numberFromAny(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		if parsed, err := json.Number(strings.TrimSpace(typed)).Float64(); err == nil {
			return parsed
		}
	}
	return fallback
}

func stringFromAny(value any, fallback string) string {
	if typed, ok := value.(string); ok && strings.TrimSpace(typed) != "" {
		return typed
	}
	return fallback
}

func normalizeBlendMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "alpha", "blend", "transparent":
		return "alpha"
	case "add", "additive", "glow", "emissive":
		return "additive"
	case "opaque":
		return "opaque"
	default:
		return ""
	}
}

func boolFromAny(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return fallback
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func parseHexByte(value string) uint64 {
	var parsed uint64
	for _, ch := range value {
		parsed <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			parsed += uint64(ch - '0')
		case ch >= 'a' && ch <= 'f':
			parsed += uint64(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			parsed += uint64(ch-'A') + 10
		}
	}
	return parsed
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
