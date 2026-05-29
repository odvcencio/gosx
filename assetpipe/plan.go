// Package assetpipe inventories Scene3D-ready project assets and describes the
// build-time optimization work GoSX should perform before those assets ship.
package assetpipe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"m31labs.dev/gosx/render/bundle/ktx2"
)

const (
	SchemaVersion        = 1
	DefaultMaxProbeBytes = int64(8 << 20)
)

// Options controls how much asset data Plan reads while probing. The defaults
// intentionally cap reads so build planning cannot OOM on large authored assets.
type Options struct {
	MaxProbeBytes           int64
	TurboQuantBitWidth      int
	TurboQuantPreviewBits   int
	IncludeUntrackedFormats bool
}

// SkinInfo records skinning facts for one GLB/glTF asset derived from the
// cheap structural probe. A runtime backstop (later JS task) covers GLBs the
// build never saw.
type SkinInfo struct {
	Skinned      bool `json:"skinned"`
	MorphTargets bool `json:"morphTargets,omitempty"`
}

// skinInfoFromGLTF maps a probed GLTFInfo to SkinInfo. MorphTargets is always
// false because the current gltfProbe does not inspect mesh.primitives[].targets;
// the field is reserved for a future probe pass.
func skinInfoFromGLTF(info GLTFInfo) SkinInfo {
	return SkinInfo{Skinned: info.Skins > 0}
}

// Report is the JSON-serializable contract emitted by `gosx assets plan` and
// by `gosx build` when Scene3D-grade assets are present.
type Report struct {
	SchemaVersion int          `json:"schemaVersion"`
	Roots         []string     `json:"roots"`
	Assets        []Asset      `json:"assets"`
	Totals        Totals       `json:"totals"`
	Budget        BudgetInfo   `json:"budget,omitempty"`
	Warnings      []string     `json:"warnings,omitempty"`
	Diagnostics   []Diagnostic `json:"diagnostics,omitempty"`
	// SkinManifest maps each GLB/glTF asset path (relative slash path from the
	// scan root) to its SkinInfo. Only assets with Skinned=true are included.
	SkinManifest map[string]SkinInfo `json:"skinManifest,omitempty"`
}

// Totals summarizes a report for the build manifest.
type Totals struct {
	Assets                int   `json:"assets"`
	Bytes                 int64 `json:"bytes"`
	GLB                   int   `json:"glb,omitempty"`
	GLTF                  int   `json:"gltf,omitempty"`
	KTX2                  int   `json:"ktx2,omitempty"`
	Texture               int   `json:"texture,omitempty"`
	BasisTexture          int   `json:"basisTexture,omitempty"`
	Environment           int   `json:"environment,omitempty"`
	Audio                 int   `json:"audio,omitempty"`
	Video                 int   `json:"video,omitempty"`
	DataBuffer            int   `json:"dataBuffer,omitempty"`
	USDZ                  int   `json:"usdz,omitempty"`
	Shader                int   `json:"shader,omitempty"`
	HTMLTextureManifest   int   `json:"htmlTextureManifest,omitempty"`
	OptimizationActions   int   `json:"optimizationActions,omitempty"`
	Variants              int   `json:"variants,omitempty"`
	ExpectedGPUBytes      int64 `json:"expectedGPUBytes,omitempty"`
	FirstFrameUploadBytes int64 `json:"firstFrameUploadBytes,omitempty"`
}

// Asset describes one discovered file and the build-time work it merits.
type Asset struct {
	Path        string                   `json:"path"`
	Kind        string                   `json:"kind"`
	Bytes       int64                    `json:"bytes"`
	MIME        string                   `json:"mime,omitempty"`
	Actions     []Action                 `json:"actions,omitempty"`
	Variants    []Variant                `json:"variants,omitempty"`
	Warnings    []string                 `json:"warnings,omitempty"`
	Diagnostics []Diagnostic             `json:"diagnostics,omitempty"`
	GLTF        *GLTFInfo                `json:"gltf,omitempty"`
	KTX2        *KTX2Info                `json:"ktx2,omitempty"`
	HTML        *HTMLTextureManifestInfo `json:"htmlTextureManifest,omitempty"`
	Shader      *ShaderInfo              `json:"shader,omitempty"`
	Environment *EnvironmentInfo         `json:"environment,omitempty"`
	Media       *MediaInfo               `json:"media,omitempty"`
}

// BudgetInfo summarizes expected runtime memory/upload pressure by category.
type BudgetInfo struct {
	ExpectedGPUBytes      int64 `json:"expectedGPUBytes,omitempty"`
	FirstFrameUploadBytes int64 `json:"firstFrameUploadBytes,omitempty"`
	ModelBytes            int64 `json:"modelBytes,omitempty"`
	TextureBytes          int64 `json:"textureBytes,omitempty"`
	EnvironmentBytes      int64 `json:"environmentBytes,omitempty"`
	HTMLTextureBytes      int64 `json:"htmlTextureBytes,omitempty"`
	ShaderBytes           int64 `json:"shaderBytes,omitempty"`
	MediaBytes            int64 `json:"mediaBytes,omitempty"`
}

// Diagnostic is a structured, machine-readable planner warning.
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
	Action   string `json:"action,omitempty"`
}

// Action is intentionally declarative. Follow-on build passes can execute these
// actions without changing the manifest shape users already inspect.
type Action struct {
	Name                 string   `json:"name"`
	Status               string   `json:"status"`
	Reason               string   `json:"reason,omitempty"`
	Target               string   `json:"target,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
}

// Variant describes a concrete optimized output the asset pipeline should
// eventually materialize. It is a plan, not a claim that the file already
// exists.
type Variant struct {
	URI                  string   `json:"uri"`
	Kind                 string   `json:"kind,omitempty"`
	Quality              string   `json:"quality,omitempty"`
	Compression          string   `json:"compression,omitempty"`
	SourceAction         string   `json:"sourceAction,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
}

// GLTFInfo records cheap structural facts from glTF JSON.
type GLTFInfo struct {
	ExtensionsUsed     []string `json:"extensionsUsed,omitempty"`
	ExtensionsRequired []string `json:"extensionsRequired,omitempty"`
	Meshes             int      `json:"meshes,omitempty"`
	Primitives         int      `json:"primitives,omitempty"`
	Materials          int      `json:"materials,omitempty"`
	Images             int      `json:"images,omitempty"`
	Textures           int      `json:"textures,omitempty"`
	Animations         int      `json:"animations,omitempty"`
	Skins              int      `json:"skins,omitempty"`
	Nodes              int      `json:"nodes,omitempty"`
}

// KTX2Info records upload-relevant texture metadata.
type KTX2Info struct {
	Format     int    `json:"format"`
	FormatName string `json:"formatName,omitempty"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Depth      int    `json:"depth,omitempty"`
	Layers     int    `json:"layers,omitempty"`
	Faces      int    `json:"faces,omitempty"`
	Levels     int    `json:"levels"`
	Compressed bool   `json:"compressed,omitempty"`
}

// HTMLTextureManifestInfo records cheap planning facts for texture-backed HTML.
type HTMLTextureManifestInfo struct {
	Surfaces               int   `json:"surfaces,omitempty"`
	TexturePixels          int64 `json:"texturePixels,omitempty"`
	TextureBytes           int64 `json:"textureBytes,omitempty"`
	MaxTexturePixels       int64 `json:"maxTexturePixels,omitempty"`
	DirtyRegionEntries     int   `json:"dirtyRegionEntries,omitempty"`
	AccessibilityFallbacks int   `json:"accessibilityFallbacks,omitempty"`
}

// ShaderInfo records cheap WGSL reflection hints without compiling the shader.
type ShaderInfo struct {
	Lines            int                `json:"lines,omitempty"`
	Stages           []string           `json:"stages,omitempty"`
	EntryPoints      []ShaderEntryPoint `json:"entryPoints,omitempty"`
	BindGroups       int                `json:"bindGroups,omitempty"`
	Bindings         int                `json:"bindings,omitempty"`
	HasWorkgroupSize bool               `json:"hasWorkgroupSize,omitempty"`
}

// ShaderEntryPoint describes one discovered WGSL entry function.
type ShaderEntryPoint struct {
	Name  string `json:"name"`
	Stage string `json:"stage,omitempty"`
}

// EnvironmentInfo describes the planned IBL products for HDR/EXR inputs.
type EnvironmentInfo struct {
	SourceFormat                  string `json:"sourceFormat,omitempty"`
	PlanKind                      string `json:"planKind,omitempty"`
	TargetCubeSize                int    `json:"targetCubeSize,omitempty"`
	MipLevels                     int    `json:"mipLevels,omitempty"`
	BRDFLUTSize                   int    `json:"brdfLUTSize,omitempty"`
	EstimatedGeneratedBytes       int64  `json:"estimatedGeneratedBytes,omitempty"`
	ExpectedFirstFrameUploadBytes int64  `json:"expectedFirstFrameUploadBytes,omitempty"`
}

// MediaInfo describes browser media files used by Scene3D surfaces or audio.
type MediaInfo struct {
	Kind                string `json:"kind,omitempty"`
	Container           string `json:"container,omitempty"`
	Streamable          bool   `json:"streamable,omitempty"`
	ExpectedUploadBytes int64  `json:"expectedUploadBytes,omitempty"`
}

// Plan scans roots and returns a low-memory asset optimization plan.
func Plan(roots []string, opts Options) (Report, error) {
	opts = normalizeOptions(opts)
	if len(roots) == 0 {
		roots = []string{"."}
	}
	report := Report{SchemaVersion: SchemaVersion, Assets: []Asset{}}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return Report{}, fmt.Errorf("resolve %s: %w", root, err)
		}
		report.Roots = append(report.Roots, filepath.ToSlash(absRoot))
		info, err := os.Stat(absRoot)
		if err != nil {
			return Report{}, fmt.Errorf("stat %s: %w", root, err)
		}
		if !info.IsDir() {
			asset, ok, err := planFile(absRoot, filepath.Dir(absRoot), opts)
			if err != nil {
				return Report{}, err
			}
			if ok {
				report.Assets = append(report.Assets, asset)
			}
			continue
		}
		if err := filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %v", filepath.ToSlash(path), walkErr))
				return nil
			}
			if entry.IsDir() {
				if shouldSkipDir(entry.Name()) && path != absRoot {
					return filepath.SkipDir
				}
				return nil
			}
			asset, ok, err := planFile(path, absRoot, opts)
			if err != nil {
				return err
			}
			if ok {
				report.Assets = append(report.Assets, asset)
			}
			return nil
		}); err != nil {
			return Report{}, fmt.Errorf("walk %s: %w", root, err)
		}
	}
	sort.Slice(report.Assets, func(i, j int) bool {
		return report.Assets[i].Path < report.Assets[j].Path
	})
	report.Totals = reportTotals(report.Assets)
	report.Budget = reportBudget(report.Assets)
	report.Totals.ExpectedGPUBytes = report.Budget.ExpectedGPUBytes
	report.Totals.FirstFrameUploadBytes = report.Budget.FirstFrameUploadBytes
	report.Diagnostics = reportDiagnostics(report.Warnings, report.Assets)
	sort.Strings(report.Roots)
	// Populate SkinManifest from probed GLB/glTF assets that have skins.
	for _, asset := range report.Assets {
		if asset.GLTF == nil {
			continue
		}
		si := skinInfoFromGLTF(*asset.GLTF)
		if !si.Skinned {
			continue
		}
		if report.SkinManifest == nil {
			report.SkinManifest = make(map[string]SkinInfo)
		}
		report.SkinManifest[asset.Path] = si
	}
	return report, nil
}

func normalizeOptions(opts Options) Options {
	if opts.MaxProbeBytes <= 0 {
		opts.MaxProbeBytes = DefaultMaxProbeBytes
	}
	if opts.TurboQuantBitWidth <= 0 {
		opts.TurboQuantBitWidth = 12
	}
	if opts.TurboQuantPreviewBits < 0 {
		opts.TurboQuantPreviewBits = 0
	}
	return opts
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".canopy", "node_modules", "dist", "build", "tmp":
		return true
	default:
		return false
	}
}

func planFile(path, root string, opts Options) (Asset, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Asset{}, false, fmt.Errorf("stat %s: %w", path, err)
	}
	kind, mime, ok := classify(path)
	if !ok && !opts.IncludeUntrackedFormats {
		return Asset{}, false, nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return Asset{}, false, fmt.Errorf("relative path %s: %w", path, err)
	}
	asset := Asset{
		Path:  filepath.ToSlash(rel),
		Kind:  kind,
		Bytes: info.Size(),
		MIME:  mime,
	}
	switch kind {
	case "glb":
		asset = planGLB(asset, path, opts)
	case "gltf":
		asset = planGLTF(asset, path, opts)
	case "ktx2":
		asset = planKTX2(asset, path, opts)
	case "texture":
		asset.Actions = textureActions()
		asset.Variants = textureVariants(asset.Path)
	case "basis-texture":
		asset.Actions = basisTextureActions()
		asset.Variants = textureVariants(asset.Path)
	case "environment":
		asset = planEnvironment(asset)
	case "audio":
		asset = planAudio(asset)
	case "video":
		asset = planVideo(asset)
	case "data-buffer":
		asset.Actions = dataBufferActions()
	case "usdz":
		asset.Actions = usdzActions()
		asset.Variants = usdzVariants(asset.Path)
	case "shader":
		asset = planShader(asset, path, opts)
	case "html-texture-manifest":
		asset = planHTMLTextureManifest(asset, path, opts)
	default:
		asset.Actions = append(asset.Actions, Action{Name: "classify-asset", Status: "candidate"})
	}
	asset.Diagnostics = diagnosticsFromWarnings(asset.Path, asset.Warnings)
	return asset, true, nil
}

func classify(path string) (kind string, mime string, ok bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".glb":
		return "glb", "model/gltf-binary", true
	case ".gltf":
		return "gltf", "model/gltf+json", true
	case ".ktx2":
		return "ktx2", "image/ktx2", true
	case ".basis":
		return "basis-texture", "image/vnd.basis", true
	case ".hdr":
		return "environment", "image/vnd.radiance", true
	case ".exr":
		return "environment", "image/aces", true
	case ".png":
		return "texture", "image/png", true
	case ".jpg", ".jpeg":
		return "texture", "image/jpeg", true
	case ".webp":
		return "texture", "image/webp", true
	case ".avif":
		return "texture", "image/avif", true
	case ".wav":
		return "audio", "audio/wav", true
	case ".mp3":
		return "audio", "audio/mpeg", true
	case ".ogg":
		return "audio", "audio/ogg", true
	case ".opus":
		return "audio", "audio/ogg", true
	case ".m4a":
		return "audio", "audio/mp4", true
	case ".mp4", ".m4v":
		return "video", "video/mp4", true
	case ".webm":
		return "video", "video/webm", true
	case ".mov":
		return "video", "video/quicktime", true
	case ".m3u8":
		return "video", "application/vnd.apple.mpegurl", true
	case ".bin":
		return "data-buffer", "application/octet-stream", true
	case ".usdz":
		return "usdz", "model/vnd.usdz+zip", true
	case ".wgsl":
		return "shader", "text/wgsl", true
	case ".htmltex", ".htmltexture":
		return "html-texture-manifest", "application/vnd.gosx.html-texture-manifest+json", true
	case ".json":
		if isHTMLTextureManifestPath(path) {
			return "html-texture-manifest", "application/vnd.gosx.html-texture-manifest+json", true
		}
		return "", "", false
	default:
		return "", "", false
	}
}

func isHTMLTextureManifestPath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.ReplaceAll(name, "_", "-")
	return strings.Contains(name, "html-texture") || strings.Contains(name, "htmltexture")
}

func planGLB(asset Asset, path string, opts Options) Asset {
	jsonData, err := readGLBJSON(path, opts.MaxProbeBytes)
	if err != nil {
		asset.Warnings = append(asset.Warnings, err.Error())
		asset.Actions = gltfFallbackActions(opts)
		return asset
	}
	return applyGLTFInfo(asset, jsonData, opts)
}

func planGLTF(asset Asset, path string, opts Options) Asset {
	data, err := readSmallFile(path, opts.MaxProbeBytes)
	if err != nil {
		asset.Warnings = append(asset.Warnings, err.Error())
		asset.Actions = gltfFallbackActions(opts)
		return asset
	}
	return applyGLTFInfo(asset, data, opts)
}

func applyGLTFInfo(asset Asset, jsonData []byte, opts Options) Asset {
	info, warnings := inspectGLTF(jsonData)
	asset.GLTF = &info
	asset.Warnings = append(asset.Warnings, warnings...)
	asset.Actions = gltfActions(info, opts)
	asset.Variants = gltfVariants(asset.Path, info)
	return asset
}

func gltfFallbackActions(opts Options) []Action {
	return []Action{
		{Name: "inspect-gltf", Status: "planned", Reason: "probe skipped or failed"},
		turboQuantAction(opts),
		{Name: "build-lod-stack", Status: "candidate", Reason: "large model assets should ship authored LODs"},
	}
}

func gltfActions(info GLTFInfo, opts Options) []Action {
	var actions []Action
	if info.Primitives > 0 || info.Meshes > 0 {
		actions = append(actions,
			Action{Name: "build-lod-stack", Status: "candidate", Reason: "mesh scenes need route-selectable LODs"},
			Action{Name: "meshopt-compress", Status: compressionStatus(info, "EXT_meshopt_compression"), Reason: "compact index and vertex streams"},
			Action{Name: "draco-compress", Status: compressionStatus(info, "KHR_draco_mesh_compression"), Reason: "asset-store compatibility path"},
			turboQuantAction(opts),
		)
	}
	if info.Images > 0 || info.Textures > 0 {
		actions = append(actions, Action{
			Name:                 "texture-transcode-ktx2",
			Status:               "candidate",
			Reason:               "ship BC7/ASTC/ETC2 GPU-native texture variants",
			Target:               "bc7,astc,etc2",
			RequiredCapabilities: []string{"webgpu", "webgl2"},
		})
	}
	if hasAnyExtension(info, "KHR_materials_clearcoat", "KHR_materials_sheen", "KHR_materials_iridescence", "KHR_materials_transmission", "KHR_materials_anisotropy", "KHR_materials_unlit") {
		actions = append(actions, Action{
			Name:   "preserve-pbr-extensions",
			Status: "candidate",
			Reason: "material extensions must survive import, editor round-trip, and renderer lowering",
			Target: "clearcoat,sheen,iridescence,transmission,anisotropy,unlit",
		})
	}
	if hasAnyExtension(info, "KHR_lights_punctual") {
		actions = append(actions, Action{Name: "preserve-punctual-lights", Status: "candidate", Reason: "glTF-authored lights should lower into Scene3D lights"})
	}
	if info.Animations > 0 || info.Skins > 0 {
		actions = append(actions, Action{Name: "animation-stream-quantization", Status: "candidate", Reason: "animation keyframes are high-leverage TurboQuant targets"})
	}
	return actions
}

func gltfVariants(path string, info GLTFInfo) []Variant {
	var variants []Variant
	if info.Primitives > 0 || info.Meshes > 0 {
		variants = append(variants,
			Variant{
				URI:                  siblingVariant(path, ".meshopt", ".glb"),
				Kind:                 "model",
				Compression:          "meshopt",
				SourceAction:         "meshopt-compress",
				RequiredCapabilities: []string{"meshopt"},
			},
			Variant{
				URI:          siblingVariant(path, ".draco", ".glb"),
				Kind:         "model",
				Compression:  "draco",
				SourceAction: "draco-compress",
			},
			Variant{
				URI:          siblingVariant(path, ".tq", ".glb"),
				Kind:         "model",
				Compression:  "turboquant",
				SourceAction: "turboquant-streams",
			},
			Variant{
				URI:          siblingVariant(path, ".lod0", ".glb"),
				Kind:         "model",
				Quality:      "high",
				SourceAction: "build-lod-stack",
			},
			Variant{
				URI:          siblingVariant(path, ".lod1", ".glb"),
				Kind:         "model",
				Quality:      "medium",
				SourceAction: "build-lod-stack",
			},
		)
	}
	if info.Images > 0 || info.Textures > 0 {
		variants = append(variants, modelTextureVariants(path)...)
	}
	return variants
}

func turboQuantAction(opts Options) Action {
	target := fmt.Sprintf("vertex and transform streams (%d-bit)", opts.TurboQuantBitWidth)
	if opts.TurboQuantPreviewBits > 0 {
		target += fmt.Sprintf(", preview %d-bit", opts.TurboQuantPreviewBits)
	}
	return Action{
		Name:   "turboquant-streams",
		Status: "candidate",
		Reason: "GoSX owns the SceneIR stream format and can quantize before runtime load",
		Target: target,
	}
}

func compressionStatus(info GLTFInfo, extension string) string {
	if hasAnyExtension(info, extension) {
		return "present"
	}
	return "candidate"
}

func planKTX2(asset Asset, path string, opts Options) Asset {
	data, err := readSmallFile(path, opts.MaxProbeBytes)
	if err != nil {
		asset.Warnings = append(asset.Warnings, err.Error())
		asset.Actions = append(asset.Actions, Action{Name: "inspect-ktx2", Status: "planned", Reason: "probe skipped or failed"})
		return asset
	}
	img, err := ktx2.Parse(data)
	if err != nil {
		asset.Warnings = append(asset.Warnings, fmt.Sprintf("parse ktx2: %v", err))
		asset.Actions = append(asset.Actions, Action{Name: "rebuild-ktx2", Status: "candidate", Reason: "container is not upload-ready"})
		return asset
	}
	block, _ := ktx2.FormatBlockInfo(img.Format)
	asset.KTX2 = &KTX2Info{
		Format:     img.Format,
		FormatName: ktx2FormatName(img.Format),
		Width:      img.Width,
		Height:     img.Height,
		Depth:      img.Depth,
		Layers:     img.Layers,
		Faces:      img.Faces,
		Levels:     len(img.Levels),
		Compressed: block.Compressed,
	}
	asset.Actions = append(asset.Actions, Action{Name: "webgpu-upload-ready", Status: "ready", Reason: "KTX2 parser can upload this format directly"})
	if img.Faces == 6 {
		asset.Actions = append(asset.Actions, environmentActions()...)
		asset.Variants = append(asset.Variants, environmentVariants(asset.Path)...)
	}
	return asset
}

func planHTMLTextureManifest(asset Asset, path string, opts Options) Asset {
	data, err := readSmallFile(path, opts.MaxProbeBytes)
	if err != nil {
		asset.Warnings = append(asset.Warnings, err.Error())
		asset.Actions = htmlTextureManifestActions(nil)
		return asset
	}
	info, warnings := inspectHTMLTextureManifest(data)
	asset.HTML = &info
	asset.Warnings = append(asset.Warnings, warnings...)
	asset.Actions = htmlTextureManifestActions(&info)
	return asset
}

func htmlTextureManifestActions(info *HTMLTextureManifestInfo) []Action {
	target := ""
	if info != nil && info.TextureBytes > 0 {
		target = fmt.Sprintf("%d surfaces, %d bytes", info.Surfaces, info.TextureBytes)
	}
	return []Action{
		{Name: "measure-html-textures", Status: "planned", Reason: "texture-backed HTML must be measured before raster upload", Target: target},
		{Name: "enforce-html-texture-budgets", Status: "planned", Reason: "HTML textures share the Scene3D GPU memory budget", Target: target},
		{Name: "dirty-region-upload-plan", Status: "candidate", Reason: "runtime should upload changed rectangles instead of full HTML textures"},
		{Name: "accessibility-mirror-dom", Status: "planned", Reason: "texture-backed HTML needs DOM fallback content in document order"},
	}
}

func textureActions() []Action {
	return []Action{
		{
			Name:                 "texture-transcode-ktx2",
			Status:               "candidate",
			Reason:               "replace network PNG/JPEG/WebP payloads with GPU-native texture variants",
			Target:               "bc7,astc,etc2",
			RequiredCapabilities: []string{"webgpu", "webgl2"},
		},
		{Name: "generate-mips", Status: "candidate", Reason: "stable minification and lower shimmer in 3D views"},
	}
}

func textureVariants(path string) []Variant {
	return ktx2TextureVariants(path, "")
}

func basisTextureActions() []Action {
	return []Action{
		{Name: "basis-transcode-ktx2", Status: "candidate", Reason: "Basis payloads should become explicit KTX2 GPU upload variants"},
		{Name: "generate-mips", Status: "candidate", Reason: "stable minification and lower shimmer in 3D views"},
	}
}

func modelTextureVariants(path string) []Variant {
	return ktx2TextureVariants(path, ".textures")
}

func ktx2TextureVariants(path, groupSuffix string) []Variant {
	return []Variant{
		{
			URI:                  siblingVariant(path, groupSuffix+".bc7", ".ktx2"),
			Kind:                 "texture",
			Compression:          "ktx2-bc7",
			SourceAction:         "texture-transcode-ktx2",
			RequiredCapabilities: []string{"webgpu:texture-compression-bc"},
		},
		{
			URI:                  siblingVariant(path, groupSuffix+".astc", ".ktx2"),
			Kind:                 "texture",
			Compression:          "ktx2-astc",
			SourceAction:         "texture-transcode-ktx2",
			RequiredCapabilities: []string{"webgpu:texture-compression-astc"},
		},
		{
			URI:                  siblingVariant(path, groupSuffix+".etc2", ".ktx2"),
			Kind:                 "texture",
			Compression:          "ktx2-etc2",
			SourceAction:         "texture-transcode-ktx2",
			RequiredCapabilities: []string{"webgl2"},
		},
	}
}

func environmentActions() []Action {
	return []Action{
		{Name: "prefilter-ibl-ggx", Status: "candidate", Reason: "metallic PBR surfaces need prefiltered specular environment maps"},
		{Name: "generate-split-sum-lut", Status: "candidate", Reason: "runtime PBR should sample a baked BRDF integration LUT"},
	}
}

func planEnvironment(asset Asset) Asset {
	asset.Environment = environmentInfo(asset.Path)
	asset.Actions = environmentActions()
	asset.Variants = environmentVariants(asset.Path)
	return asset
}

func environmentInfo(path string) *EnvironmentInfo {
	const targetCubeSize = 1024
	const brdfLUTSize = 256
	cubeBytes := cubeMipBytes(targetCubeSize, 8)
	lutBytes := int64(brdfLUTSize) * int64(brdfLUTSize) * 8
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	return &EnvironmentInfo{
		SourceFormat:                  format,
		PlanKind:                      "ggx-prefiltered-cubemap",
		TargetCubeSize:                targetCubeSize,
		MipLevels:                     mipLevels(targetCubeSize),
		BRDFLUTSize:                   brdfLUTSize,
		EstimatedGeneratedBytes:       cubeBytes + lutBytes,
		ExpectedFirstFrameUploadBytes: cubeBytes + lutBytes,
	}
}

func environmentVariants(path string) []Variant {
	return []Variant{
		{
			URI:          siblingVariant(path, ".ibl", ".ktx2"),
			Kind:         "environment",
			Compression:  "ktx2",
			SourceAction: "prefilter-ibl-ggx",
		},
		{
			URI:          siblingVariant(path, ".brdf-lut", ".ktx2"),
			Kind:         "environment-lut",
			Compression:  "ktx2",
			SourceAction: "generate-split-sum-lut",
		},
	}
}

func audioActions() []Action {
	return []Action{
		{Name: "audio-transcode", Status: "candidate", Reason: "ship Opus/AAC variants for browser coverage"},
		{Name: "positional-audio-node", Status: "planned", Reason: "Scene3D needs listener-bound positional audio metadata"},
	}
}

func planAudio(asset Asset) Asset {
	asset.Media = &MediaInfo{
		Kind:                "audio",
		Container:           strings.TrimPrefix(strings.ToLower(filepath.Ext(asset.Path)), "."),
		Streamable:          true,
		ExpectedUploadBytes: asset.Bytes,
	}
	asset.Actions = audioActions()
	asset.Variants = audioVariants(asset.Path)
	return asset
}

func audioVariants(path string) []Variant {
	return []Variant{
		{
			URI:          siblingVariant(path, "", ".opus"),
			Kind:         "audio",
			Compression:  "opus",
			SourceAction: "audio-transcode",
		},
		{
			URI:          siblingVariant(path, "", ".m4a"),
			Kind:         "audio",
			Compression:  "aac",
			SourceAction: "audio-transcode",
		},
	}
}

func planVideo(asset Asset) Asset {
	asset.Media = &MediaInfo{
		Kind:                "video",
		Container:           strings.TrimPrefix(strings.ToLower(filepath.Ext(asset.Path)), "."),
		Streamable:          true,
		ExpectedUploadBytes: asset.Bytes,
	}
	asset.Actions = videoActions()
	asset.Variants = videoVariants(asset.Path)
	return asset
}

func videoActions() []Action {
	return []Action{
		{Name: "video-transcode", Status: "candidate", Reason: "ship MP4/WebM/HLS variants for browser coverage"},
		{Name: "video-surface-manifest", Status: "planned", Reason: "Scene3D video surfaces need sampler, poster, and autoplay policy metadata"},
	}
}

func videoVariants(path string) []Variant {
	return []Variant{
		{
			URI:          siblingVariant(path, "", ".mp4"),
			Kind:         "video",
			Compression:  "h264-aac",
			SourceAction: "video-transcode",
		},
		{
			URI:          siblingVariant(path, "", ".webm"),
			Kind:         "video",
			Compression:  "vp9-opus",
			SourceAction: "video-transcode",
		},
		{
			URI:          siblingVariant(path, ".hls", ".m3u8"),
			Kind:         "video",
			Compression:  "hls",
			SourceAction: "video-transcode",
		},
	}
}

func dataBufferActions() []Action {
	return []Action{
		{Name: "buffer-inventory", Status: "planned", Reason: "glTF and field-data buffers should be tracked for streaming and upload budgets"},
	}
}

func usdzActions() []Action {
	return []Action{
		{Name: "usdz-import", Status: "planned", Reason: "Apple ecosystem assets should enter the same SceneIR pipeline"},
		{Name: "convert-usdz-preview", Status: "candidate", Reason: "generate glTF/SceneIR preview assets for non-Apple browsers"},
	}
}

func usdzVariants(path string) []Variant {
	return []Variant{
		{
			URI:          siblingVariant(path, ".preview", ".glb"),
			Kind:         "model",
			SourceAction: "convert-usdz-preview",
		},
	}
}

func shaderActions() []Action {
	return []Action{
		{Name: "validate-wgsl", Status: "candidate", Reason: "custom WebGPU shader hooks should fail at build time"},
		{Name: "reflect-bindings", Status: "candidate", Reason: "typed shader bindings make custom pipelines safer"},
	}
}

func planShader(asset Asset, path string, opts Options) Asset {
	data, err := readSmallFile(path, opts.MaxProbeBytes)
	if err != nil {
		asset.Warnings = append(asset.Warnings, err.Error())
		asset.Actions = shaderActions()
		return asset
	}
	info := inspectWGSL(data)
	asset.Shader = &info
	asset.Actions = shaderActions()
	if len(info.EntryPoints) > 0 {
		asset.Actions = append(asset.Actions, Action{
			Name:   "shader-entrypoint-inventory",
			Status: "ready",
			Reason: "WGSL entry points were discovered during the low-memory probe",
			Target: strings.Join(info.Stages, ","),
		})
	}
	return asset
}

func siblingVariant(path, suffix, ext string) string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	return filepath.ToSlash(base + suffix + ext)
}

var errProbeTooLarge = errors.New("asset probe exceeds max probe bytes")

func readSmallFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %s is %d bytes, max %d", errProbeTooLarge, filepath.ToSlash(path), info.Size(), maxBytes)
	}
	return os.ReadFile(path)
}

func readGLBJSON(path string, maxJSONBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var header [20]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return nil, fmt.Errorf("read glb header: %w", err)
	}
	if !bytes.Equal(header[0:4], []byte("glTF")) {
		return nil, fmt.Errorf("invalid glb magic")
	}
	if version := binary.LittleEndian.Uint32(header[4:8]); version != 2 {
		return nil, fmt.Errorf("unsupported glb version %d", version)
	}
	totalLength := binary.LittleEndian.Uint32(header[8:12])
	if stat, err := f.Stat(); err == nil && int64(totalLength) > stat.Size() {
		return nil, fmt.Errorf("glb declared length %d exceeds file size %d", totalLength, stat.Size())
	}
	jsonLen := binary.LittleEndian.Uint32(header[12:16])
	chunkType := binary.LittleEndian.Uint32(header[16:20])
	if chunkType != 0x4E4F534A {
		return nil, fmt.Errorf("first glb chunk is not JSON")
	}
	if int64(jsonLen) > maxJSONBytes {
		return nil, fmt.Errorf("%w: glb JSON chunk is %d bytes, max %d", errProbeTooLarge, jsonLen, maxJSONBytes)
	}
	data := make([]byte, jsonLen)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, fmt.Errorf("read glb JSON: %w", err)
	}
	return data, nil
}

type gltfProbe struct {
	ExtensionsUsed     []string        `json:"extensionsUsed"`
	ExtensionsRequired []string        `json:"extensionsRequired"`
	Meshes             []gltfMeshProbe `json:"meshes"`
	Materials          []struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	} `json:"materials"`
	Images     []json.RawMessage `json:"images"`
	Textures   []json.RawMessage `json:"textures"`
	Animations []json.RawMessage `json:"animations"`
	Skins      []json.RawMessage `json:"skins"`
	Nodes      []json.RawMessage `json:"nodes"`
}

type gltfMeshProbe struct {
	Primitives []struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	} `json:"primitives"`
}

func inspectGLTF(data []byte) (GLTFInfo, []string) {
	var doc gltfProbe
	if err := json.Unmarshal(data, &doc); err != nil {
		return GLTFInfo{}, []string{fmt.Sprintf("parse gltf json: %v", err)}
	}
	extensions := stringSet{}
	for _, ext := range doc.ExtensionsUsed {
		extensions.add(ext)
	}
	for _, ext := range doc.ExtensionsRequired {
		extensions.add(ext)
	}
	for _, mat := range doc.Materials {
		for ext := range mat.Extensions {
			extensions.add(ext)
		}
	}
	primitives := 0
	for _, mesh := range doc.Meshes {
		primitives += len(mesh.Primitives)
		for _, prim := range mesh.Primitives {
			for ext := range prim.Extensions {
				extensions.add(ext)
			}
		}
	}
	info := GLTFInfo{
		ExtensionsUsed:     sortedStrings(extensions),
		ExtensionsRequired: sortedTrimmed(doc.ExtensionsRequired),
		Meshes:             len(doc.Meshes),
		Primitives:         primitives,
		Materials:          len(doc.Materials),
		Images:             len(doc.Images),
		Textures:           len(doc.Textures),
		Animations:         len(doc.Animations),
		Skins:              len(doc.Skins),
		Nodes:              len(doc.Nodes),
	}
	var warnings []string
	for _, ext := range doc.ExtensionsRequired {
		if !knownGLTFExtension(ext) {
			warnings = append(warnings, fmt.Sprintf("required glTF extension %q is not in GoSX's asset-plan catalog", ext))
		}
	}
	return info, warnings
}

type htmlTextureManifestProbe struct {
	Surfaces     []htmlTextureSurfaceProbe `json:"surfaces"`
	Textures     []htmlTextureSurfaceProbe `json:"textures"`
	HTMLTextures []htmlTextureSurfaceProbe `json:"htmlTextures"`
	HTML         []htmlTextureSurfaceProbe `json:"html"`
}

type htmlTextureSurfaceProbe struct {
	Width                 int               `json:"width"`
	Height                int               `json:"height"`
	TextureWidth          int               `json:"textureWidth"`
	TextureHeight         int               `json:"textureHeight"`
	MaxTexturePixels      int               `json:"maxTexturePixels"`
	DirtyRegions          []json.RawMessage `json:"dirtyRegions"`
	AccessibilityFallback json.RawMessage   `json:"accessibilityFallback"`
	Fallback              json.RawMessage   `json:"fallback"`
}

func inspectHTMLTextureManifest(data []byte) (HTMLTextureManifestInfo, []string) {
	var doc htmlTextureManifestProbe
	if err := json.Unmarshal(data, &doc); err != nil {
		return HTMLTextureManifestInfo{}, []string{fmt.Sprintf("parse html texture manifest json: %v", err)}
	}
	entries := append([]htmlTextureSurfaceProbe{}, doc.Surfaces...)
	entries = append(entries, doc.Textures...)
	entries = append(entries, doc.HTMLTextures...)
	entries = append(entries, doc.HTML...)
	info := HTMLTextureManifestInfo{Surfaces: len(entries)}
	for _, entry := range entries {
		width := entry.TextureWidth
		if width <= 0 {
			width = entry.Width
		}
		height := entry.TextureHeight
		if height <= 0 {
			height = entry.Height
		}
		if width > 0 && height > 0 {
			pixels := int64(width) * int64(height)
			info.TexturePixels += pixels
			info.TextureBytes += pixels * 4
		}
		if entry.MaxTexturePixels > 0 {
			info.MaxTexturePixels += int64(entry.MaxTexturePixels)
		}
		info.DirtyRegionEntries += len(entry.DirtyRegions)
		if len(entry.AccessibilityFallback) > 0 || len(entry.Fallback) > 0 {
			info.AccessibilityFallbacks++
		}
	}
	var warnings []string
	if info.Surfaces == 0 {
		warnings = append(warnings, "html texture manifest contains no surfaces")
	}
	return info, warnings
}

var (
	wgslFunctionRE = regexp.MustCompile(`\bfn\s+([A-Za-z_][A-Za-z0-9_]*)`)
	wgslGroupRE    = regexp.MustCompile(`@group\(\s*([0-9]+)\s*\)`)
	wgslBindingRE  = regexp.MustCompile(`@binding\(\s*([0-9]+)\s*\)`)
)

func inspectWGSL(data []byte) ShaderInfo {
	text := string(data)
	lines := strings.Split(text, "\n")
	info := ShaderInfo{Lines: len(lines)}
	stageSet := stringSet{}
	entryNames := map[string]struct{}{}
	pendingStage := ""
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if strings.Contains(line, "@vertex") {
			pendingStage = "vertex"
		}
		if strings.Contains(line, "@fragment") {
			pendingStage = "fragment"
		}
		if strings.Contains(line, "@compute") {
			pendingStage = "compute"
		}
		if strings.Contains(line, "@workgroup_size") {
			info.HasWorkgroupSize = true
		}
		match := wgslFunctionRE.FindStringSubmatch(line)
		if len(match) == 2 {
			name := match[1]
			key := pendingStage + ":" + name
			if _, ok := entryNames[key]; !ok {
				entryNames[key] = struct{}{}
				info.EntryPoints = append(info.EntryPoints, ShaderEntryPoint{Name: name, Stage: pendingStage})
			}
			if pendingStage != "" {
				stageSet.add(pendingStage)
				pendingStage = ""
			}
		}
	}
	info.Stages = sortedStrings(stageSet)
	info.BindGroups = len(uniqueRegexMatches(wgslGroupRE, text))
	info.Bindings = len(uniqueRegexMatches(wgslBindingRE, text))
	return info
}

func uniqueRegexMatches(re *regexp.Regexp, text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		if len(match) == 2 {
			out[match[1]] = struct{}{}
		}
	}
	return out
}

type stringSet map[string]struct{}

func (s stringSet) add(value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		s[value] = struct{}{}
	}
}

func sortedStrings(set stringSet) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedTrimmed(values []string) []string {
	set := stringSet{}
	for _, value := range values {
		set.add(value)
	}
	return sortedStrings(set)
}

func hasAnyExtension(info GLTFInfo, extensions ...string) bool {
	set := make(map[string]struct{}, len(info.ExtensionsUsed)+len(info.ExtensionsRequired))
	for _, ext := range info.ExtensionsUsed {
		set[ext] = struct{}{}
	}
	for _, ext := range info.ExtensionsRequired {
		set[ext] = struct{}{}
	}
	for _, ext := range extensions {
		if _, ok := set[ext]; ok {
			return true
		}
	}
	return false
}

func knownGLTFExtension(ext string) bool {
	switch ext {
	case "KHR_materials_clearcoat",
		"KHR_materials_sheen",
		"KHR_materials_iridescence",
		"KHR_materials_transmission",
		"KHR_materials_anisotropy",
		"KHR_materials_unlit",
		"KHR_materials_emissive_strength",
		"KHR_materials_ior",
		"KHR_materials_specular",
		"KHR_materials_volume",
		"KHR_lights_punctual",
		"KHR_draco_mesh_compression",
		"KHR_mesh_quantization",
		"KHR_texture_basisu",
		"EXT_meshopt_compression":
		return true
	default:
		return false
	}
}

func reportTotals(assets []Asset) Totals {
	var totals Totals
	totals.Assets = len(assets)
	for _, asset := range assets {
		totals.Bytes += asset.Bytes
		totals.OptimizationActions += len(asset.Actions)
		totals.Variants += len(asset.Variants)
		switch asset.Kind {
		case "glb":
			totals.GLB++
		case "gltf":
			totals.GLTF++
		case "ktx2":
			totals.KTX2++
		case "texture":
			totals.Texture++
		case "basis-texture":
			totals.BasisTexture++
		case "environment":
			totals.Environment++
		case "audio":
			totals.Audio++
		case "video":
			totals.Video++
		case "data-buffer":
			totals.DataBuffer++
		case "usdz":
			totals.USDZ++
		case "shader":
			totals.Shader++
		case "html-texture-manifest":
			totals.HTMLTextureManifest++
		}
	}
	return totals
}

func reportBudget(assets []Asset) BudgetInfo {
	var budget BudgetInfo
	for _, asset := range assets {
		switch asset.Kind {
		case "glb", "gltf", "data-buffer":
			budget.ModelBytes += asset.Bytes
			budget.FirstFrameUploadBytes += asset.Bytes
		case "ktx2", "texture", "basis-texture":
			bytes := asset.Bytes
			if asset.KTX2 != nil && asset.KTX2.Width > 0 && asset.KTX2.Height > 0 && !asset.KTX2.Compressed {
				bytes = int64(asset.KTX2.Width) * int64(asset.KTX2.Height) * 4
			}
			budget.TextureBytes += bytes
			budget.FirstFrameUploadBytes += bytes
		case "environment":
			if asset.Environment != nil {
				budget.EnvironmentBytes += asset.Environment.EstimatedGeneratedBytes
				budget.FirstFrameUploadBytes += asset.Environment.ExpectedFirstFrameUploadBytes
			} else {
				budget.EnvironmentBytes += asset.Bytes
				budget.FirstFrameUploadBytes += asset.Bytes
			}
		case "html-texture-manifest":
			if asset.HTML != nil {
				budget.HTMLTextureBytes += asset.HTML.TextureBytes
				budget.FirstFrameUploadBytes += asset.HTML.TextureBytes
			}
		case "shader":
			budget.ShaderBytes += asset.Bytes
			budget.FirstFrameUploadBytes += asset.Bytes
		case "audio", "video":
			budget.MediaBytes += asset.Bytes
		}
	}
	budget.ExpectedGPUBytes = budget.ModelBytes + budget.TextureBytes + budget.EnvironmentBytes + budget.HTMLTextureBytes + budget.ShaderBytes
	return budget
}

func reportDiagnostics(reportWarnings []string, assets []Asset) []Diagnostic {
	var diagnostics []Diagnostic
	for _, warning := range reportWarnings {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: "warning",
			Code:     "asset-plan-warning",
			Message:  warning,
		})
	}
	for _, asset := range assets {
		diagnostics = append(diagnostics, asset.Diagnostics...)
	}
	return diagnostics
}

func diagnosticsFromWarnings(path string, warnings []string) []Diagnostic {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]Diagnostic, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, Diagnostic{
			Severity: "warning",
			Code:     diagnosticCodeForWarning(warning),
			Path:     path,
			Message:  warning,
			Action:   diagnosticActionForWarning(warning),
		})
	}
	return out
}

func diagnosticCodeForWarning(warning string) string {
	switch {
	case strings.Contains(warning, "html texture manifest"):
		return "html-texture-manifest"
	case strings.Contains(warning, "gltf"):
		return "gltf-probe"
	case strings.Contains(warning, "ktx2"):
		return "ktx2-probe"
	case strings.Contains(warning, errProbeTooLarge.Error()):
		return "probe-too-large"
	default:
		return "asset-warning"
	}
}

func diagnosticActionForWarning(warning string) string {
	switch diagnosticCodeForWarning(warning) {
	case "probe-too-large":
		return "increase max probe bytes or use a manifest sidecar"
	case "html-texture-manifest":
		return "fix the HTML texture manifest before relying on dirty-region uploads"
	case "gltf-probe":
		return "validate the glTF/GLB asset before build"
	case "ktx2-probe":
		return "rebuild or re-export the KTX2 texture"
	default:
		return ""
	}
}

func cubeMipBytes(size int, bytesPerPixel int64) int64 {
	var pixels int64
	for level := size; level > 0; level /= 2 {
		pixels += int64(level) * int64(level)
	}
	return pixels * 6 * bytesPerPixel
}

func mipLevels(size int) int {
	levels := 0
	for level := size; level > 0; level /= 2 {
		levels++
	}
	return levels
}

func ktx2FormatName(format int) string {
	switch format {
	case ktx2.VkFormatR8G8B8A8Unorm:
		return "R8G8B8A8_UNORM"
	case ktx2.VkFormatR8G8B8A8SRGB:
		return "R8G8B8A8_SRGB"
	case ktx2.VkFormatB8G8R8A8Unorm:
		return "B8G8R8A8_UNORM"
	case ktx2.VkFormatB8G8R8A8SRGB:
		return "B8G8R8A8_SRGB"
	case ktx2.VkFormatBC7UnormBlock:
		return "BC7_UNORM_BLOCK"
	case ktx2.VkFormatBC7SRGBBlock:
		return "BC7_SRGB_BLOCK"
	case ktx2.VkFormatETC2R8G8B8UnormBlock:
		return "ETC2_R8G8B8_UNORM_BLOCK"
	case ktx2.VkFormatETC2R8G8B8SRGBBlock:
		return "ETC2_R8G8B8_SRGB_BLOCK"
	case ktx2.VkFormatETC2R8G8B8A8UnormBlock:
		return "ETC2_R8G8B8A8_UNORM_BLOCK"
	case ktx2.VkFormatETC2R8G8B8A8SRGBBlock:
		return "ETC2_R8G8B8A8_SRGB_BLOCK"
	case ktx2.VkFormatASTC4x4UnormBlock:
		return "ASTC_4x4_UNORM_BLOCK"
	case ktx2.VkFormatASTC4x4SRGBBlock:
		return "ASTC_4x4_SRGB_BLOCK"
	case ktx2.VkFormatASTC6x6UnormBlock:
		return "ASTC_6x6_UNORM_BLOCK"
	case ktx2.VkFormatASTC6x6SRGBBlock:
		return "ASTC_6x6_SRGB_BLOCK"
	case ktx2.VkFormatASTC8x8UnormBlock:
		return "ASTC_8x8_UNORM_BLOCK"
	case ktx2.VkFormatASTC8x8SRGBBlock:
		return "ASTC_8x8_SRGB_BLOCK"
	default:
		return ""
	}
}
