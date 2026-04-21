package enginevm

import (
	"encoding/json"
	"hash/fnv"
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
	X         float64
	Y         float64
	Z         float64
	RotationX float64
	RotationY float64
	RotationZ float64
	FOV       float64
	Near      float64
	Far       float64
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
	Points       []point3
	LineSegments [][2]int
	X            float64
	Y            float64
	Z            float64
	Color        string
	Texture      string
	RotationX    float64
	RotationY    float64
	RotationZ    float64
	SpinX        float64
	SpinY        float64
	SpinZ        float64
	ShiftX       float64
	ShiftY       float64
	ShiftZ       float64
	DriftSpeed   float64
	DriftPhase   float64
	Opacity      float64
	Wireframe    bool
	BlendMode    string
	Emissive     float64
	Pickable     *bool
	HasTexture   bool
	HasOpacity   bool
	HasWireframe bool
	HasBlendMode bool
	HasEmissive  bool
	Static       bool
}

type sceneLabel struct {
	ID          string
	Text        string
	ClassName   string
	X           float64
	Y           float64
	Z           float64
	Priority    float64
	ShiftX      float64
	ShiftY      float64
	ShiftZ      float64
	DriftSpeed  float64
	DriftPhase  float64
	MaxWidth    float64
	MaxLines    int
	Overflow    string
	Font        string
	LineHeight  float64
	Color       string
	Background  string
	BorderColor string
	OffsetX     float64
	OffsetY     float64
	AnchorX     float64
	AnchorY     float64
	Collision   string
	Occlude     bool
	WhiteSpace  string
	TextAlign   string
}

type sceneSprite struct {
	ID         string
	Src        string
	ClassName  string
	X          float64
	Y          float64
	Z          float64
	Priority   float64
	ShiftX     float64
	ShiftY     float64
	ShiftZ     float64
	DriftSpeed float64
	DriftPhase float64
	Width      float64
	Height     float64
	Scale      float64
	Opacity    float64
	OffsetX    float64
	OffsetY    float64
	AnchorX    float64
	AnchorY    float64
	Occlude    bool
	Fit        string
}

type sceneLight struct {
	ID         string
	Kind       string
	Color      string
	Intensity  float64
	X          float64
	Y          float64
	Z          float64
	DirectionX float64
	DirectionY float64
	DirectionZ float64
	Range      float64
	Decay      float64
}

type sceneEnvironment struct {
	AmbientColor     string
	AmbientIntensity float64
	SkyColor         string
	SkyIntensity     float64
	GroundColor      string
	GroundIntensity  float64
	Exposure         float64
	ToneMapping      string
	Specified        bool
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

type sceneAppendResult struct {
	Bounds     rootengine.RenderBounds
	HasBounds  bool
	ViewCulled bool
}

func buildRenderBundle(props map[string]any, nodes []resolvedNode, width, height int, timeSeconds float64) rootengine.RenderBundle {
	if width <= 0 {
		width = 720
	}
	if height <= 0 {
		height = 420
	}

	bundle := rootengine.RenderBundle{
		Background:      sceneBackground(props),
		Materials:       []rootengine.RenderMaterial{},
		Objects:         []rootengine.RenderObject{},
		Surfaces:        []rootengine.RenderSurface{},
		Lights:          []rootengine.RenderLight{},
		Lines:           []rootengine.RenderLine{},
		Labels:          []rootengine.RenderLabel{},
		Sprites:         []rootengine.RenderSprite{},
		Positions:       []float64{},
		Colors:          []float64{},
		WorldPositions:  []float64{},
		WorldColors:     []float64{},
		PostFXMaxPixels: int(math.Max(0, math.Floor(numberFromAny(sceneValue(props, "postFXMaxPixels"), numberFromAny(propValue(props, "postFXMaxPixels"), 0))))),
	}

	camera := sceneCameraFromProps(props)
	lights := sceneLightsFromProps(props)
	sprites := sceneSpritesFromProps(props)
	objects := make([]sceneObject, 0, len(nodes))
	labels := make([]sceneLabel, 0, len(nodes))
	for index, node := range nodes {
		switch strings.TrimSpace(strings.ToLower(node.Kind)) {
		case "camera":
			camera = normalizeSceneCameraMap(node.Props, camera)
		case "light":
			if light, ok := sceneLightFromResolvedNode(index, node); ok {
				lights = append(lights, light)
			}
		case "mesh":
			objects = append(objects, sceneObjectFromResolvedNode(index, node))
		case "label":
			if label, ok := sceneLabelFromResolvedNode(index, node); ok {
				labels = append(labels, label)
			}
		case "sprite":
			if sprite, ok := sceneSpriteFromResolvedNode(index, node); ok {
				sprites = append(sprites, sprite)
			}
		}
	}
	environment := resolveSceneEnvironment(props, len(lights) > 0)
	bundle.Camera = rootengine.RenderCamera{
		X:         camera.X,
		Y:         camera.Y,
		Z:         camera.Z,
		RotationX: camera.RotationX,
		RotationY: camera.RotationY,
		RotationZ: camera.RotationZ,
		FOV:       camera.FOV,
		Near:      camera.Near,
		Far:       camera.Far,
	}
	bundle.Environment = renderSceneEnvironment(environment)
	if len(lights) > 0 {
		bundle.Lights = renderSceneLights(lights)
	}
	appendSceneGrid(&bundle, width, height)
	for _, object := range objects {
		vertexOffset := len(bundle.WorldPositions) / 3
		materialIndex := ensureRenderMaterial(&bundle, object)
		material := bundle.Materials[materialIndex]
		appendResult := appendSceneObject(&bundle, camera, width, height, object, material, lights, environment, timeSeconds)
		vertexCount := (len(bundle.WorldPositions) / 3) - vertexOffset
		if vertexCount > 0 || appendResult.HasBounds || appendResult.ViewCulled {
			bounds := appendResult.Bounds
			if !appendResult.HasBounds && vertexCount > 0 {
				bounds = renderObjectBounds(bundle.WorldPositions, vertexOffset, vertexCount)
			}
			depthNear, depthFar, depthCenter := renderBoundsDepthMetrics(bounds, camera)
			bundle.Objects = append(bundle.Objects, rootengine.RenderObject{
				ID:            object.ID,
				Kind:          object.Kind,
				Pickable:      object.Pickable,
				MaterialIndex: materialIndex,
				RenderPass:    bundle.Materials[materialIndex].RenderPass,
				VertexOffset:  vertexOffset,
				VertexCount:   vertexCount,
				Static:        object.Static,
				Bounds:        bounds,
				DepthNear:     depthNear,
				DepthFar:      depthFar,
				DepthCenter:   depthCenter,
				ViewCulled:    appendResult.ViewCulled,
			})
			appendSceneSurface(&bundle, camera, width, height, object, materialIndex, material, bounds, timeSeconds)
		}
	}
	for _, label := range labels {
		appendSceneLabel(&bundle, camera, width, height, label, timeSeconds)
	}
	for _, sprite := range sprites {
		appendSceneSprite(&bundle, camera, width, height, sprite, timeSeconds)
	}
	bundle.ObjectCount = len(bundle.Objects)
	bundle.VertexCount = len(bundle.Positions) / 2
	bundle.WorldVertexCount = len(bundle.WorldPositions) / 3
	bundle.Passes = buildRenderPassBundles(bundle)
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
	camera := sceneCamera{Z: 6, FOV: 75, Near: 0.05, Far: 128}
	if props == nil {
		return camera
	}
	raw, _ := props["camera"].(map[string]any)
	return normalizeSceneCameraMap(raw, camera)
}

func normalizeSceneCameraMap(raw map[string]any, fallback sceneCamera) sceneCamera {
	return sceneCamera{
		X:         numberFromAny(rawValue(raw, "x"), fallback.X),
		Y:         numberFromAny(rawValue(raw, "y"), fallback.Y),
		Z:         numberFromAny(rawValue(raw, "z"), fallback.Z),
		RotationX: numberFromAny(rawValue(raw, "rotationX"), fallback.RotationX),
		RotationY: numberFromAny(rawValue(raw, "rotationY"), fallback.RotationY),
		RotationZ: numberFromAny(rawValue(raw, "rotationZ"), fallback.RotationZ),
		FOV:       numberFromAny(rawValue(raw, "fov"), fallback.FOV),
		Near:      numberFromAny(rawValue(raw, "near"), fallback.Near),
		Far:       numberFromAny(rawValue(raw, "far"), fallback.Far),
	}
}

func sceneLightsFromProps(props map[string]any) []sceneLight {
	raw := sceneLightList(sceneValue(props, "lights"))
	if len(raw) == 0 {
		raw = sceneLightList(rawValue(props, "lights"))
	}
	if len(raw) == 0 {
		return nil
	}
	out := make([]sceneLight, 0, len(raw))
	for index, item := range raw {
		if light, ok := normalizeSceneLightMap(item, "scene-light-"+strconv.Itoa(index), sceneLight{}); ok {
			out = append(out, light)
		}
	}
	return out
}

func sceneLightList(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		if len(items) == 0 {
			return nil
		}
		return append([]map[string]any(nil), items...)
	case []any:
		if len(items) == 0 {
			return nil
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			mapped, ok := item.(map[string]any)
			if ok {
				out = append(out, mapped)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func sceneSpritesFromProps(props map[string]any) []sceneSprite {
	raw := sceneSpriteList(sceneValue(props, "sprites"))
	if len(raw) == 0 {
		raw = sceneSpriteList(rawValue(props, "sprites"))
	}
	if len(raw) == 0 {
		return nil
	}
	out := make([]sceneSprite, 0, len(raw))
	for index, item := range raw {
		if sprite, ok := normalizeSceneSpriteMap(item, "scene-sprite-"+strconv.Itoa(index), sceneSprite{}); ok {
			out = append(out, sprite)
		}
	}
	return out
}

func sceneSpriteList(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		if len(items) == 0 {
			return nil
		}
		return append([]map[string]any(nil), items...)
	case []any:
		if len(items) == 0 {
			return nil
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			mapped, ok := item.(map[string]any)
			if ok {
				out = append(out, mapped)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func sceneEnvironmentFromProps(props map[string]any) sceneEnvironment {
	raw, _ := sceneValue(props, "environment").(map[string]any)
	if raw == nil {
		raw, _ = rawValue(props, "environment").(map[string]any)
	}
	return normalizeSceneEnvironment(raw, sceneEnvironment{})
}

func resolveSceneEnvironment(props map[string]any, hasLights bool) sceneEnvironment {
	environment := sceneEnvironmentFromProps(props)
	if environment.Exposure <= 0 {
		environment.Exposure = 1
	}
	if environment.Specified || !hasLights {
		return environment
	}
	environment.AmbientColor = "#f5fbff"
	environment.AmbientIntensity = 0.18
	environment.SkyColor = "#d5ebff"
	environment.SkyIntensity = 0.12
	environment.GroundColor = "#102030"
	environment.GroundIntensity = 0.04
	return environment
}

func normalizeSceneEnvironment(raw map[string]any, fallback sceneEnvironment) sceneEnvironment {
	exposure := fallback.Exposure
	if raw != nil && rawValue(raw, "exposure") != nil {
		exposure = numberFromAny(rawValue(raw, "exposure"), fallback.Exposure)
	}
	if exposure == 0 {
		exposure = 1
	}
	toneMapping := strings.TrimSpace(stringFromAny(rawValue(raw, "toneMapping"), fallback.ToneMapping))
	environment := sceneEnvironment{
		AmbientColor:     strings.TrimSpace(stringFromAny(rawValue(raw, "ambientColor"), fallback.AmbientColor)),
		AmbientIntensity: clamp(numberFromAny(rawValue(raw, "ambientIntensity"), fallback.AmbientIntensity), 0, 4),
		SkyColor:         strings.TrimSpace(stringFromAny(rawValue(raw, "skyColor"), fallback.SkyColor)),
		SkyIntensity:     clamp(numberFromAny(rawValue(raw, "skyIntensity"), fallback.SkyIntensity), 0, 4),
		GroundColor:      strings.TrimSpace(stringFromAny(rawValue(raw, "groundColor"), fallback.GroundColor)),
		GroundIntensity:  clamp(numberFromAny(rawValue(raw, "groundIntensity"), fallback.GroundIntensity), 0, 4),
		Exposure:         clamp(exposure, 0.05, 4),
		ToneMapping:      toneMapping,
		Specified:        false,
	}
	if raw == nil {
		return environment
	}
	environment.Specified = environment.AmbientColor != "" ||
		environment.AmbientIntensity != 0 ||
		environment.SkyColor != "" ||
		environment.SkyIntensity != 0 ||
		environment.GroundColor != "" ||
		environment.GroundIntensity != 0 ||
		rawValue(raw, "exposure") != nil
	return environment
}

func sceneLightFromResolvedNode(index int, node resolvedNode) (sceneLight, bool) {
	return normalizeSceneLightMap(node.Props, "scene-light-"+strconv.Itoa(index), sceneLight{})
}

func normalizeSceneLightMap(raw map[string]any, fallbackID string, fallback sceneLight) (sceneLight, bool) {
	if raw == nil {
		return sceneLight{}, false
	}
	kind := normalizeSceneLightKind(stringFromAny(propValue(raw, "kind"), stringFromAny(propValue(raw, "lightKind"), fallback.Kind)))
	if kind == "" {
		return sceneLight{}, false
	}
	intensityFallback := fallback.Intensity
	if intensityFallback == 0 {
		intensityFallback = defaultSceneLightIntensity(kind)
	}
	rangeFallback := fallback.Range
	if kind == "point" && rangeFallback == 0 {
		rangeFallback = 6.5
	}
	decayFallback := fallback.Decay
	if kind == "point" && decayFallback == 0 {
		decayFallback = 1.35
	}
	light := sceneLight{
		ID:         stringFromAny(propValue(raw, "id"), fallbackID),
		Kind:       kind,
		Color:      stringFromAny(propValue(raw, "color"), "#f3fbff"),
		Intensity:  clamp(numberFromAny(propValue(raw, "intensity"), intensityFallback), 0, 6),
		X:          numberFromAny(propValue(raw, "x"), fallback.X),
		Y:          numberFromAny(propValue(raw, "y"), fallback.Y),
		Z:          numberFromAny(propValue(raw, "z"), fallback.Z),
		DirectionX: numberFromAny(propValue(raw, "directionX"), fallback.DirectionX),
		DirectionY: numberFromAny(propValue(raw, "directionY"), fallback.DirectionY),
		DirectionZ: numberFromAny(propValue(raw, "directionZ"), fallback.DirectionZ),
		Range:      clamp(numberFromAny(propValue(raw, "range"), rangeFallback), 0, 256),
		Decay:      clamp(numberFromAny(propValue(raw, "decay"), decayFallback), 0.1, 8),
	}
	if kind == "directional" && light.DirectionX == 0 && light.DirectionY == 0 && light.DirectionZ == 0 {
		light.DirectionX = 0.35
		light.DirectionY = -1
		light.DirectionZ = -0.4
	}
	return light, true
}

func normalizeSceneLightKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ambient":
		return "ambient"
	case "directional", "sun":
		return "directional"
	case "point":
		return "point"
	default:
		return ""
	}
}

func defaultSceneLightIntensity(kind string) float64 {
	switch kind {
	case "ambient":
		return 0.28
	case "directional":
		return 1
	case "point":
		return 1.1
	default:
		return 1
	}
}

func renderSceneLights(lights []sceneLight) []rootengine.RenderLight {
	if len(lights) == 0 {
		return nil
	}
	out := make([]rootengine.RenderLight, 0, len(lights))
	for _, light := range lights {
		out = append(out, rootengine.RenderLight{
			ID:         light.ID,
			Kind:       light.Kind,
			Color:      light.Color,
			Intensity:  light.Intensity,
			X:          light.X,
			Y:          light.Y,
			Z:          light.Z,
			DirectionX: light.DirectionX,
			DirectionY: light.DirectionY,
			DirectionZ: light.DirectionZ,
			Range:      light.Range,
			Decay:      light.Decay,
		})
	}
	return out
}

func renderSceneEnvironment(environment sceneEnvironment) rootengine.RenderEnvironment {
	return rootengine.RenderEnvironment{
		AmbientColor:     environment.AmbientColor,
		AmbientIntensity: environment.AmbientIntensity,
		SkyColor:         environment.SkyColor,
		SkyIntensity:     environment.SkyIntensity,
		GroundColor:      environment.GroundColor,
		GroundIntensity:  environment.GroundIntensity,
		Exposure:         environment.Exposure,
		ToneMapping:      environment.ToneMapping,
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
	rawTexture := propValue(node.Props, "texture")
	rawPickable := propValue(node.Props, "pickable")
	kind := normalizeSceneKind(stringFromAny(propValue(node.Props, "kind"), node.Geometry))
	points := scenePointList(propValue(node.Props, "points"))
	lineMetrics := sceneLineGeometryMetrics(points)
	return sceneObject{
		ID:           stringFromAny(propValue(node.Props, "id"), "scene-object-"+strconv.Itoa(index)),
		Kind:         kind,
		Material:     stringFromAny(node.Material, "flat"),
		Size:         size,
		Width:        numberFromAny(propValue(node.Props, "width"), fallbackLineMetric(lineMetrics, "width", size)),
		Height:       numberFromAny(propValue(node.Props, "height"), fallbackLineMetric(lineMetrics, "height", size)),
		Depth:        numberFromAny(propValue(node.Props, "depth"), fallbackSceneDepth(kind, node.Props, lineMetrics, size)),
		Radius:       numberFromAny(propValue(node.Props, "radius"), fallbackLineMetric(lineMetrics, "radius", size/2)),
		Segments:     sceneSegmentResolution(propValue(node.Props, "segments")),
		Points:       points,
		LineSegments: sceneLineSegments(propValue(node.Props, "segments"), len(points)),
		X:            numberFromAny(propValue(node.Props, "x"), 0),
		Y:            numberFromAny(propValue(node.Props, "y"), 0),
		Z:            numberFromAny(propValue(node.Props, "z"), 0),
		Color:        stringFromAny(propValue(node.Props, "color"), "#8de1ff"),
		Texture:      strings.TrimSpace(stringFromAny(rawTexture, "")),
		RotationX:    numberFromAny(propValue(node.Props, "rotationX"), 0),
		RotationY:    numberFromAny(propValue(node.Props, "rotationY"), 0),
		RotationZ:    numberFromAny(propValue(node.Props, "rotationZ"), 0),
		SpinX:        numberFromAny(propValue(node.Props, "spinX"), 0),
		SpinY:        numberFromAny(propValue(node.Props, "spinY"), 0),
		SpinZ:        numberFromAny(propValue(node.Props, "spinZ"), 0),
		ShiftX:       numberFromAny(propValue(node.Props, "shiftX"), 0),
		ShiftY:       numberFromAny(propValue(node.Props, "shiftY"), 0),
		ShiftZ:       numberFromAny(propValue(node.Props, "shiftZ"), 0),
		DriftSpeed:   numberFromAny(propValue(node.Props, "driftSpeed"), 0),
		DriftPhase:   numberFromAny(propValue(node.Props, "driftPhase"), 0),
		Opacity:      clamp(numberFromAny(rawOpacity, 1), 0, 1),
		Wireframe:    boolFromAny(rawWireframe, rawTexture == nil),
		BlendMode:    normalizeBlendMode(stringFromAny(rawBlendMode, "")),
		Emissive:     clamp(numberFromAny(rawEmissive, 0), 0, 1),
		Pickable:     boolPtrFromAny(rawPickable),
		HasTexture:   rawTexture != nil,
		HasOpacity:   rawOpacity != nil,
		HasWireframe: rawWireframe != nil,
		HasBlendMode: rawBlendMode != nil,
		HasEmissive:  rawEmissive != nil,
		Static:       node.Static,
	}
}

func sceneLabelFromResolvedNode(index int, node resolvedNode) (sceneLabel, bool) {
	text := stringFromAny(propValue(node.Props, "text"), "")
	if strings.TrimSpace(text) == "" {
		return sceneLabel{}, false
	}
	return sceneLabel{
		ID:          stringFromAny(propValue(node.Props, "id"), "scene-label-"+strconv.Itoa(index)),
		Text:        text,
		ClassName:   sceneLabelClassName(node.Props),
		X:           numberFromAny(propValue(node.Props, "x"), 0),
		Y:           numberFromAny(propValue(node.Props, "y"), 0),
		Z:           numberFromAny(propValue(node.Props, "z"), 0),
		Priority:    numberFromAny(propValue(node.Props, "priority"), 0),
		ShiftX:      numberFromAny(propValue(node.Props, "shiftX"), 0),
		ShiftY:      numberFromAny(propValue(node.Props, "shiftY"), 0),
		ShiftZ:      numberFromAny(propValue(node.Props, "shiftZ"), 0),
		DriftSpeed:  numberFromAny(propValue(node.Props, "driftSpeed"), 0),
		DriftPhase:  numberFromAny(propValue(node.Props, "driftPhase"), 0),
		MaxWidth:    math.Max(48, numberFromAny(propValue(node.Props, "maxWidth"), 180)),
		MaxLines:    int(math.Max(0, numberFromAny(propValue(node.Props, "maxLines"), 0))),
		Overflow:    normalizeSceneLabelOverflow(stringFromAny(propValue(node.Props, "overflow"), "")),
		Font:        stringFromAny(propValue(node.Props, "font"), `600 13px "IBM Plex Sans", "Segoe UI", sans-serif`),
		LineHeight:  math.Max(12, numberFromAny(propValue(node.Props, "lineHeight"), 18)),
		Color:       stringFromAny(propValue(node.Props, "color"), "#ecf7ff"),
		Background:  stringFromAny(propValue(node.Props, "background"), "rgba(8, 21, 31, 0.82)"),
		BorderColor: stringFromAny(propValue(node.Props, "borderColor"), "rgba(141, 225, 255, 0.24)"),
		OffsetX:     numberFromAny(propValue(node.Props, "offsetX"), 0),
		OffsetY:     numberFromAny(propValue(node.Props, "offsetY"), -14),
		AnchorX:     clamp(numberFromAny(propValue(node.Props, "anchorX"), 0.5), 0, 1),
		AnchorY:     clamp(numberFromAny(propValue(node.Props, "anchorY"), 1), 0, 1),
		Collision:   normalizeSceneLabelCollision(stringFromAny(propValue(node.Props, "collision"), "avoid")),
		Occlude:     boolFromAny(propValue(node.Props, "occlude"), false),
		WhiteSpace:  normalizeSceneLabelWhiteSpace(stringFromAny(propValue(node.Props, "whiteSpace"), "normal")),
		TextAlign:   normalizeSceneLabelAlign(stringFromAny(propValue(node.Props, "textAlign"), "center")),
	}, true
}

func sceneSpriteFromResolvedNode(index int, node resolvedNode) (sceneSprite, bool) {
	return normalizeSceneSpriteMap(node.Props, "scene-sprite-"+strconv.Itoa(index), sceneSprite{})
}

func normalizeSceneSpriteMap(raw map[string]any, fallbackID string, fallback sceneSprite) (sceneSprite, bool) {
	if raw == nil {
		return sceneSprite{}, false
	}
	src := strings.TrimSpace(stringFromAny(propValue(raw, "src"), fallback.Src))
	if src == "" {
		return sceneSprite{}, false
	}
	width := numberFromAny(propValue(raw, "width"), fallback.Width)
	if width <= 0 {
		width = 1.25
	}
	height := numberFromAny(propValue(raw, "height"), fallback.Height)
	if height <= 0 {
		height = width
	}
	scale := numberFromAny(propValue(raw, "scale"), fallback.Scale)
	if scale <= 0 {
		scale = 1
	}
	opacity := clamp(numberFromAny(propValue(raw, "opacity"), fallback.Opacity), 0, 1)
	if fallback.Opacity == 0 && rawValue(raw, "opacity") == nil {
		opacity = 1
	}
	anchorX := clamp(numberFromAny(propValue(raw, "anchorX"), fallback.AnchorX), 0, 1)
	if fallback.AnchorX == 0 && rawValue(raw, "anchorX") == nil {
		anchorX = 0.5
	}
	anchorY := clamp(numberFromAny(propValue(raw, "anchorY"), fallback.AnchorY), 0, 1)
	if fallback.AnchorY == 0 && rawValue(raw, "anchorY") == nil {
		anchorY = 0.5
	}
	return sceneSprite{
		ID:         stringFromAny(propValue(raw, "id"), fallbackID),
		Src:        src,
		ClassName:  sceneLabelClassName(raw),
		X:          numberFromAny(propValue(raw, "x"), fallback.X),
		Y:          numberFromAny(propValue(raw, "y"), fallback.Y),
		Z:          numberFromAny(propValue(raw, "z"), fallback.Z),
		Priority:   numberFromAny(propValue(raw, "priority"), fallback.Priority),
		ShiftX:     numberFromAny(propValue(raw, "shiftX"), fallback.ShiftX),
		ShiftY:     numberFromAny(propValue(raw, "shiftY"), fallback.ShiftY),
		ShiftZ:     numberFromAny(propValue(raw, "shiftZ"), fallback.ShiftZ),
		DriftSpeed: numberFromAny(propValue(raw, "driftSpeed"), fallback.DriftSpeed),
		DriftPhase: numberFromAny(propValue(raw, "driftPhase"), fallback.DriftPhase),
		Width:      width,
		Height:     height,
		Scale:      scale,
		Opacity:    opacity,
		OffsetX:    numberFromAny(propValue(raw, "offsetX"), fallback.OffsetX),
		OffsetY:    numberFromAny(propValue(raw, "offsetY"), fallback.OffsetY),
		AnchorX:    anchorX,
		AnchorY:    anchorY,
		Occlude:    boolFromAny(propValue(raw, "occlude"), fallback.Occlude),
		Fit:        normalizeSceneSpriteFit(stringFromAny(propValue(raw, "fit"), fallback.Fit)),
	}, true
}

func appendSceneGrid(bundle *rootengine.RenderBundle, width, height int) {
	for x := 0; x <= width; x += 48 {
		appendSceneLine(bundle, width, height, rootengine.RenderPoint{X: float64(x), Y: 0}, rootengine.RenderPoint{X: float64(x), Y: float64(height)}, "rgba(141, 225, 255, 0.14)", 1)
	}
	for y := 0; y <= height; y += 48 {
		appendSceneLine(bundle, width, height, rootengine.RenderPoint{X: 0, Y: float64(y)}, rootengine.RenderPoint{X: float64(width), Y: float64(y)}, "rgba(141, 225, 255, 0.14)", 1)
	}
}

func appendSceneObject(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, object sceneObject, material rootengine.RenderMaterial, lights []sceneLight, environment sceneEnvironment, timeSeconds float64) sceneAppendResult {
	aspect := math.Max(0.0001, float64(width)/math.Max(1, float64(height)))
	result := sceneAppendResult{}
	if !sceneObjectUsesLineGeometry(object, material) && sceneObjectHasTexturedSurface(object, material) {
		for _, corner := range scenePlaneSurfaceCorners(object, timeSeconds) {
			result.Bounds, result.HasBounds = expandRenderBounds(result.Bounds, result.HasBounds, corner)
		}
		if result.HasBounds {
			result.ViewCulled = renderBoundsOutsideFrustum(result.Bounds, camera, width, height)
		}
		return result
	}
	for _, segment := range sceneObjectSegments(object) {
		worldFrom := translatePoint(segment[0], object, timeSeconds)
		worldTo := translatePoint(segment[1], object, timeSeconds)
		fromNormal := sceneObjectWorldNormal(object, segment[0], timeSeconds)
		toNormal := sceneObjectWorldNormal(object, segment[1], timeSeconds)
		fromRGBA := sceneLitColorRGBA(material, worldFrom, fromNormal, lights, environment)
		toRGBA := sceneLitColorRGBA(material, worldTo, toNormal, lights, environment)
		result.Bounds, result.HasBounds = expandRenderBounds(result.Bounds, result.HasBounds, worldFrom)
		result.Bounds, result.HasBounds = expandRenderBounds(result.Bounds, result.HasBounds, worldTo)
		clippedFrom, clippedTo, ok := clipWorldSegmentForCamera(worldFrom, worldTo, camera, aspect)
		if !ok {
			continue
		}
		appendWorldSceneLine(bundle, clippedFrom, clippedTo, fromRGBA, toRGBA)
		from := projectPoint(clippedFrom, camera, width, height)
		to := projectPoint(clippedTo, camera, width, height)
		if from == nil || to == nil {
			continue
		}
		stroke := mixRGBA(fromRGBA, toRGBA)
		stroke[3] = clamp(stroke[3]*material.Opacity, 0, 1)
		appendSceneLine(bundle, width, height, *from, *to, sceneRGBAString(stroke), 1.8)
	}
	if result.HasBounds {
		result.ViewCulled = renderBoundsOutsideFrustum(result.Bounds, camera, width, height)
	}
	return result
}

func sceneObjectUsesLineGeometry(object sceneObject, material rootengine.RenderMaterial) bool {
	return !(sceneObjectHasTexturedSurface(object, material) && !material.Wireframe)
}

func appendSceneSurface(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, object sceneObject, materialIndex int, material rootengine.RenderMaterial, bounds rootengine.RenderBounds, timeSeconds float64) {
	if !sceneObjectHasTexturedSurface(object, material) {
		return
	}
	corners := scenePlaneSurfaceCorners(object, timeSeconds)
	if len(corners) != 4 {
		return
	}
	depthNear, depthFar, depthCenter := renderBoundsDepthMetrics(bounds, camera)
	bundle.Surfaces = append(bundle.Surfaces, rootengine.RenderSurface{
		ID:            object.ID,
		Kind:          object.Kind,
		MaterialIndex: materialIndex,
		RenderPass:    material.RenderPass,
		Static:        object.Static,
		Positions:     scenePlaneSurfacePositions(corners),
		UV:            scenePlaneSurfaceUVs(),
		VertexCount:   6,
		Bounds:        bounds,
		DepthNear:     depthNear,
		DepthFar:      depthFar,
		DepthCenter:   depthCenter,
		ViewCulled:    renderBoundsOutsideFrustum(bounds, camera, width, height),
	})
}

func sceneObjectHasTexturedSurface(object sceneObject, material rootengine.RenderMaterial) bool {
	return object.Kind == "plane" && strings.TrimSpace(material.Texture) != ""
}

func scenePlaneSurfaceCorners(object sceneObject, timeSeconds float64) []point3 {
	vertices := boxVertices(object.Width, 0, object.Depth)
	if len(vertices) < 4 {
		return nil
	}
	return []point3{
		translatePoint(vertices[0], object, timeSeconds),
		translatePoint(vertices[1], object, timeSeconds),
		translatePoint(vertices[2], object, timeSeconds),
		translatePoint(vertices[3], object, timeSeconds),
	}
}

func scenePlaneSurfacePositions(corners []point3) []float64 {
	if len(corners) < 4 {
		return nil
	}
	return []float64{
		corners[0].X, corners[0].Y, corners[0].Z,
		corners[1].X, corners[1].Y, corners[1].Z,
		corners[2].X, corners[2].Y, corners[2].Z,
		corners[0].X, corners[0].Y, corners[0].Z,
		corners[2].X, corners[2].Y, corners[2].Z,
		corners[3].X, corners[3].Y, corners[3].Z,
	}
}

func scenePlaneSurfaceUVs() []float64 {
	return []float64{
		0, 1,
		1, 1,
		1, 0,
		0, 1,
		1, 0,
		0, 0,
	}
}

func appendSceneLabel(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, label sceneLabel, timeSeconds float64) {
	world := sceneLabelPoint(label, timeSeconds)
	position := projectPoint(world, camera, width, height)
	if position == nil {
		return
	}
	marginX := math.Max(24, label.MaxWidth)
	marginY := math.Max(24, label.LineHeight*2)
	if position.X < -marginX || position.X > float64(width)+marginX || position.Y < -marginY || position.Y > float64(height)+marginY {
		return
	}
	bundle.Labels = append(bundle.Labels, rootengine.RenderLabel{
		ID:          label.ID,
		Text:        label.Text,
		ClassName:   label.ClassName,
		Position:    *position,
		Depth:       cameraLocalPoint(world, camera).Z,
		Priority:    label.Priority,
		MaxWidth:    label.MaxWidth,
		MaxLines:    label.MaxLines,
		Overflow:    label.Overflow,
		Font:        label.Font,
		LineHeight:  label.LineHeight,
		Color:       label.Color,
		Background:  label.Background,
		BorderColor: label.BorderColor,
		OffsetX:     label.OffsetX,
		OffsetY:     label.OffsetY,
		AnchorX:     label.AnchorX,
		AnchorY:     label.AnchorY,
		Collision:   label.Collision,
		Occlude:     label.Occlude,
		WhiteSpace:  label.WhiteSpace,
		TextAlign:   label.TextAlign,
	})
}

func appendSceneSprite(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, sprite sceneSprite, timeSeconds float64) {
	point := sceneSpritePoint(sprite, timeSeconds)
	position := projectPoint(point, camera, width, height)
	if position == nil {
		return
	}
	depth := cameraLocalPoint(point, camera).Z
	screenWidth, screenHeight := projectedSceneSpriteSize(camera, width, height, sprite, depth)
	if screenWidth <= 0 || screenHeight <= 0 {
		return
	}
	marginX := math.Max(24, screenWidth)
	marginY := math.Max(24, screenHeight)
	if position.X < -marginX || position.X > float64(width)+marginX || position.Y < -marginY || position.Y > float64(height)+marginY {
		return
	}
	bundle.Sprites = append(bundle.Sprites, rootengine.RenderSprite{
		ID:        sprite.ID,
		Src:       sprite.Src,
		ClassName: sprite.ClassName,
		Position:  *position,
		Depth:     depth,
		Priority:  sprite.Priority,
		Width:     screenWidth,
		Height:    screenHeight,
		Opacity:   sprite.Opacity,
		OffsetX:   sprite.OffsetX,
		OffsetY:   sprite.OffsetY,
		AnchorX:   sprite.AnchorX,
		AnchorY:   sprite.AnchorY,
		Occlude:   sprite.Occlude,
		Fit:       normalizeSceneSpriteFit(sprite.Fit),
	})
}

func sceneLabelClassName(props map[string]any) string {
	className := strings.TrimSpace(stringFromAny(propValue(props, "className"), ""))
	if className != "" {
		return className
	}
	return strings.TrimSpace(stringFromAny(propValue(props, "class"), ""))
}

func normalizeSceneLabelOverflow(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "ellipsis":
		return "ellipsis"
	default:
		return "clip"
	}
}

func appendWorldSceneLine(bundle *rootengine.RenderBundle, from, to point3, fromRGBA, toRGBA [4]float64) {
	bundle.WorldPositions = append(bundle.WorldPositions,
		from.X, from.Y, from.Z,
		to.X, to.Y, to.Z,
	)
	bundle.WorldColors = append(bundle.WorldColors,
		fromRGBA[0], fromRGBA[1], fromRGBA[2], fromRGBA[3],
		toRGBA[0], toRGBA[1], toRGBA[2], toRGBA[3],
	)
}

func sceneLabelPoint(label sceneLabel, timeSeconds float64) point3 {
	offset := sceneLabelOffset(label, timeSeconds)
	return point3{
		X: label.X + offset.X,
		Y: label.Y + offset.Y,
		Z: label.Z + offset.Z,
	}
}

func sceneLabelOffset(label sceneLabel, timeSeconds float64) point3 {
	if label.ShiftX == 0 && label.ShiftY == 0 && label.ShiftZ == 0 {
		return point3{}
	}
	angle := label.DriftPhase + timeSeconds*label.DriftSpeed
	return point3{
		X: math.Cos(angle) * label.ShiftX,
		Y: math.Sin(angle*0.82+label.DriftPhase*0.35) * label.ShiftY,
		Z: math.Sin(angle) * label.ShiftZ,
	}
}

func sceneSpritePoint(sprite sceneSprite, timeSeconds float64) point3 {
	offset := sceneSpriteOffset(sprite, timeSeconds)
	return point3{
		X: sprite.X + offset.X,
		Y: sprite.Y + offset.Y,
		Z: sprite.Z + offset.Z,
	}
}

func sceneSpriteOffset(sprite sceneSprite, timeSeconds float64) point3 {
	if sprite.ShiftX == 0 && sprite.ShiftY == 0 && sprite.ShiftZ == 0 {
		return point3{}
	}
	angle := sprite.DriftPhase + timeSeconds*sprite.DriftSpeed
	return point3{
		X: math.Cos(angle) * sprite.ShiftX,
		Y: math.Sin(angle*0.82+sprite.DriftPhase*0.35) * sprite.ShiftY,
		Z: math.Sin(angle) * sprite.ShiftZ,
	}
}

func projectedSceneSpriteSize(camera sceneCamera, width, height int, sprite sceneSprite, depth float64) (float64, float64) {
	if depth <= 0 {
		return 0, 0
	}
	focal := (math.Min(float64(width), float64(height)) / 2) / math.Tan((camera.FOV*math.Pi)/360)
	scale := sprite.Scale
	if scale <= 0 {
		scale = 1
	}
	worldWidth := sprite.Width
	if worldWidth <= 0 {
		worldWidth = 1.25
	}
	worldHeight := sprite.Height
	if worldHeight <= 0 {
		worldHeight = worldWidth
	}
	return math.Max(1, (worldWidth*scale*focal)/depth), math.Max(1, (worldHeight*scale*focal)/depth)
}

func ensureRenderMaterial(bundle *rootengine.RenderBundle, object sceneObject) int {
	profile := resolveRenderMaterial(object)
	for index, existing := range bundle.Materials {
		if renderMaterialEqual(existing, profile) {
			return index
		}
	}
	bundle.Materials = append(bundle.Materials, profile)
	return len(bundle.Materials) - 1
}

func renderMaterialEqual(left, right rootengine.RenderMaterial) bool {
	if left.Key != right.Key ||
		left.Kind != right.Kind ||
		left.Color != right.Color ||
		left.Texture != right.Texture ||
		left.Opacity != right.Opacity ||
		left.Wireframe != right.Wireframe ||
		left.BlendMode != right.BlendMode ||
		left.RenderPass != right.RenderPass ||
		left.Emissive != right.Emissive {
		return false
	}
	if len(left.ShaderData) != len(right.ShaderData) {
		return false
	}
	for i := range left.ShaderData {
		if left.ShaderData[i] != right.ShaderData[i] {
			return false
		}
	}
	return true
}

func resolveRenderMaterial(object sceneObject) rootengine.RenderMaterial {
	profile := rootengine.RenderMaterial{
		Kind:      stringFromAny(object.Material, "flat"),
		Color:     object.Color,
		Texture:   strings.TrimSpace(object.Texture),
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
	if object.HasTexture {
		profile.Texture = strings.TrimSpace(object.Texture)
	}
	if profile.Opacity < 0.999 && profile.BlendMode == "opaque" {
		profile.BlendMode = "alpha"
	}
	profile.RenderPass = renderPassFromMaterialProfile(profile)
	profile.Key = renderMaterialKey(profile)
	profile.ShaderData = renderMaterialShaderData(profile)
	return profile
}

func renderPassFromMaterialProfile(profile rootengine.RenderMaterial) string {
	switch strings.ToLower(strings.TrimSpace(profile.BlendMode)) {
	case "additive":
		return "additive"
	case "alpha":
		return "alpha"
	}
	if profile.Opacity < 0.999 {
		return "alpha"
	}
	return "opaque"
}

func renderMaterialKey(profile rootengine.RenderMaterial) string {
	// Direct Builder writes avoid the fmt.Sprintf boxing/reflection walk
	// that this runs on every material profile (one per scene element).
	kind := strings.ToLower(strings.TrimSpace(profile.Kind))
	color := strings.TrimSpace(profile.Color)
	texture := strings.TrimSpace(profile.Texture)
	blendMode := strings.ToLower(strings.TrimSpace(profile.BlendMode))
	var b strings.Builder
	b.Grow(len(kind) + len(color) + len(texture) + len(blendMode) + len(profile.RenderPass) + 40)
	b.WriteString(kind)
	b.WriteByte('|')
	b.WriteString(color)
	b.WriteByte('|')
	b.WriteString(texture)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Opacity, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatBool(profile.Wireframe))
	b.WriteByte('|')
	b.WriteString(blendMode)
	b.WriteByte('|')
	b.WriteString(profile.RenderPass)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Emissive, 'f', 3, 64))
	return b.String()
}

func renderMaterialShaderData(profile rootengine.RenderMaterial) []float64 {
	kind := strings.ToLower(strings.TrimSpace(profile.Kind))
	emissive := profile.Emissive
	switch kind {
	case "ghost":
		return []float64{1, emissive, 0.3}
	case "glass":
		return []float64{2, emissive, 0.7}
	case "glow":
		return []float64{3, emissive, 1}
	case "matte":
		return []float64{4, emissive, 0.2}
	default:
		return []float64{0, emissive, 1}
	}
}

func buildRenderPassBundles(bundle rootengine.RenderBundle) []rootengine.RenderPassBundle {
	passes := map[string]*rootengine.RenderPassBundle{
		"staticOpaque": {
			Name:      "staticOpaque",
			Blend:     "opaque",
			Depth:     "opaque",
			Static:    true,
			CacheKey:  renderStaticPassKey(bundle),
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
		"dynamicOpaque": {
			Name:      "dynamicOpaque",
			Blend:     "opaque",
			Depth:     "opaque",
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
		"alpha": {
			Name:      "alpha",
			Blend:     "alpha",
			Depth:     "translucent",
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
		"additive": {
			Name:      "additive",
			Blend:     "additive",
			Depth:     "translucent",
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
	}
	opaqueObjects := []rootengine.RenderObject{}
	alphaObjects := []rootengine.RenderObject{}
	additiveObjects := []rootengine.RenderObject{}

	for _, object := range bundle.Objects {
		switch renderPassBucketName(object) {
		case "alpha":
			alphaObjects = append(alphaObjects, object)
		case "additive":
			additiveObjects = append(additiveObjects, object)
		default:
			opaqueObjects = append(opaqueObjects, object)
		}
	}

	sort.SliceStable(alphaObjects, func(i, j int) bool {
		if alphaObjects[i].DepthCenter != alphaObjects[j].DepthCenter {
			return alphaObjects[i].DepthCenter > alphaObjects[j].DepthCenter
		}
		return alphaObjects[i].VertexOffset < alphaObjects[j].VertexOffset
	})
	sort.SliceStable(additiveObjects, func(i, j int) bool {
		if additiveObjects[i].DepthCenter != additiveObjects[j].DepthCenter {
			return additiveObjects[i].DepthCenter > additiveObjects[j].DepthCenter
		}
		return additiveObjects[i].VertexOffset < additiveObjects[j].VertexOffset
	})

	for _, object := range opaqueObjects {
		appendRenderPassObject(passes, bundle, object)
	}
	for _, object := range alphaObjects {
		appendRenderPassObject(passes, bundle, object)
	}
	for _, object := range additiveObjects {
		appendRenderPassObject(passes, bundle, object)
	}

	ordered := []rootengine.RenderPassBundle{
		finalizeRenderPassBundle(passes["staticOpaque"]),
		finalizeRenderPassBundle(passes["dynamicOpaque"]),
	}
	if passes["alpha"].VertexCount > 0 {
		ordered = append(ordered, finalizeRenderPassBundle(passes["alpha"]))
	}
	if passes["additive"].VertexCount > 0 {
		ordered = append(ordered, finalizeRenderPassBundle(passes["additive"]))
	}
	return ordered
}

func appendRenderPassObject(passes map[string]*rootengine.RenderPassBundle, bundle rootengine.RenderBundle, object rootengine.RenderObject) {
	if object.ViewCulled || object.VertexCount <= 0 {
		return
	}
	material := bundle.Materials[object.MaterialIndex]
	if !renderObjectUsesLinePass(object, material) {
		return
	}
	passName := renderPassBucketName(object)
	pass := passes[passName]
	if pass == nil {
		return
	}
	pass.Positions = appendPassSlice(pass.Positions, bundle.WorldPositions, object.VertexOffset*3, object.VertexCount*3)
	appendPassColors(pass, bundle.WorldColors, object, material)
	appendPassMaterials(pass, material, object.VertexCount)
}

func renderObjectUsesLinePass(object rootengine.RenderObject, material rootengine.RenderMaterial) bool {
	return !(object.Kind == "plane" && strings.TrimSpace(material.Texture) != "" && !material.Wireframe)
}

func appendPassSlice(target []float64, source []float64, start, length int) []float64 {
	end := start + length
	if start < 0 || end > len(source) || start > end {
		return target
	}
	return append(target, source[start:end]...)
}

func appendPassColors(pass *rootengine.RenderPassBundle, source []float64, object rootengine.RenderObject, material rootengine.RenderMaterial) {
	start := object.VertexOffset * 4
	end := start + object.VertexCount*4
	if start < 0 || end > len(source) || start > end {
		return
	}
	opacity := material.Opacity
	for i := start; i < end; i += 4 {
		pass.Colors = append(pass.Colors,
			source[i],
			source[i+1],
			source[i+2],
			source[i+3]*opacity,
		)
	}
}

func appendPassMaterials(pass *rootengine.RenderPassBundle, material rootengine.RenderMaterial, vertexCount int) {
	if len(material.ShaderData) < 3 || vertexCount <= 0 {
		return
	}
	for i := 0; i < vertexCount; i++ {
		pass.Materials = append(pass.Materials, material.ShaderData[0], material.ShaderData[1], material.ShaderData[2])
	}
}

func finalizeRenderPassBundle(pass *rootengine.RenderPassBundle) rootengine.RenderPassBundle {
	if pass == nil {
		return rootengine.RenderPassBundle{}
	}
	pass.VertexCount = len(pass.Positions) / 3
	return *pass
}

func renderPassBucketName(object rootengine.RenderObject) string {
	switch object.RenderPass {
	case "additive":
		return "additive"
	case "alpha":
		return "alpha"
	case "opaque":
		if object.Static {
			return "staticOpaque"
		}
		return "dynamicOpaque"
	default:
		if object.Static {
			return "staticOpaque"
		}
		return "dynamicOpaque"
	}
}

func renderStaticPassKey(bundle rootengine.RenderBundle) string {
	hasher := fnv.New64a()
	writeStaticPassFloat := func(value float64) {
		_, _ = hasher.Write([]byte(strconv.FormatFloat(value, 'f', 3, 64)))
		_, _ = hasher.Write([]byte{'|'})
	}
	writeStaticPassString := func(value string) {
		_, _ = hasher.Write([]byte(value))
		_, _ = hasher.Write([]byte{'|'})
	}

	writeStaticPassFloat(bundle.Camera.X)
	writeStaticPassFloat(bundle.Camera.Y)
	writeStaticPassFloat(bundle.Camera.Z)
	writeStaticPassFloat(bundle.Camera.RotationX)
	writeStaticPassFloat(bundle.Camera.RotationY)
	writeStaticPassFloat(bundle.Camera.RotationZ)
	writeStaticPassFloat(bundle.Camera.FOV)
	writeStaticPassFloat(bundle.Camera.Near)
	writeStaticPassFloat(bundle.Camera.Far)

	for _, object := range bundle.Objects {
		if object.ViewCulled || object.VertexCount <= 0 || object.RenderPass != "opaque" || !object.Static {
			continue
		}
		if object.MaterialIndex >= 0 && object.MaterialIndex < len(bundle.Materials) {
			if !renderObjectUsesLinePass(object, bundle.Materials[object.MaterialIndex]) {
				continue
			}
		}
		writeStaticPassString(object.ID)
		writeStaticPassString(object.Kind)
		writeStaticPassFloat(float64(object.MaterialIndex))
		writeStaticPassFloat(float64(object.VertexOffset))
		writeStaticPassFloat(float64(object.VertexCount))
		writeStaticPassFloat(object.DepthNear)
		writeStaticPassFloat(object.DepthFar)
		writeStaticPassFloat(object.DepthCenter)
		writeStaticPassFloat(object.Bounds.MinX)
		writeStaticPassFloat(object.Bounds.MinY)
		writeStaticPassFloat(object.Bounds.MinZ)
		writeStaticPassFloat(object.Bounds.MaxX)
		writeStaticPassFloat(object.Bounds.MaxY)
		writeStaticPassFloat(object.Bounds.MaxZ)
		if object.MaterialIndex >= 0 && object.MaterialIndex < len(bundle.Materials) {
			writeStaticPassString(bundle.Materials[object.MaterialIndex].Key)
		}
		start := object.VertexOffset * 4
		end := start + object.VertexCount*4
		if start >= 0 && end <= len(bundle.WorldColors) && start <= end {
			for _, value := range bundle.WorldColors[start:end] {
				writeStaticPassFloat(value)
			}
		}
	}
	return strconv.FormatUint(hasher.Sum64(), 16)
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

func sceneRGBAString(rgba [4]float64) string {
	r := int(math.Round(clamp(rgba[0], 0, 1) * 255))
	g := int(math.Round(clamp(rgba[1], 0, 1) * 255))
	bb := int(math.Round(clamp(rgba[2], 0, 1) * 255))
	a := clamp(rgba[3], 0, 1)
	var b strings.Builder
	b.Grow(24)
	b.WriteString("rgba(")
	b.WriteString(strconv.Itoa(r))
	b.WriteString(", ")
	b.WriteString(strconv.Itoa(g))
	b.WriteString(", ")
	b.WriteString(strconv.Itoa(bb))
	b.WriteString(", ")
	b.WriteString(strconv.FormatFloat(a, 'f', 3, 64))
	b.WriteByte(')')
	return b.String()
}

func mixRGBA(left, right [4]float64) [4]float64 {
	return [4]float64{
		(left[0] + right[0]) / 2,
		(left[1] + right[1]) / 2,
		(left[2] + right[2]) / 2,
		(left[3] + right[3]) / 2,
	}
}

func sceneLitColorRGBA(material rootengine.RenderMaterial, worldPoint, normal point3, lights []sceneLight, environment sceneEnvironment) [4]float64 {
	base := sceneColorRGBA(material.Color, [4]float64{0.55, 0.88, 1, 1})
	if !sceneLightingActive(lights, environment) {
		return base
	}

	normal = normalizePoint3(normal)
	if normal == (point3{}) {
		normal = point3{Y: 1}
	}
	baseColor := point3{X: base[0], Y: base[1], Z: base[2]}
	emissive := clamp(material.Emissive, 0, 1)
	lighting := point3{}
	if environment.AmbientIntensity > 0 {
		lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(environment.AmbientColor, point3{X: 1, Y: 1, Z: 1}), environment.AmbientIntensity)))
	}
	if environment.SkyIntensity > 0 || environment.GroundIntensity > 0 {
		hemi := clamp((normal.Y*0.5)+0.5, 0, 1)
		sky := scalePoint3(sceneColorPoint(environment.SkyColor, point3{X: 0.88, Y: 0.94, Z: 1}), environment.SkyIntensity*hemi)
		ground := scalePoint3(sceneColorPoint(environment.GroundColor, point3{X: 0.12, Y: 0.16, Z: 0.22}), environment.GroundIntensity*(1-hemi))
		lighting = addPoint3(lighting, multiplyPoint3(baseColor, addPoint3(sky, ground)))
	}
	for _, light := range lights {
		switch light.Kind {
		case "ambient":
			lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1}), light.Intensity)))
		case "directional":
			direction := normalizePoint3(point3{X: -light.DirectionX, Y: -light.DirectionY, Z: -light.DirectionZ})
			diffuse := clamp(dotPoint3(normal, direction), 0, 1)
			if diffuse > 0 {
				lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1}), light.Intensity*diffuse)))
			}
		case "point":
			offset := point3{X: light.X - worldPoint.X, Y: light.Y - worldPoint.Y, Z: light.Z - worldPoint.Z}
			distance := point3Length(offset)
			if distance == 0 {
				distance = 0.0001
			}
			diffuse := clamp(dotPoint3(normal, scalePoint3(offset, 1/distance)), 0, 1)
			if diffuse <= 0 {
				continue
			}
			attenuation := scenePointLightAttenuation(light, distance)
			if attenuation <= 0 {
				continue
			}
			lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1}), light.Intensity*diffuse*attenuation)))
		}
	}
	lit := addPoint3(scalePoint3(baseColor, emissive), scalePoint3(lighting, environment.Exposure))
	lit = addPoint3(lit, scalePoint3(baseColor, 0.06))
	return [4]float64{
		clamp(lit.X, 0, 1),
		clamp(lit.Y, 0, 1),
		clamp(lit.Z, 0, 1),
		base[3],
	}
}

func sceneLightingActive(lights []sceneLight, environment sceneEnvironment) bool {
	return len(lights) > 0 ||
		environment.AmbientIntensity > 0 ||
		environment.SkyIntensity > 0 ||
		environment.GroundIntensity > 0
}

func sceneObjectWorldNormal(object sceneObject, point point3, timeSeconds float64) point3 {
	normal := sceneObjectLocalNormal(object, point)
	return normalizePoint3(rotatePoint(normal,
		object.RotationX+object.SpinX*timeSeconds,
		object.RotationY+object.SpinY*timeSeconds,
		object.RotationZ+object.SpinZ*timeSeconds,
	))
}

func sceneObjectLocalNormal(object sceneObject, point point3) point3 {
	switch object.Kind {
	case "lines":
		width := math.Max(object.Width/2, 0.0001)
		height := math.Max(object.Height/2, 0.0001)
		depth := math.Max(object.Depth/2, 0.0001)
		ax := math.Abs(point.X / width)
		ay := math.Abs(point.Y / height)
		az := math.Abs(point.Z / depth)
		switch {
		case ax >= ay && ax >= az:
			return point3{X: math.Copysign(1, point.X)}
		case ay >= az:
			return point3{Y: math.Copysign(1, point.Y)}
		default:
			return point3{Z: math.Copysign(1, point.Z)}
		}
	case "plane":
		return point3{Y: 1}
	case "sphere":
		return normalizePoint3(point)
	case "pyramid":
		width := math.Max(object.Width/2, 0.0001)
		height := math.Max(object.Height/2, 0.0001)
		depth := math.Max(object.Depth/2, 0.0001)
		return normalizePoint3(point3{
			X: point.X / width,
			Y: (point.Y / height) + 0.35,
			Z: point.Z / depth,
		})
	default:
		width := math.Max(object.Width/2, 0.0001)
		height := math.Max(object.Height/2, 0.0001)
		depth := math.Max(object.Depth/2, 0.0001)
		ax := math.Abs(point.X / width)
		ay := math.Abs(point.Y / height)
		az := math.Abs(point.Z / depth)
		switch {
		case ax >= ay && ax >= az:
			return point3{X: math.Copysign(1, point.X)}
		case ay >= az:
			return point3{Y: math.Copysign(1, point.Y)}
		default:
			return point3{Z: math.Copysign(1, point.Z)}
		}
	}
}

func scenePointLightAttenuation(light sceneLight, distance float64) float64 {
	if light.Range > 0 {
		falloff := clamp(1-(distance/light.Range), 0, 1)
		return math.Pow(falloff, light.Decay)
	}
	return 1 / (1 + math.Pow(distance*0.35, math.Max(light.Decay, 1)))
}

func sceneColorPoint(value string, fallback point3) point3 {
	rgba := sceneColorRGBA(value, [4]float64{fallback.X, fallback.Y, fallback.Z, 1})
	return point3{X: rgba[0], Y: rgba[1], Z: rgba[2]}
}

func addPoint3(left, right point3) point3 {
	return point3{X: left.X + right.X, Y: left.Y + right.Y, Z: left.Z + right.Z}
}

func scalePoint3(point point3, scale float64) point3 {
	return point3{X: point.X * scale, Y: point.Y * scale, Z: point.Z * scale}
}

func multiplyPoint3(left, right point3) point3 {
	return point3{X: left.X * right.X, Y: left.Y * right.Y, Z: left.Z * right.Z}
}

func point3Length(point point3) float64 {
	return math.Sqrt((point.X * point.X) + (point.Y * point.Y) + (point.Z * point.Z))
}

func normalizePoint3(point point3) point3 {
	length := point3Length(point)
	if length == 0 {
		return point3{}
	}
	return scalePoint3(point, 1/length)
}

func dotPoint3(left, right point3) float64 {
	return (left.X * right.X) + (left.Y * right.Y) + (left.Z * right.Z)
}

func sceneClipPoint(point rootengine.RenderPoint, width, height int) (float64, float64) {
	return (point.X/float64(width))*2 - 1, 1 - (point.Y/float64(height))*2
}

func sceneObjectSegments(object sceneObject) [][2]point3 {
	switch object.Kind {
	case "box", "cube":
		return boxSegments(object)
	case "lines":
		return customLineSegments(object)
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

func customLineSegments(object sceneObject) [][2]point3 {
	out := make([][2]point3, 0, len(object.LineSegments))
	for _, edge := range object.LineSegments {
		if edge[0] < 0 || edge[1] < 0 || edge[0] >= len(object.Points) || edge[1] >= len(object.Points) || edge[0] == edge[1] {
			continue
		}
		out = append(out, [2]point3{object.Points[edge[0]], object.Points[edge[1]]})
	}
	return out
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
	offset := sceneMotionOffset(object, timeSeconds)
	return point3{
		X: rotated.X + object.X + offset.X,
		Y: rotated.Y + object.Y + offset.Y,
		Z: rotated.Z + object.Z + offset.Z,
	}
}

func sceneMotionOffset(object sceneObject, timeSeconds float64) point3 {
	if object.ShiftX == 0 && object.ShiftY == 0 && object.ShiftZ == 0 {
		return point3{}
	}
	angle := object.DriftPhase + timeSeconds*object.DriftSpeed
	return point3{
		X: math.Cos(angle) * object.ShiftX,
		Y: math.Sin(angle*0.82+object.DriftPhase*0.35) * object.ShiftY,
		Z: math.Sin(angle) * object.ShiftZ,
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
	local := cameraLocalPoint(point, camera)
	depth := local.Z
	if depth <= camera.Near || depth >= camera.Far {
		return nil
	}
	focal := (math.Min(float64(width), float64(height)) / 2) / math.Tan((camera.FOV*math.Pi)/360)
	return &rootengine.RenderPoint{
		X: float64(width)/2 + (local.X*focal)/depth,
		Y: float64(height)/2 - (local.Y*focal)/depth,
	}
}

func clipWorldSegmentForCamera(from, to point3, camera sceneCamera, aspect float64) (point3, point3, bool) {
	localFrom := cameraLocalPoint(from, camera)
	localTo := cameraLocalPoint(to, camera)
	depthFrom := localFrom.Z
	depthTo := localTo.Z
	if depthFrom <= camera.Near && depthTo <= camera.Near {
		return point3{}, point3{}, false
	}
	clippedFrom := from
	clippedTo := to
	if depthFrom <= camera.Near || depthTo <= camera.Near {
		t := (camera.Near - depthFrom) / math.Max(0.000001, depthTo-depthFrom)
		if depthFrom <= camera.Near {
			clippedFrom = lerpPoint3(from, to, t)
		} else {
			clippedTo = lerpPoint3(from, to, t)
		}
	}
	depthFrom = cameraLocalPoint(clippedFrom, camera).Z
	depthTo = cameraLocalPoint(clippedTo, camera).Z
	if worldSegmentOutsideFrustum(clippedFrom, depthFrom, clippedTo, depthTo, camera, aspect) {
		return point3{}, point3{}, false
	}
	return clippedFrom, clippedTo, true
}

func renderObjectBounds(worldPositions []float64, vertexOffset, vertexCount int) rootengine.RenderBounds {
	if vertexCount <= 0 {
		return rootengine.RenderBounds{}
	}
	start := vertexOffset * 3
	bounds := rootengine.RenderBounds{
		MinX: worldPositions[start],
		MinY: worldPositions[start+1],
		MinZ: worldPositions[start+2],
		MaxX: worldPositions[start],
		MaxY: worldPositions[start+1],
		MaxZ: worldPositions[start+2],
	}
	for i := start + 3; i < start+vertexCount*3; i += 3 {
		x := worldPositions[i]
		y := worldPositions[i+1]
		z := worldPositions[i+2]
		bounds.MinX = math.Min(bounds.MinX, x)
		bounds.MinY = math.Min(bounds.MinY, y)
		bounds.MinZ = math.Min(bounds.MinZ, z)
		bounds.MaxX = math.Max(bounds.MaxX, x)
		bounds.MaxY = math.Max(bounds.MaxY, y)
		bounds.MaxZ = math.Max(bounds.MaxZ, z)
	}
	return bounds
}

func renderBoundsOutsideFrustum(bounds rootengine.RenderBounds, camera sceneCamera, width, height int) bool {
	aspect := math.Max(0.0001, float64(width)/math.Max(1, float64(height)))
	corners := renderBoundsCorners(bounds)
	allLeft, allRight, allBottom, allTop := true, true, true, true
	allNear, allFar := true, true
	for _, corner := range corners {
		local := cameraLocalPoint(corner, camera)
		allNear = allNear && local.Z <= camera.Near
		allFar = allFar && local.Z >= camera.Far
		clipX, clipY := projectWorldClipPoint(corner, camera, aspect)
		allLeft = allLeft && clipX < -1
		allRight = allRight && clipX > 1
		allBottom = allBottom && clipY < -1
		allTop = allTop && clipY > 1
	}
	if allNear || allFar {
		return true
	}
	return allLeft || allRight || allBottom || allTop
}

func renderBoundsDepthMetrics(bounds rootengine.RenderBounds, camera sceneCamera) (near, far, center float64) {
	corners := renderBoundsCorners(bounds)
	if len(corners) == 0 {
		depth := cameraLocalPoint(point3{}, camera).Z
		return depth, depth, depth
	}
	near = cameraLocalPoint(corners[0], camera).Z
	far = near
	for _, corner := range corners[1:] {
		depth := cameraLocalPoint(corner, camera).Z
		near = math.Min(near, depth)
		far = math.Max(far, depth)
	}
	center = (near + far) / 2
	return near, far, center
}

func expandRenderBounds(bounds rootengine.RenderBounds, hasBounds bool, point point3) (rootengine.RenderBounds, bool) {
	if !hasBounds {
		return rootengine.RenderBounds{
			MinX: point.X,
			MinY: point.Y,
			MinZ: point.Z,
			MaxX: point.X,
			MaxY: point.Y,
			MaxZ: point.Z,
		}, true
	}
	bounds.MinX = math.Min(bounds.MinX, point.X)
	bounds.MinY = math.Min(bounds.MinY, point.Y)
	bounds.MinZ = math.Min(bounds.MinZ, point.Z)
	bounds.MaxX = math.Max(bounds.MaxX, point.X)
	bounds.MaxY = math.Max(bounds.MaxY, point.Y)
	bounds.MaxZ = math.Max(bounds.MaxZ, point.Z)
	return bounds, true
}

func renderBoundsCorners(bounds rootengine.RenderBounds) []point3 {
	return []point3{
		{X: bounds.MinX, Y: bounds.MinY, Z: bounds.MinZ},
		{X: bounds.MinX, Y: bounds.MinY, Z: bounds.MaxZ},
		{X: bounds.MinX, Y: bounds.MaxY, Z: bounds.MinZ},
		{X: bounds.MinX, Y: bounds.MaxY, Z: bounds.MaxZ},
		{X: bounds.MaxX, Y: bounds.MinY, Z: bounds.MinZ},
		{X: bounds.MaxX, Y: bounds.MinY, Z: bounds.MaxZ},
		{X: bounds.MaxX, Y: bounds.MaxY, Z: bounds.MinZ},
		{X: bounds.MaxX, Y: bounds.MaxY, Z: bounds.MaxZ},
	}
}

func projectWorldClipPoint(point point3, camera sceneCamera, aspect float64) (float64, float64) {
	local := cameraLocalPoint(point, camera)
	depth := local.Z
	focal := 1 / math.Max(0.0001, math.Tan((camera.FOV*math.Pi)/360))
	x := (local.X * focal / math.Max(depth, 0.0001)) / math.Max(aspect, 0.0001)
	y := local.Y * focal / math.Max(depth, 0.0001)
	return x, y
}

func worldSegmentOutsideFrustum(from point3, depthFrom float64, to point3, depthTo float64, camera sceneCamera, aspect float64) bool {
	if depthFrom >= camera.Far && depthTo >= camera.Far {
		return true
	}
	clipFromX, clipFromY := projectWorldClipPoint(from, camera, aspect)
	clipToX, clipToY := projectWorldClipPoint(to, camera, aspect)
	return (clipFromX < -1 && clipToX < -1) ||
		(clipFromX > 1 && clipToX > 1) ||
		(clipFromY < -1 && clipToY < -1) ||
		(clipFromY > 1 && clipToY > 1)
}

func cameraLocalPoint(point point3, camera sceneCamera) point3 {
	translated := point3{
		X: point.X - camera.X,
		Y: point.Y - camera.Y,
		Z: point.Z + camera.Z,
	}
	return inverseRotatePoint(translated, camera.RotationX, camera.RotationY, camera.RotationZ)
}

func inverseRotatePoint(point point3, rotationX, rotationY, rotationZ float64) point3 {
	x := point.X
	y := point.Y
	z := point.Z

	sinZ, cosZ := math.Sin(-rotationZ), math.Cos(-rotationZ)
	nextX := x*cosZ - y*sinZ
	nextY := x*sinZ + y*cosZ
	x, y = nextX, nextY

	sinY, cosY := math.Sin(-rotationY), math.Cos(-rotationY)
	nextX = x*cosY + z*sinY
	nextZ := -x*sinY + z*cosY
	x, z = nextX, nextZ

	sinX, cosX := math.Sin(-rotationX), math.Cos(-rotationX)
	nextY = y*cosX - z*sinX
	nextZ = y*sinX + z*cosX
	return point3{X: x, Y: nextY, Z: nextZ}
}

func lerpPoint3(from, to point3, t float64) point3 {
	t = clamp(t, 0, 1)
	return point3{
		X: from.X + (to.X-from.X)*t,
		Y: from.Y + (to.Y-from.Y)*t,
		Z: from.Z + (to.Z-from.Z)*t,
	}
}

func normalizeSceneKind(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "box", "lines", "plane", "pyramid", "sphere":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "cube"
	}
}

func fallbackLineMetric(metrics map[string]float64, key string, fallback float64) float64 {
	if len(metrics) == 0 {
		return fallback
	}
	if value, ok := metrics[key]; ok && value > 0 {
		return value
	}
	return fallback
}

func fallbackSceneDepth(kind string, props map[string]any, metrics map[string]float64, fallback float64) float64 {
	if kind == "plane" {
		if height := numberFromAny(propValue(props, "height"), 0); height != 0 {
			return height
		}
	}
	return fallbackLineMetric(metrics, "depth", fallback)
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

func scenePointList(value any) []point3 {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]point3, 0, len(list))
	for _, item := range list {
		out = append(out, scenePointValue(item))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func scenePointValue(value any) point3 {
	switch current := value.(type) {
	case map[string]any:
		return point3{
			X: numberFromAny(current["x"], 0),
			Y: numberFromAny(current["y"], 0),
			Z: numberFromAny(current["z"], 0),
		}
	case []any:
		return point3{
			X: numberFromAny(indexAny(current, 0), 0),
			Y: numberFromAny(indexAny(current, 1), 0),
			Z: numberFromAny(indexAny(current, 2), 0),
		}
	default:
		return point3{}
	}
}

func indexAny(values []any, index int) any {
	if index < 0 || index >= len(values) {
		return nil
	}
	return values[index]
}

func sceneLineSegments(value any, pointCount int) [][2]int {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([][2]int, 0, len(list))
	for _, item := range list {
		pair := sceneLineSegmentValue(item)
		if pair[0] < 0 || pair[1] < 0 || pair[0] >= pointCount || pair[1] >= pointCount || pair[0] == pair[1] {
			continue
		}
		out = append(out, pair)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sceneLineSegmentValue(value any) [2]int {
	switch current := value.(type) {
	case []any:
		return [2]int{int(numberFromAny(indexAny(current, 0), -1)), int(numberFromAny(indexAny(current, 1), -1))}
	case map[string]any:
		return [2]int{int(numberFromAny(current["from"], -1)), int(numberFromAny(current["to"], -1))}
	default:
		return [2]int{-1, -1}
	}
}

func sceneLineGeometryMetrics(points []point3) map[string]float64 {
	if len(points) == 0 {
		return nil
	}
	minX, maxX := points[0].X, points[0].X
	minY, maxY := points[0].Y, points[0].Y
	minZ, maxZ := points[0].Z, points[0].Z
	for _, point := range points[1:] {
		minX = math.Min(minX, point.X)
		maxX = math.Max(maxX, point.X)
		minY = math.Min(minY, point.Y)
		maxY = math.Max(maxY, point.Y)
		minZ = math.Min(minZ, point.Z)
		maxZ = math.Max(maxZ, point.Z)
	}
	width := math.Max(0.0001, maxX-minX)
	height := math.Max(0.0001, maxY-minY)
	depth := math.Max(0.0001, maxZ-minZ)
	return map[string]float64{
		"width":  width,
		"height": height,
		"depth":  depth,
		"radius": math.Max(width, math.Max(height, depth)) / 2,
	}
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

func sceneValue(values map[string]any, key string) any {
	scene, _ := rawValue(values, "scene").(map[string]any)
	return rawValue(scene, key)
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

func normalizeSceneLabelWhiteSpace(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pre-wrap":
		return "pre-wrap"
	case "pre":
		return "pre"
	default:
		return "normal"
	}
}

func normalizeSceneLabelAlign(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "left", "start":
		return "left"
	case "right", "end":
		return "right"
	default:
		return "center"
	}
}

func normalizeSceneLabelCollision(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow", "none", "overlap":
		return "allow"
	default:
		return "avoid"
	}
}

func normalizeSceneSpriteFit(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cover":
		return "cover"
	case "stretch", "fill":
		return "fill"
	default:
		return "contain"
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

func boolPtrFromAny(value any) *bool {
	switch typed := value.(type) {
	case bool:
		return &typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "on":
			value := true
			return &value
		case "false", "0", "no", "off":
			value := false
			return &value
		}
	}
	return nil
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
