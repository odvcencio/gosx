package vm

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"sync"

	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
)

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
	ID                 string
	Kind               string
	Material           string
	Size               float64
	Width              float64
	Height             float64
	Depth              float64
	Radius             float64
	Segments           int
	Points             []point3
	LineSegments       [][2]int
	X                  float64
	Y                  float64
	Z                  float64
	Color              string
	Texture            string
	RotationX          float64
	RotationY          float64
	RotationZ          float64
	SpinX              float64
	SpinY              float64
	SpinZ              float64
	ShiftX             float64
	ShiftY             float64
	ShiftZ             float64
	DriftSpeed         float64
	DriftPhase         float64
	Opacity            float64
	Wireframe          bool
	BlendMode          string
	Emissive           float64
	Roughness          float64
	Metalness          float64
	Clearcoat          float64
	Sheen              float64
	Transmission       float64
	Iridescence        float64
	Anisotropy         float64
	NormalMap          string
	RoughnessMap       string
	MetalnessMap       string
	EmissiveMap        string
	CustomVertex       string
	CustomFragment     string
	CustomVertexWGSL   string
	CustomFragmentWGSL string
	CustomUniforms     map[string]any
	ShaderBackend      string
	ShaderLayout       map[string]any
	Pickable           *bool
	HasTexture         bool
	HasOpacity         bool
	HasWireframe       bool
	HasBlendMode       bool
	HasEmissive        bool
	HasRoughness       bool
	HasMetalness       bool
	HasClearcoat       bool
	HasSheen           bool
	HasTransmission    bool
	HasIridescence     bool
	HasAnisotropy      bool
	HasNormalMap       bool
	HasRoughnessMap    bool
	HasMetalnessMap    bool
	HasEmissiveMap     bool
	Static             bool
}

// MaterialProfile registers a render-material preset for the shared engine VM.
// It lets embedders add material kinds without editing the engine VM switch.
type MaterialProfile struct {
	Opacity         float64
	HasOpacity      bool
	Wireframe       bool
	HasWireframe    bool
	BlendMode       string
	HasBlendMode    bool
	Emissive        float64
	HasEmissive     bool
	Roughness       float64
	HasRoughness    bool
	Metalness       float64
	HasMetalness    bool
	Clearcoat       float64
	HasClearcoat    bool
	Sheen           float64
	HasSheen        bool
	Transmission    float64
	HasTransmission bool
	Iridescence     float64
	HasIridescence  bool
	Anisotropy      float64
	HasAnisotropy   bool
	NormalMap       string
	HasNormalMap    bool
	RoughnessMap    string
	HasRoughnessMap bool
	MetalnessMap    string
	HasMetalnessMap bool
	EmissiveMap     string
	HasEmissiveMap  bool
	ShaderData      []float64
}

var (
	materialProfilesMu sync.RWMutex
	materialProfiles   = map[string]MaterialProfile{}
)

// RegisterMaterialProfile installs or replaces a material profile and returns a
// cleanup function that restores the previous profile for that kind.
func RegisterMaterialProfile(kind string, profile MaterialProfile) func() {
	key := materialProfileKey(kind)
	if key == "" {
		return func() {}
	}
	profile.ShaderData = cloneMaterialShaderData(profile.ShaderData)

	materialProfilesMu.Lock()
	previous, hadPrevious := materialProfiles[key]
	materialProfiles[key] = profile
	materialProfilesMu.Unlock()

	return func() {
		materialProfilesMu.Lock()
		if hadPrevious {
			materialProfiles[key] = previous
		} else {
			delete(materialProfiles, key)
		}
		materialProfilesMu.Unlock()
	}
}

func materialProfileKey(kind string) string {
	key := strings.ToLower(strings.TrimSpace(kind))
	if key == "" {
		return ""
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return ""
	}
	return key
}

func materialProfileForKind(kind string) (MaterialProfile, bool) {
	key := materialProfileKey(kind)
	if key == "" {
		return MaterialProfile{}, false
	}
	materialProfilesMu.RLock()
	profile, ok := materialProfiles[key]
	materialProfilesMu.RUnlock()
	if !ok {
		return MaterialProfile{}, false
	}
	profile.ShaderData = cloneMaterialShaderData(profile.ShaderData)
	return profile, true
}

func cloneMaterialShaderData(values []float64) []float64 {
	if len(values) < 3 {
		return nil
	}
	out := make([]float64, 3)
	copy(out, values[:3])
	return out
}

func cloneAnyMap(value any) map[string]any {
	source, ok := value.(map[string]any)
	if !ok || len(source) == 0 {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, item := range source {
		out[key] = item
	}
	return out
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
	SpinQ      motion.Quat
}

func sceneBackground(props map[string]any) string {
	if props != nil {
		if value, ok := props["background"].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "#08151f"
}

func nativeRenderPostEffects(props map[string]any) ([]rootengine.RenderPostEffect, []rootengine.RenderDiagnostic) {
	raw := scenePostEffectList(sceneValue(props, "postEffects"))
	if len(raw) == 0 {
		raw = scenePostEffectList(sceneValue(props, "postFX"))
	}
	if len(raw) == 0 {
		raw = scenePostEffectList(propValue(props, "postEffects"))
	}
	if len(raw) == 0 {
		raw = scenePostEffectList(propValue(props, "postFX"))
	}
	if len(raw) == 0 {
		return nil, nil
	}
	effects := make([]rootengine.RenderPostEffect, 0, len(raw))
	var diagnostics []rootengine.RenderDiagnostic
	for _, item := range raw {
		effect := renderPostEffectFromMap(item)
		if effect.Kind == "" {
			continue
		}
		effects = append(effects, effect)
		if !nativeRenderPostEffectSupported(effect.Kind) {
			diagnostics = append(diagnostics, rootengine.RenderDiagnostic{
				Severity: "warning",
				Code:     "native-postfx-unsupported",
				Backend:  "native",
				Target:   effect.Kind,
				Message:  "native Go render/bundle does not execute this post-FX yet; this effect remains in the render bundle as an explicit capability degradation",
			})
		}
	}
	return effects, diagnostics
}

func nativeRenderPostEffectSupported(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "bloom", "ssao", "dof", "vignette", "colorgrade", "color-grade", "tonemapping", "tone-mapping", "tonemap":
		return true
	default:
		return false
	}
}

func scenePostEffectList(value any) []map[string]any {
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

func renderPostEffectFromMap(raw map[string]any) rootengine.RenderPostEffect {
	kind := strings.TrimSpace(stringFromAny(rawValue(raw, "kind"), ""))
	if kind == "" {
		return rootengine.RenderPostEffect{}
	}
	effect := rootengine.RenderPostEffect{
		Kind:      kind,
		Mode:      strings.TrimSpace(stringFromAny(rawValue(raw, "mode"), "")),
		Threshold: numberFromAny(rawValue(raw, "threshold"), 0),
		Intensity: numberFromAny(rawValue(raw, "intensity"), numberFromAny(rawValue(raw, "strength"), 0)),
		Radius:    numberFromAny(rawValue(raw, "radius"), 0),
		Scale:     numberFromAny(rawValue(raw, "scale"), 0),
		Params:    map[string]float64{},
	}
	for _, key := range []string{
		"threshold",
		"intensity",
		"strength",
		"radius",
		"scale",
		"bias",
		"saturation",
		"contrast",
		"exposure",
		"focusDistance",
		"aperture",
		"maxBlur",
	} {
		if value := numberFromAny(rawValue(raw, key), math.NaN()); !math.IsNaN(value) {
			effect.Params[key] = value
		}
	}
	if len(effect.Params) == 0 {
		effect.Params = nil
	}
	return effect
}

func nativeRenderAnimations(props map[string]any) []rootengine.RenderAnimation {
	raw := sceneAnimationList(sceneValue(props, "animations"))
	if len(raw) == 0 {
		raw = sceneAnimationList(propValue(props, "animations"))
	}
	if len(raw) == 0 {
		return nil
	}
	animations := make([]rootengine.RenderAnimation, 0, len(raw))
	for index, item := range raw {
		animation := renderAnimationFromMap(item, index)
		if animation.Name == "" || len(animation.Channels) == 0 {
			continue
		}
		animations = append(animations, animation)
	}
	return animations
}

func sceneAnimationList(value any) []map[string]any {
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

func renderAnimationFromMap(raw map[string]any, index int) rootengine.RenderAnimation {
	name := strings.TrimSpace(stringFromAny(rawValue(raw, "name"), ""))
	if name == "" {
		name = "scene-animation-" + strconv.Itoa(index)
	}
	return rootengine.RenderAnimation{
		Name:     name,
		Duration: math.Max(0, numberFromAny(rawValue(raw, "duration"), 0)),
		Channels: renderAnimationChannels(rawValue(raw, "channels")),
	}
}

func renderAnimationChannels(value any) []rootengine.RenderAnimationChannel {
	var raw []map[string]any
	switch items := value.(type) {
	case []map[string]any:
		raw = items
	case []any:
		for _, item := range items {
			if mapped, ok := item.(map[string]any); ok {
				raw = append(raw, mapped)
			}
		}
	}
	if len(raw) == 0 {
		return nil
	}
	channels := make([]rootengine.RenderAnimationChannel, 0, len(raw))
	for _, item := range raw {
		property := strings.TrimSpace(stringFromAny(rawValue(item, "property"), ""))
		if property == "" {
			property = "translation"
		}
		channel := rootengine.RenderAnimationChannel{
			TargetID:      renderAnimationTargetID(item),
			Property:      property,
			Times:         numberSliceFromAny(rawValue(item, "times")),
			Values:        numberSliceFromAny(rawValue(item, "values")),
			Interpolation: strings.TrimSpace(stringFromAny(rawValue(item, "interpolation"), "")),
		}
		if channel.Interpolation == "" {
			channel.Interpolation = "LINEAR"
		}
		if channel.TargetID == "" || len(channel.Times) == 0 || len(channel.Values) == 0 {
			continue
		}
		channels = append(channels, channel)
	}
	return channels
}

func renderAnimationTargetID(raw map[string]any) string {
	for _, key := range []string{"targetID", "targetId", "target", "targetNode"} {
		value := rawValue(raw, key)
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return strconv.FormatInt(parsed, 10)
			}
		case int:
			return strconv.Itoa(typed)
		case int64:
			return strconv.FormatInt(typed, 10)
		case int32:
			return strconv.FormatInt(int64(typed), 10)
		case float64:
			if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
				return strconv.Itoa(int(math.Round(typed)))
			}
		case float32:
			asFloat := float64(typed)
			if !math.IsNaN(asFloat) && !math.IsInf(asFloat, 0) {
				return strconv.Itoa(int(math.Round(asFloat)))
			}
		}
	}
	return ""
}

func numberSliceFromAny(value any) []float64 {
	switch items := value.(type) {
	case []float64:
		return append([]float64(nil), items...)
	case []float32:
		out := make([]float64, 0, len(items))
		for _, item := range items {
			out = append(out, float64(item))
		}
		return out
	case []int:
		out := make([]float64, 0, len(items))
		for _, item := range items {
			out = append(out, float64(item))
		}
		return out
	case []any:
		out := make([]float64, 0, len(items))
		for _, item := range items {
			value := numberFromAny(item, math.NaN())
			if !math.IsNaN(value) && !math.IsInf(value, 0) {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
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
	rawRoughness := propValue(node.Props, "roughness")
	rawMetalness := propValue(node.Props, "metalness")
	rawClearcoat := propValue(node.Props, "clearcoat")
	rawSheen := propValue(node.Props, "sheen")
	rawTransmission := propValue(node.Props, "transmission")
	rawIridescence := propValue(node.Props, "iridescence")
	rawAnisotropy := propValue(node.Props, "anisotropy")
	rawTexture := propValue(node.Props, "texture")
	rawNormalMap := propValue(node.Props, "normalMap")
	rawRoughnessMap := propValue(node.Props, "roughnessMap")
	rawMetalnessMap := propValue(node.Props, "metalnessMap")
	rawEmissiveMap := propValue(node.Props, "emissiveMap")
	rawPickable := propValue(node.Props, "pickable")
	kind := normalizeSceneKind(stringFromAny(propValue(node.Props, "kind"), node.Geometry))
	points := scenePointList(propValue(node.Props, "points"))
	lineMetrics := sceneLineGeometryMetrics(points)
	return sceneObject{
		ID:                 stringFromAny(propValue(node.Props, "id"), "scene-object-"+strconv.Itoa(index)),
		Kind:               kind,
		Material:           stringFromAny(node.Material, "flat"),
		Size:               size,
		Width:              numberFromAny(propValue(node.Props, "width"), fallbackLineMetric(lineMetrics, "width", size)),
		Height:             numberFromAny(propValue(node.Props, "height"), fallbackLineMetric(lineMetrics, "height", size)),
		Depth:              numberFromAny(propValue(node.Props, "depth"), fallbackSceneDepth(kind, node.Props, lineMetrics, size)),
		Radius:             numberFromAny(propValue(node.Props, "radius"), fallbackLineMetric(lineMetrics, "radius", size/2)),
		Segments:           sceneSegmentResolution(propValue(node.Props, "segments")),
		Points:             points,
		LineSegments:       sceneLineSegments(propValue(node.Props, "segments"), len(points)),
		X:                  numberFromAny(propValue(node.Props, "x"), 0),
		Y:                  numberFromAny(propValue(node.Props, "y"), 0),
		Z:                  numberFromAny(propValue(node.Props, "z"), 0),
		Color:              stringFromAny(propValue(node.Props, "color"), "#8de1ff"),
		Texture:            strings.TrimSpace(stringFromAny(rawTexture, "")),
		RotationX:          numberFromAny(propValue(node.Props, "rotationX"), 0),
		RotationY:          numberFromAny(propValue(node.Props, "rotationY"), 0),
		RotationZ:          numberFromAny(propValue(node.Props, "rotationZ"), 0),
		SpinX:              numberFromAny(propValue(node.Props, "spinX"), 0),
		SpinY:              numberFromAny(propValue(node.Props, "spinY"), 0),
		SpinZ:              numberFromAny(propValue(node.Props, "spinZ"), 0),
		ShiftX:             numberFromAny(propValue(node.Props, "shiftX"), 0),
		ShiftY:             numberFromAny(propValue(node.Props, "shiftY"), 0),
		ShiftZ:             numberFromAny(propValue(node.Props, "shiftZ"), 0),
		DriftSpeed:         numberFromAny(propValue(node.Props, "driftSpeed"), 0),
		DriftPhase:         numberFromAny(propValue(node.Props, "driftPhase"), 0),
		Opacity:            clamp(numberFromAny(rawOpacity, 1), 0, 1),
		Wireframe:          boolFromAny(rawWireframe, rawTexture == nil),
		BlendMode:          normalizeBlendMode(stringFromAny(rawBlendMode, "")),
		Emissive:           clamp(numberFromAny(rawEmissive, 0), 0, 1),
		Roughness:          clamp(numberFromAny(rawRoughness, 0.55), 0, 1),
		Metalness:          clamp(numberFromAny(rawMetalness, 0), 0, 1),
		Clearcoat:          clamp(numberFromAny(rawClearcoat, 0), 0, 1),
		Sheen:              clamp(numberFromAny(rawSheen, 0), 0, 1),
		Transmission:       clamp(numberFromAny(rawTransmission, 0), 0, 1),
		Iridescence:        clamp(numberFromAny(rawIridescence, 0), 0, 1),
		Anisotropy:         clamp(numberFromAny(rawAnisotropy, 0), -1, 1),
		NormalMap:          strings.TrimSpace(stringFromAny(rawNormalMap, "")),
		RoughnessMap:       strings.TrimSpace(stringFromAny(rawRoughnessMap, "")),
		MetalnessMap:       strings.TrimSpace(stringFromAny(rawMetalnessMap, "")),
		EmissiveMap:        strings.TrimSpace(stringFromAny(rawEmissiveMap, "")),
		CustomVertex:       strings.TrimSpace(stringFromAny(propValue(node.Props, "customVertex"), "")),
		CustomFragment:     strings.TrimSpace(stringFromAny(propValue(node.Props, "customFragment"), "")),
		CustomVertexWGSL:   strings.TrimSpace(stringFromAny(propValue(node.Props, "customVertexWGSL"), "")),
		CustomFragmentWGSL: strings.TrimSpace(stringFromAny(propValue(node.Props, "customFragmentWGSL"), "")),
		CustomUniforms:     cloneAnyMap(propValue(node.Props, "customUniforms")),
		ShaderBackend:      strings.TrimSpace(stringFromAny(propValue(node.Props, "shaderBackend"), "")),
		ShaderLayout:       cloneAnyMap(propValue(node.Props, "shaderLayout")),
		Pickable:           boolPtrFromAny(rawPickable),
		HasTexture:         rawTexture != nil,
		HasOpacity:         rawOpacity != nil,
		HasWireframe:       rawWireframe != nil,
		HasBlendMode:       rawBlendMode != nil,
		HasEmissive:        rawEmissive != nil,
		HasRoughness:       rawRoughness != nil,
		HasMetalness:       rawMetalness != nil,
		HasClearcoat:       rawClearcoat != nil,
		HasSheen:           rawSheen != nil,
		HasTransmission:    rawTransmission != nil,
		HasIridescence:     rawIridescence != nil,
		HasAnisotropy:      rawAnisotropy != nil,
		HasNormalMap:       rawNormalMap != nil,
		HasRoughnessMap:    rawRoughnessMap != nil,
		HasMetalnessMap:    rawMetalnessMap != nil,
		HasEmissiveMap:     rawEmissiveMap != nil,
		Static:             node.Static,
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
