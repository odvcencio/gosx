package inspect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/geom"
	sceneschema "m31labs.dev/gosx/scene/schema"
)

const Schema = "gosx.scene3d.inspect.v1"

type Options struct {
	Strict           bool
	MaxTexturePixels int
	// AssetRoots are directories that scene asset sources resolve against. When
	// the list is empty, inspect does not check reachability and the report says
	// so, so a report that did not look never reads as a report that found
	// everything.
	AssetRoots []string
}

type SceneReport struct {
	Path       string              `json:"path"`
	Surface    SurfaceReport       `json:"surface"`
	Assets     AssetSummary        `json:"assets"`
	Memory     SceneMemoryEstimate `json:"memory"`
	FeatureUse map[string]int      `json:"featureUse"`
	Fallbacks  []FallbackReport    `json:"fallbacks,omitempty"`
	// AssetResolution reports which asset sources were found on disk. It is nil
	// only when the caller used the deprecated no-options entry points.
	AssetResolution *AssetResolutionReport `json:"assetResolution,omitempty"`
	Validation      sceneschema.Report     `json:"validation"`
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
	report := InspectDocumentWithOptions(path, doc, validation, opts)
	return report, nil
}

// InspectDocument inspects a decoded document without checking asset
// reachability. Call InspectDocumentWithOptions to resolve assets against real
// directories.
func InspectDocument(path string, doc sceneschema.Document, validation sceneschema.Report) SceneReport {
	return InspectDocumentWithOptions(path, doc, validation, Options{})
}

func InspectDocumentWithOptions(path string, doc sceneschema.Document, validation sceneschema.Report, opts Options) SceneReport {
	report := SceneReport{
		Path:       path,
		Surface:    surfaceReport(path, doc),
		Assets:     AssetSummary{},
		Memory:     SceneMemoryEstimate{},
		FeatureUse: map[string]int{},
		Validation: validation,
	}
	seenAssets := map[string]bool{}
	references := newAssetReferenceIndex()
	addAsset := func(src, id, docPath string) {
		src = strings.TrimSpace(src)
		if src == "" || strings.HasPrefix(src, "var(") {
			return
		}
		references.add(src, id, docPath)
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

	for index, object := range doc.Objects {
		docPath := fmt.Sprintf("objects[%d]", index)
		kind := normalizeKind(object.Kind, "object")
		addFeature("geometry." + kind)
		report.Memory.GeometryBytes += objectGeometryBytes(object)
		report.Memory.TextureBytes += materialTextureBytes(object.Texture, object.NormalMap, object.RoughnessMap, object.MetalnessMap, object.EmissiveMap)
		addMaterialFeatures(addFeature, object.Texture, object.NormalMap, object.RoughnessMap, object.MetalnessMap, object.EmissiveMap)
		addAsset(object.Texture, object.ID, docPath+".texture")
		addAsset(object.NormalMap, object.ID, docPath+".normalMap")
		addAsset(object.RoughnessMap, object.ID, docPath+".roughnessMap")
		addAsset(object.MetalnessMap, object.ID, docPath+".metalnessMap")
		addAsset(object.EmissiveMap, object.ID, docPath+".emissiveMap")
		if object.CustomVertexWGSL != "" || object.CustomFragmentWGSL != "" {
			addFeature("material.customWGSL")
			report.Assets.Shaders++
		}
		if object.CustomVertex != "" || object.CustomFragment != "" {
			addFeature("material.customGLSL")
			report.Assets.Shaders++
		}
	}
	for index, model := range doc.Models {
		report.Assets.Models++
		addAsset(model.Src, model.ID, fmt.Sprintf("models[%d].src", index))
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
	for index, mesh := range doc.InstancedGLBMeshes {
		addFeature("geometry.instancedGLBMesh")
		report.Assets.Models++
		report.Memory.InstanceBytes += int64(len(mesh.Instances)) * 64
		addAsset(mesh.Src, mesh.ID, fmt.Sprintf("instancedGLBMeshes[%d].src", index))
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
	for index, sprite := range doc.Sprites {
		addFeature("overlay.sprite")
		report.Assets.Textures++
		report.Memory.TextureBytes += defaultTextureBytes
		addAsset(sprite.Src, sprite.ID, fmt.Sprintf("sprites[%d].src", index))
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

	resolution := resolveAssets(references, opts.AssetRoots)
	report.AssetResolution = &resolution
	appendUnresolvedAssetDiagnostics(&report.Validation, resolution)
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
		BackendIntent:      backendIntent(doc),
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

// backendIntent reports which rendering backends the scene can faithfully
// target. When the document carries the Go-computed honesty-gate verdict
// (backendCaps), the surface report mirrors it; otherwise it falls back to the
// legacy assumption that every backend is viable.
func backendIntent(doc sceneschema.Document) []string {
	if doc.BackendCaps != nil && len(doc.BackendCaps.Capable) > 0 {
		out := make([]string, 0, len(doc.BackendCaps.Capable))
		for _, b := range doc.BackendCaps.Capable {
			out = append(out, string(b))
		}
		return out
	}
	return []string{"webgpu", "webgl", "canvas"}
}

const defaultTextureBytes int64 = 4 << 20

// bytesPerVertex is the size of one uploaded vertex: three floats of position,
// three of normal, two of texture coordinate, three of color, and one packed
// tangent word.
const bytesPerVertex = 44

// objectGeometryBytes estimates the GPU bytes one scene object uploads.
//
// An object that carries inline vertices is a generated mesh: a polyhedron, a
// disc, a shape, an extrusion, or an imported glTF mesh. Its real size is its own
// vertex count, so read that first.
//
// Reading the kind alone used to send every such object to the default branch,
// which reports 36 vertices. A hundred-thousand-triangle mesh was therefore
// reported as one cube. The estimate has to read the vertices or it understates
// the whole family by orders of magnitude.
func objectGeometryBytes(object scene.ObjectIR) int64 {
	if count := inlineVertexCount(object); count > 0 {
		return int64(count) * bytesPerVertex
	}
	return primitiveGeometryBytes(object.Kind, object.Segments, object.RadialSegments, object.TubularSegments)
}

// inlineVertexCount returns the vertex count an object ships inline, or zero when
// it ships none.
func inlineVertexCount(object scene.ObjectIR) int {
	vertices := object.Vertices
	if vertices == nil {
		return 0
	}
	if vertices.Count > 0 {
		return vertices.Count
	}
	return len(vertices.Positions) / 3
}

func primitiveGeometryBytes(kind string, segments, radialSegments, tubularSegments int) int64 {
	verts := primitiveVertexCount(kind, segments, radialSegments, tubularSegments)
	return int64(verts) * bytesPerVertex
}

// primitiveVertexCount returns how many vertices one named primitive uploads.
//
// The nine parametric kinds delegate to package scene/geom, the single generator
// the browser wire path, the native renderer and the picker all read. Delegating
// removes a whole class of defect: a private copy of the formula here reported
// 1152 vertices for a 12-segment sphere while the generator built 432, because
// the copy hardcoded 16 latitude rows instead of deriving them from the segment
// count. The report was only right at the default resolution.
//
// The generated families keep their own formulas below, because they lower to a
// "gltf-mesh" object rather than a named kind. objectGeometryBytes reads their
// real vertex list first, so these formulas only matter if a future lowering
// names one of them as a kind.
func primitiveVertexCount(kind string, segments, radialSegments, tubularSegments int) int {
	normalized := normalizeKind(kind, "")
	if canonical := geom.NormalizeKind(normalized); canonical != "" {
		return geom.DrawVertexCount(geom.Params{
			Kind:            canonical,
			Segments:        segments,
			RadialSegments:  radialSegments,
			TubularSegments: tubularSegments,
		})
	}
	switch normalized {
	case "tetrahedron", "tetrahedrongeometry":
		return polyhedronVertexCount(4, segments)
	case "octahedron", "octahedrongeometry":
		return polyhedronVertexCount(8, segments)
	case "icosahedron", "icosahedrongeometry":
		return polyhedronVertexCount(20, segments)
	case "dodecahedron", "dodecahedrongeometry":
		// Twelve pentagon faces ship as three triangles each.
		return polyhedronVertexCount(36, segments)
	case "circle", "circlegeometry":
		// A fan of one triangle per rim step, expanded to a flat list.
		return positiveOrDefault(segments, 32) * 3
	case "ring", "ringgeometry":
		// Two triangles per step of each band. radialSegments carries the band
		// count, which RingGeometry names phiSegments.
		return positiveOrDefault(segments, 32) * positiveOrDefault(radialSegments, 1) * 6
	default:
		return 36
	}
}

// polyhedronVertexCount returns the expanded vertex count of a subdivided
// polyhedron. Every subdivision step splits each edge, so a face becomes
// (detail+1)^2 triangles. The detail is capped at 5, as the generator caps it.
func polyhedronVertexCount(faces, detail int) int {
	if detail < 0 {
		detail = 0
	}
	if detail > 5 {
		detail = 5
	}
	cols := detail + 1
	return faces * cols * cols * 3
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
