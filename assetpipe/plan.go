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
	"sort"
	"strings"

	"github.com/odvcencio/gosx/render/bundle/ktx2"
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

// Report is the JSON-serializable contract emitted by `gosx assets plan` and
// by `gosx build` when Scene3D-grade assets are present.
type Report struct {
	SchemaVersion int      `json:"schemaVersion"`
	Roots         []string `json:"roots"`
	Assets        []Asset  `json:"assets"`
	Totals        Totals   `json:"totals"`
	Warnings      []string `json:"warnings,omitempty"`
}

// Totals summarizes a report for the build manifest.
type Totals struct {
	Assets              int   `json:"assets"`
	Bytes               int64 `json:"bytes"`
	GLB                 int   `json:"glb,omitempty"`
	GLTF                int   `json:"gltf,omitempty"`
	KTX2                int   `json:"ktx2,omitempty"`
	Texture             int   `json:"texture,omitempty"`
	Environment         int   `json:"environment,omitempty"`
	Audio               int   `json:"audio,omitempty"`
	USDZ                int   `json:"usdz,omitempty"`
	Shader              int   `json:"shader,omitempty"`
	OptimizationActions int   `json:"optimizationActions,omitempty"`
	Variants            int   `json:"variants,omitempty"`
}

// Asset describes one discovered file and the build-time work it merits.
type Asset struct {
	Path     string    `json:"path"`
	Kind     string    `json:"kind"`
	Bytes    int64     `json:"bytes"`
	MIME     string    `json:"mime,omitempty"`
	Actions  []Action  `json:"actions,omitempty"`
	Variants []Variant `json:"variants,omitempty"`
	Warnings []string  `json:"warnings,omitempty"`
	GLTF     *GLTFInfo `json:"gltf,omitempty"`
	KTX2     *KTX2Info `json:"ktx2,omitempty"`
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
	sort.Strings(report.Roots)
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
	case "environment":
		asset.Actions = environmentActions()
		asset.Variants = environmentVariants(asset.Path)
	case "audio":
		asset.Actions = audioActions()
		asset.Variants = audioVariants(asset.Path)
	case "usdz":
		asset.Actions = usdzActions()
		asset.Variants = usdzVariants(asset.Path)
	case "shader":
		asset.Actions = shaderActions()
	default:
		asset.Actions = append(asset.Actions, Action{Name: "classify-asset", Status: "candidate"})
	}
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
	case ".usdz":
		return "usdz", "model/vnd.usdz+zip", true
	case ".wgsl":
		return "shader", "text/wgsl", true
	default:
		return "", "", false
	}
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
		case "environment":
			totals.Environment++
		case "audio":
			totals.Audio++
		case "usdz":
			totals.USDZ++
		case "shader":
			totals.Shader++
		}
	}
	return totals
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
