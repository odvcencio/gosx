package inspect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/gosx/scene"
	sceneschema "github.com/odvcencio/gosx/scene/schema"
)

const Schema = "gosx.scene3d.inspect.v1"

type Options struct {
	Strict           bool
	MaxTexturePixels int
}

type SceneReport struct {
	Path       string              `json:"path"`
	Surface    SurfaceReport       `json:"surface"`
	Assets     AssetSummary        `json:"assets"`
	Memory     SceneMemoryEstimate `json:"memory"`
	FeatureUse map[string]int      `json:"featureUse"`
	Fallbacks  []FallbackReport    `json:"fallbacks,omitempty"`
	Validation sceneschema.Report  `json:"validation"`
}

type SurfaceReport struct {
	ID                   string   `json:"id"`
	BackendIntent        []string `json:"backendIntent"`
	Objects              int      `json:"objects"`
	Models               int      `json:"models"`
	Points               int      `json:"points"`
	InstancedMeshes      int      `json:"instancedMeshes"`
	InstancedGLBMeshes   int      `json:"instancedGLBMeshes"`
	ComputeParticles     int      `json:"computeParticles"`
	Labels               int      `json:"labels"`
	Sprites              int      `json:"sprites"`
	HTML                 int      `json:"html"`
	Lights               int      `json:"lights"`
	Animations           int      `json:"animations"`
	PostEffects          int      `json:"postEffects"`
	EstimatedDrawCalls   int      `json:"estimatedDrawCalls"`
	EstimatedUploadCount int      `json:"estimatedUploadCount"`
}

type AssetSummary struct {
	Sources             []string `json:"sources,omitempty"`
	Models              int      `json:"models,omitempty"`
	Textures            int      `json:"textures,omitempty"`
	EnvironmentMaps     int      `json:"environmentMaps,omitempty"`
	HTMLTextureSurfaces int      `json:"htmlTextureSurfaces,omitempty"`
	Shaders             int      `json:"shaders,omitempty"`
}

type SceneMemoryEstimate struct {
	GeometryBytes    int64 `json:"geometryBytes"`
	InstanceBytes    int64 `json:"instanceBytes"`
	PointBytes       int64 `json:"pointBytes"`
	ParticleBytes    int64 `json:"particleBytes"`
	TextureBytes     int64 `json:"textureBytes"`
	HTMLTextureBytes int64 `json:"htmlTextureBytes"`
	ShadowBytes      int64 `json:"shadowBytes"`
	PostFXBytes      int64 `json:"postFXBytes"`
	TotalGPUBytes    int64 `json:"totalGPUBytes"`
}

type FallbackReport struct {
	Feature string `json:"feature"`
	ID      string `json:"id,omitempty"`
	Reason  string `json:"reason"`
}

type SceneBudget struct {
	MaxInitialGPUBytes       int64   `json:"maxInitialGPUBytes,omitempty"`
	MaxTextureBytes          int64   `json:"maxTextureBytes,omitempty"`
	MaxHTMLTextureBytes      int64   `json:"maxHTMLTextureBytes,omitempty"`
	MaxShadowBytes           int64   `json:"maxShadowBytes,omitempty"`
	MaxPostFXBytes           int64   `json:"maxPostFXBytes,omitempty"`
	MaxFirstFrameUploads     int     `json:"maxFirstFrameUploads,omitempty"`
	MaxFirstFrameUploadBytes int64   `json:"maxFirstFrameUploadBytes,omitempty"`
	MaxDrawCalls             int     `json:"maxDrawCalls,omitempty"`
	MaxP95FrameMS            float64 `json:"maxP95FrameMS,omitempty"`
}

type BudgetStatus string

const (
	BudgetPass    BudgetStatus = "pass"
	BudgetWarn    BudgetStatus = "warn"
	BudgetFail    BudgetStatus = "fail"
	BudgetUnknown BudgetStatus = "unknown"
)

type BudgetResult struct {
	Scene    string       `json:"scene"`
	Category string       `json:"category"`
	Status   BudgetStatus `json:"status"`
	Actual   float64      `json:"actual,omitempty"`
	Limit    float64      `json:"limit,omitempty"`
	Message  string       `json:"message,omitempty"`
}

func InspectJSON(path string, data []byte, opts Options) (SceneReport, error) {
	validation := sceneschema.ValidateJSON(data, sceneschema.Options{
		Strict:           opts.Strict,
		MaxTexturePixels: opts.MaxTexturePixels,
	})
	var doc sceneschema.Document
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return SceneReport{
			Path:       path,
			FeatureUse: map[string]int{},
			Validation: validation,
		}, nil
	}
	report := InspectDocument(path, doc, validation)
	return report, nil
}

func InspectDocument(path string, doc sceneschema.Document, validation sceneschema.Report) SceneReport {
	report := SceneReport{
		Path:       path,
		Surface:    surfaceReport(path, doc),
		Assets:     AssetSummary{},
		Memory:     SceneMemoryEstimate{},
		FeatureUse: map[string]int{},
		Validation: validation,
	}
	seenAssets := map[string]bool{}
	addAsset := func(src string) {
		src = strings.TrimSpace(src)
		if src == "" || strings.HasPrefix(src, "var(") {
			return
		}
		if !seenAssets[src] {
			report.Assets.Sources = append(report.Assets.Sources, src)
			seenAssets[src] = true
		}
	}
	addFeature := func(name string) {
		if name != "" {
			report.FeatureUse[name]++
		}
	}

	for _, object := range doc.Objects {
		kind := normalizeKind(object.Kind, "object")
		addFeature("geometry." + kind)
		report.Memory.GeometryBytes += primitiveGeometryBytes(kind, object.Segments, object.RadialSegments, object.TubularSegments)
		report.Memory.TextureBytes += materialTextureBytes(object.Texture, object.NormalMap, object.RoughnessMap, object.MetalnessMap, object.EmissiveMap)
		addMaterialFeatures(addFeature, object.Texture, object.NormalMap, object.RoughnessMap, object.MetalnessMap, object.EmissiveMap)
		addAsset(object.Texture)
		addAsset(object.NormalMap)
		addAsset(object.RoughnessMap)
		addAsset(object.MetalnessMap)
		addAsset(object.EmissiveMap)
		if object.CustomVertexWGSL != "" || object.CustomFragmentWGSL != "" {
			addFeature("material.customWGSL")
			report.Assets.Shaders++
		}
		if object.CustomVertex != "" || object.CustomFragment != "" {
			addFeature("material.customGLSL")
			report.Assets.Shaders++
		}
	}
	for _, model := range doc.Models {
		report.Assets.Models++
		addAsset(model.Src)
		addFeature("geometry.model")
		if strings.EqualFold(filepath.Ext(stripQuery(model.Src)), ".glb") {
			addFeature("asset.glb")
		} else if strings.EqualFold(filepath.Ext(stripQuery(model.Src)), ".gltf") {
			addFeature("asset.gltf")
		}
	}
	for _, points := range doc.Points {
		addFeature("geometry.points")
		report.Memory.PointBytes += int64(maxInt(points.Count, positionsCount(points.Positions))) * 32
	}
	for _, mesh := range doc.InstancedMeshes {
		kind := normalizeKind(mesh.Kind, "instanced")
		addFeature("geometry.instancedMesh")
		addFeature("geometry." + kind)
		report.Memory.GeometryBytes += primitiveGeometryBytes(kind, mesh.Segments, mesh.RadialSegments, mesh.TubularSegments)
		report.Memory.InstanceBytes += int64(maxInt(mesh.Count, len(mesh.Transforms)/16)) * 64
	}
	for _, mesh := range doc.InstancedGLBMeshes {
		addFeature("geometry.instancedGLBMesh")
		report.Assets.Models++
		report.Memory.InstanceBytes += int64(len(mesh.Instances)) * 64
		addAsset(mesh.Src)
	}
	for _, particles := range doc.ComputeParticles {
		addFeature("particles.compute")
		report.Memory.ParticleBytes += int64(maxInt(particles.Count, 0)) * 48
	}
	for _, label := range doc.Labels {
		if label.ID != "" {
			addFeature("overlay.label")
		}
	}
	for _, sprite := range doc.Sprites {
		addFeature("overlay.sprite")
		report.Assets.Textures++
		report.Memory.TextureBytes += defaultTextureBytes
		addAsset(sprite.Src)
	}
	for _, html := range doc.HTML {
		mode := strings.ToLower(strings.TrimSpace(html.Mode))
		if mode == "" {
			mode = "dom"
		}
		addFeature("html." + mode)
		if mode == "texture" {
			report.Assets.HTMLTextureSurfaces++
			bytes := htmlTextureBytes(html.TextureWidth, html.TextureHeight)
			report.Memory.HTMLTextureBytes += bytes
			report.Fallbacks = append(report.Fallbacks, FallbackReport{
				Feature: "html.texture",
				ID:      html.ID,
				Reason:  "DOM fallback participates in accessibility and non-texture backends",
			})
		}
	}
	for _, light := range doc.Lights {
		addFeature("lighting." + normalizeKind(light.Kind, "light"))
		if light.CastShadow || light.ShadowSize > 0 {
			addFeature("lighting.shadows")
			size := light.ShadowSize
			if size <= 0 {
				size = 1024
			}
			report.Memory.ShadowBytes += int64(size) * int64(size) * 4
		}
	}
	if doc.ShadowMaxPixels > 0 {
		capBytes := int64(doc.ShadowMaxPixels) * 4
		if report.Memory.ShadowBytes > capBytes {
			report.Memory.ShadowBytes = capBytes
			report.Fallbacks = append(report.Fallbacks, FallbackReport{Feature: "shadows", Reason: "shadow memory estimate clamped by shadowMaxPixels"})
		}
	}
	for _, animation := range doc.Animations {
		if animation.Name != "" {
			addFeature("runtime.animation")
		}
	}
	for _, raw := range doc.PostEffects {
		kind := postEffectKind(raw)
		if kind == "" {
			kind = "unknown"
		}
		addFeature("postfx." + kind)
	}
	if len(doc.PostEffects) > 0 {
		pixels := doc.PostFXMaxPixels
		if pixels <= 0 {
			pixels = 1280 * 720
		}
		report.Memory.PostFXBytes = int64(pixels) * 16
	}

	report.Assets.Textures += materialAssetTextureCount(doc.Objects)
	sort.Strings(report.Assets.Sources)
	report.Memory.TotalGPUBytes = report.Memory.GeometryBytes + report.Memory.InstanceBytes + report.Memory.PointBytes + report.Memory.ParticleBytes + report.Memory.TextureBytes + report.Memory.HTMLTextureBytes + report.Memory.ShadowBytes + report.Memory.PostFXBytes
	report.Surface.EstimatedUploadCount = len(report.Assets.Sources) + report.Surface.Objects + report.Surface.InstancedMeshes + report.Surface.Points + report.Surface.ComputeParticles
	return report
}

func EvaluateBudget(scene SceneReport, budget SceneBudget, strict bool) []BudgetResult {
	var results []BudgetResult
	addLimit := func(category string, actual, limit float64) {
		if limit <= 0 {
			return
		}
		status := BudgetPass
		message := ""
		if actual > limit {
			status = BudgetFail
			message = "budget exceeded"
		} else if actual > limit*0.85 {
			status = BudgetWarn
			message = "near budget"
		}
		results = append(results, BudgetResult{Scene: scene.Path, Category: category, Status: status, Actual: actual, Limit: limit, Message: message})
	}
	addUnknown := func(category string, limit float64) {
		if limit <= 0 {
			return
		}
		status := BudgetUnknown
		message := "runtime measurement is not available from static SceneIR inspection"
		if strict {
			message += "; strict mode treats this as a failure"
		}
		results = append(results, BudgetResult{Scene: scene.Path, Category: category, Status: status, Limit: limit, Message: message})
	}
	addLimit("initialGPUBytes", float64(scene.Memory.TotalGPUBytes), float64(budget.MaxInitialGPUBytes))
	addLimit("textureBytes", float64(scene.Memory.TextureBytes+scene.Memory.HTMLTextureBytes), float64(budget.MaxTextureBytes))
	addLimit("htmlTextureBytes", float64(scene.Memory.HTMLTextureBytes), float64(budget.MaxHTMLTextureBytes))
	addLimit("shadowBytes", float64(scene.Memory.ShadowBytes), float64(budget.MaxShadowBytes))
	addLimit("postFXBytes", float64(scene.Memory.PostFXBytes), float64(budget.MaxPostFXBytes))
	addLimit("firstFrameUploads", float64(scene.Surface.EstimatedUploadCount), float64(budget.MaxFirstFrameUploads))
	addLimit("firstFrameUploadBytes", float64(scene.Memory.TotalGPUBytes), float64(budget.MaxFirstFrameUploadBytes))
	addLimit("drawCalls", float64(scene.Surface.EstimatedDrawCalls), float64(budget.MaxDrawCalls))
	addUnknown("p95FrameMS", budget.MaxP95FrameMS)
	return results
}

func BudgetFailed(results []BudgetResult, strict bool) bool {
	for _, result := range results {
		if result.Status == BudgetFail || (strict && result.Status == BudgetUnknown) {
			return true
		}
	}
	return false
}

func surfaceReport(path string, doc sceneschema.Document) SurfaceReport {
	drawCalls := len(doc.Objects) + len(doc.Models) + len(doc.Points) + len(doc.InstancedMeshes) + len(doc.InstancedGLBMeshes) + len(doc.ComputeParticles)
	for _, html := range doc.HTML {
		if strings.EqualFold(strings.TrimSpace(html.Mode), "texture") {
			drawCalls++
		}
	}
	drawCalls += len(doc.PostEffects)
	return SurfaceReport{
		ID:                 sceneID(path),
		BackendIntent:      []string{"webgpu", "webgl", "canvas"},
		Objects:            len(doc.Objects),
		Models:             len(doc.Models),
		Points:             len(doc.Points),
		InstancedMeshes:    len(doc.InstancedMeshes),
		InstancedGLBMeshes: len(doc.InstancedGLBMeshes),
		ComputeParticles:   len(doc.ComputeParticles),
		Labels:             len(doc.Labels),
		Sprites:            len(doc.Sprites),
		HTML:               len(doc.HTML),
		Lights:             len(doc.Lights),
		Animations:         len(doc.Animations),
		PostEffects:        len(doc.PostEffects),
		EstimatedDrawCalls: drawCalls,
	}
}

const defaultTextureBytes int64 = 4 << 20

func primitiveGeometryBytes(kind string, segments, radialSegments, tubularSegments int) int64 {
	verts := primitiveVertexCount(kind, segments, radialSegments, tubularSegments)
	return int64(verts) * 44
}

func primitiveVertexCount(kind string, segments, radialSegments, tubularSegments int) int {
	switch normalizeKind(kind, "") {
	case "cube", "cubegeometry", "box", "boxgeometry":
		return 36
	case "plane", "planegeometry", "quad", "quadgeometry":
		return 6
	case "pyramid", "pyramidgeometry":
		return 18
	case "sphere", "spheregeometry", "uvsphere", "uvspheregeometry":
		lon := positiveOrDefault(segments, 32)
		return lon * 16 * 6
	case "cylinder", "cylindergeometry":
		return positiveOrDefault(segments, 32) * 12
	case "cone", "conegeometry":
		return positiveOrDefault(segments, 32) * 6
	case "torus", "torusgeometry":
		return positiveOrDefault(radialSegments, 32) * positiveOrDefault(tubularSegments, 16) * 6
	default:
		return 36
	}
}

func materialTextureBytes(values ...string) int64 {
	return int64(nonEmptyStringCount(values...)) * defaultTextureBytes
}

func materialAssetTextureCount(objects []scene.ObjectIR) int {
	total := 0
	for _, object := range objects {
		total += nonEmptyStringCount(object.Texture, object.NormalMap, object.RoughnessMap, object.MetalnessMap, object.EmissiveMap)
	}
	return total
}

func addMaterialFeatures(add func(string), values ...string) {
	names := []string{"material.textureMap", "material.normalMap", "material.roughnessMap", "material.metalnessMap", "material.emissiveMap"}
	for i, value := range values {
		if strings.TrimSpace(value) != "" {
			add(names[i])
		}
	}
}

func htmlTextureBytes(width, height int) int64 {
	if width <= 0 || height <= 0 {
		return 0
	}
	return int64(width) * int64(height) * 4
}

func postEffectKind(raw json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	for _, key := range []string{"kind", "type"} {
		if value, ok := m[key].(string); ok {
			return normalizeKind(value, "")
		}
	}
	return ""
}

func normalizeKind(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	return strings.ToLower(value)
}

func stripQuery(src string) string {
	if i := strings.IndexAny(src, "?#"); i >= 0 {
		return src[:i]
	}
	return src
}

func sceneID(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if base == "" || base == "." {
		return "scene"
	}
	return base
}

func positiveOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func positionsCount(values []float64) int {
	if len(values) == 0 {
		return 0
	}
	return int(math.Ceil(float64(len(values)) / 3))
}

func nonEmptyStringCount(values ...string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ParseBudget(data []byte) (SceneBudget, error) {
	var wrapped struct {
		Scene3D SceneBudget `json:"scene3d"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return SceneBudget{}, err
	}
	if !budgetEmpty(wrapped.Scene3D) {
		return wrapped.Scene3D, nil
	}
	var direct SceneBudget
	if err := json.Unmarshal(data, &direct); err != nil {
		return SceneBudget{}, err
	}
	if budgetEmpty(direct) {
		return SceneBudget{}, fmt.Errorf("scene budget is empty")
	}
	return direct, nil
}

func budgetEmpty(b SceneBudget) bool {
	return b.MaxInitialGPUBytes == 0 &&
		b.MaxTextureBytes == 0 &&
		b.MaxHTMLTextureBytes == 0 &&
		b.MaxShadowBytes == 0 &&
		b.MaxPostFXBytes == 0 &&
		b.MaxFirstFrameUploads == 0 &&
		b.MaxFirstFrameUploadBytes == 0 &&
		b.MaxDrawCalls == 0 &&
		b.MaxP95FrameMS == 0
}
