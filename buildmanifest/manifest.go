package buildmanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manifest describes all build outputs for deployment.
type Manifest struct {
	Runtime     RuntimeAssets       `json:"runtime"`
	Islands     []IslandAsset       `json:"islands"`
	CSS         []CSSAsset          `json:"css"`
	SceneAssets *SceneAssetManifest `json:"sceneAssets,omitempty"`
	// Images holds the build-time responsive/format variants gosx build's
	// imagepipe stage generated for every source image under a project's
	// public/ directory (issue #200). It is additive: a manifest written
	// before this field existed decodes with Images == nil, and every
	// method that reads it (ImageAssetBySource, ImageVariant) treats a nil
	// or empty slice as "no build-time variants recorded" rather than an
	// error.
	Images []ImageAsset `json:"images,omitempty"`

	// SourceRoot is the absolute project root `gosx build` ran from when it
	// wrote this manifest (the directory holding `dist/`, `app/`, and
	// `go.mod`). A server started later prefers this recorded path — if it
	// still exists on the host — over guessing the project root from where
	// it found build.json, since a guess breaks whenever the manifest is
	// not nested one level under a directory literally named "dist". See
	// StaleIslands.
	SourceRoot string `json:"sourceRoot,omitempty"`
}

type RuntimeAssets struct {
	WASM        HashedAsset `json:"wasm"`
	WASMIslands HashedAsset `json:"wasmIslands,omitempty"`
	// WASMVariants contains the capability-linked artifacts. WASM remains the
	// full-runtime compatibility field so older servers and source builds keep
	// working while new renderers select the smallest compatible entry.
	WASMVariants                      map[string]RuntimeVariantAsset `json:"wasmVariants,omitempty"`
	WASMExec                          HashedAsset                    `json:"wasmExec"`
	StandardGoWASMExec                HashedAsset                    `json:"standardGoWasmExec,omitempty"`
	Bootstrap                         HashedAsset                    `json:"bootstrap"`
	BootstrapLite                     HashedAsset                    `json:"bootstrapLite,omitempty"`
	BootstrapRuntime                  HashedAsset                    `json:"bootstrapRuntime,omitempty"`
	BootstrapFeatureIslands           HashedAsset                    `json:"bootstrapFeatureIslands,omitempty"`
	BootstrapFeatureEngines           HashedAsset                    `json:"bootstrapFeatureEngines,omitempty"`
	BootstrapFeatureHubs              HashedAsset                    `json:"bootstrapFeatureHubs,omitempty"`
	BootstrapFeatureControllers       HashedAsset                    `json:"bootstrapFeatureControllers,omitempty"`
	BootstrapFeatureTextlayout        HashedAsset                    `json:"bootstrapFeatureTextlayout,omitempty"`
	BootstrapFeatureScene3D           HashedAsset                    `json:"bootstrapFeatureScene3d,omitempty"`
	BootstrapFeatureScene3DCommand    HashedAsset                    `json:"bootstrapFeatureScene3dCommand,omitempty"`
	BootstrapFeatureScene3DWebGPU     HashedAsset                    `json:"bootstrapFeatureScene3dWebgpu,omitempty"`
	BootstrapFeatureScene3DWebGL      HashedAsset                    `json:"bootstrapFeatureScene3dWebgl,omitempty"`
	BootstrapFeatureScene3DGLTF       HashedAsset                    `json:"bootstrapFeatureScene3dGltf,omitempty"`
	BootstrapFeatureScene3DAnimation  HashedAsset                    `json:"bootstrapFeatureScene3dAnimation,omitempty"`
	BootstrapFeatureScene3DCompute    HashedAsset                    `json:"bootstrapFeatureScene3dCompute,omitempty"`
	BootstrapFeatureScene3DDecompress HashedAsset                    `json:"bootstrapFeatureScene3dDecompress,omitempty"`
	Patch                             HashedAsset                    `json:"patch"`
	VideoHLS                          HashedAsset                    `json:"videoHLS,omitempty"`
	StripeBridge                      HashedAsset                    `json:"stripeBridge,omitempty"`
	Relay                             HashedAsset                    `json:"relay,omitempty"`
}

// RuntimeVariantAsset identifies one runtime artifact independently of its
// compatibility filename. FeatureMask is the ABI capability declaration the
// browser must validate after loading the artifact.
type RuntimeVariantAsset struct {
	HashedAsset
	Variant      string `json:"variant"`
	FeatureMask  uint32 `json:"featureMask"`
	ManifestHash string `json:"manifestHash"`
}

type IslandAsset struct {
	Name   string `json:"name"`
	Format string `json:"format"` // "json" or "bin"
	HashedAsset

	// SourceFile is the project-relative, slash-separated path to the .gsx
	// file that declared this island. Empty on manifests written before
	// issue #166 added source staleness detection.
	SourceFile string `json:"sourceFile,omitempty"`
	// SourceHash is ContentHash of SourceFile's raw bytes at the moment
	// `gosx build` wrote this asset. It is deliberately NOT the same value
	// as HashedAsset.Hash: Hash covers the compiled program bytes (the
	// JSON/binary IR shipped to the browser), while SourceHash covers the
	// .gsx source text the compiler read to produce that program. A server
	// started later re-hashes SourceFile and compares against SourceHash to
	// detect that source changed since the dist/ program was built, without
	// re-running the compiler pipeline. Empty when the manifest predates
	// this field, or when the build could not resolve a source file for
	// the island (for example, an island a Go-only test synthesizes).
	SourceHash string `json:"sourceHash,omitempty"`
}

type CSSAsset struct {
	Component string `json:"component"`
	Source    string `json:"source,omitempty"`
	HashedAsset
}

// ImageAsset describes the build-time responsive variants gosx build's
// imagepipe stage generated for one source image.
type ImageAsset struct {
	// Source is the root-relative, slash-separated public URL of the
	// original image gosx build probed and resized -- for example
	// "/photo.jpg" for a file at <project>/public/photo.jpg. It is the
	// same string server.AssetURL(props.Src) produces for that image, so a
	// runtime lookup by request src never needs path translation.
	Source string `json:"source"`
	// Width and Height are the source image's intrinsic pixel dimensions,
	// as image.DecodeConfig reported them before any resize.
	Width int `json:"width"`
	// Height is the source's intrinsic pixel height. It is never
	// propagated onto individual ImageVariantAsset entries below -- a
	// variant's own aspect-correct height is implied by its Width and this
	// intrinsic Width/Height ratio, not recorded as a separate srcset
	// dimension (issue #200; compare the height/aspect-ratio bug in
	// server/image.go's own srcset entries, issue #199).
	Height int `json:"height"`
	// Variants holds one entry per (width, format) rung gosx build
	// generated for this source. Every Variant.Width is <= Width above --
	// the ladder that produced these rungs (imagepipe.Ladder) never
	// upscales.
	Variants []ImageVariantAsset `json:"variants,omitempty"`
}

// ImageVariantAsset is one hashed, build-time-generated output image: a
// single (width, format) rung of an ImageAsset's responsive ladder.
type ImageVariantAsset struct {
	Width int `json:"width"`
	// Format is imagepipe.Format's string value: "jpeg" or "png" for a
	// stock `gosx build` (its only built-in encoders), or any other value
	// (for example "webp") a project's own registered imagepipe.Encoder
	// produced -- gosx ships no such encoder in-tree.
	Format string `json:"format"`
	HashedAsset
}

type HashedAsset struct {
	File string `json:"file"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// SceneAssetManifest points at the build-time Scene3D asset optimization report.
type SceneAssetManifest struct {
	File                string `json:"file"`
	Count               int    `json:"count"`
	Bytes               int64  `json:"bytes"`
	OptimizationActions int    `json:"optimizationActions"`
	Variants            int    `json:"variants,omitempty"`
}

type RuntimePaths struct {
	WASM                              string
	WASMIslands                       string
	WASMVariants                      map[string]string
	WASMExec                          string
	StandardGoWASMExec                string
	Bootstrap                         string
	BootstrapLite                     string
	BootstrapRuntime                  string
	BootstrapFeatureIslands           string
	BootstrapFeatureEngines           string
	BootstrapFeatureHubs              string
	BootstrapFeatureControllers       string
	BootstrapFeatureTextlayout        string
	BootstrapFeatureScene3D           string
	BootstrapFeatureScene3DCommand    string
	BootstrapFeatureScene3DWebGPU     string
	BootstrapFeatureScene3DWebGL      string
	BootstrapFeatureScene3DGLTF       string
	BootstrapFeatureScene3DAnimation  string
	BootstrapFeatureScene3DCompute    string
	BootstrapFeatureScene3DDecompress string
	Patch                             string
	VideoHLS                          string
	StripeBridge                      string
	Relay                             string
}

// Load reads a build manifest from disk.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read build manifest %s: %w", path, err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode build manifest %s: %w", path, err)
	}
	return &manifest, nil
}

// RuntimeURLs returns the public URLs for the shared runtime assets.
func (m *Manifest) RuntimeURLs(assetBaseURL string) RuntimePaths {
	variants := make(map[string]string, len(m.Runtime.WASMVariants))
	for id, asset := range m.Runtime.WASMVariants {
		variants[id] = AssetURL(assetBaseURL, "runtime", asset.File)
	}
	return RuntimePaths{
		WASM:                              AssetURL(assetBaseURL, "runtime", m.Runtime.WASM.File),
		WASMIslands:                       AssetURL(assetBaseURL, "runtime", m.Runtime.WASMIslands.File),
		WASMVariants:                      variants,
		WASMExec:                          AssetURL(assetBaseURL, "runtime", m.Runtime.WASMExec.File),
		StandardGoWASMExec:                AssetURL(assetBaseURL, "runtime", m.Runtime.StandardGoWASMExec.File),
		Bootstrap:                         AssetURL(assetBaseURL, "runtime", m.Runtime.Bootstrap.File),
		BootstrapLite:                     AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapLite.File),
		BootstrapRuntime:                  AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapRuntime.File),
		BootstrapFeatureIslands:           AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureIslands.File),
		BootstrapFeatureEngines:           AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureEngines.File),
		BootstrapFeatureHubs:              AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureHubs.File),
		BootstrapFeatureControllers:       AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureControllers.File),
		BootstrapFeatureTextlayout:        AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureTextlayout.File),
		BootstrapFeatureScene3D:           AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureScene3D.File),
		BootstrapFeatureScene3DCommand:    AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureScene3DCommand.File),
		BootstrapFeatureScene3DWebGPU:     AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureScene3DWebGPU.File),
		BootstrapFeatureScene3DWebGL:      AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureScene3DWebGL.File),
		BootstrapFeatureScene3DGLTF:       AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureScene3DGLTF.File),
		BootstrapFeatureScene3DAnimation:  AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureScene3DAnimation.File),
		BootstrapFeatureScene3DCompute:    AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureScene3DCompute.File),
		BootstrapFeatureScene3DDecompress: AssetURL(assetBaseURL, "runtime", m.Runtime.BootstrapFeatureScene3DDecompress.File),
		Patch:                             AssetURL(assetBaseURL, "runtime", m.Runtime.Patch.File),
		VideoHLS:                          AssetURL(assetBaseURL, "runtime", m.Runtime.VideoHLS.File),
		StripeBridge:                      AssetURL(assetBaseURL, "runtime", m.Runtime.StripeBridge.File),
		Relay:                             AssetURL(assetBaseURL, "runtime", m.Runtime.Relay.File),
	}
}

// IslandAssetByName returns the hashed island asset for a component, if any.
func (m *Manifest) IslandAssetByName(componentName string) (IslandAsset, bool) {
	for _, asset := range m.Islands {
		if asset.Name == componentName {
			return asset, true
		}
	}
	return IslandAsset{}, false
}

// ContentHash returns the first 16 hex characters (8 bytes) of the sha256
// digest of data. This is the one hashing scheme every build-time content
// hash in a manifest uses — asset file hashes (HashedAsset.Hash) and island
// source hashes (IslandAsset.SourceHash) alike — so a hash comparison never
// silently compares two different algorithms.
func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// StaleIsland names one island asset whose recorded SourceHash no longer
// matches the current bytes of its SourceFile, together with that
// SourceFile so a caller can name the changed file in a log message.
type StaleIsland struct {
	Name       string
	SourceFile string
}

// StaleIslands returns one StaleIsland for every island asset whose recorded
// SourceHash no longer matches the current bytes of its SourceFile under
// sourceRoot. It reports nothing, and never returns an error, in any of
// these cases:
//
//   - sourceRoot is empty (no app source directory known).
//   - The island's SourceFile or SourceHash is empty — the manifest
//     predates issue #166's staleness fields, so there is nothing to
//     compare against. Reporting staleness here would be a false positive.
//   - SourceFile is not local to sourceRoot (it escapes via ".." or is
//     rooted) — a manifest should never record such a path, so this is
//     defensive, not an expected case.
//   - SourceFile cannot be read under sourceRoot — a production image may
//     ship dist/ without the app's .gsx sources, and that is not an error.
//
// Recomputing the build-time program hash would require the full compiler
// pipeline (parse, lower, encode); SourceHash is the cheap, honest
// alternative recorded once at build time expressly for this comparison.
func (m *Manifest) StaleIslands(sourceRoot string) []StaleIsland {
	if m == nil {
		return nil
	}
	sourceRoot = strings.TrimSpace(sourceRoot)
	if sourceRoot == "" {
		return nil
	}

	var stale []StaleIsland
	for _, asset := range m.Islands {
		if strings.TrimSpace(asset.SourceFile) == "" || strings.TrimSpace(asset.SourceHash) == "" {
			continue
		}
		rel := filepath.FromSlash(asset.SourceFile)
		if !filepath.IsLocal(rel) {
			continue
		}
		path := filepath.Join(sourceRoot, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if ContentHash(data) != asset.SourceHash {
			stale = append(stale, StaleIsland{Name: asset.Name, SourceFile: asset.SourceFile})
		}
	}
	return stale
}

// IslandURL returns the public URL for an island program asset.
func (m *Manifest) IslandURL(assetBaseURL string, asset IslandAsset) string {
	return AssetURL(assetBaseURL, "islands", asset.File)
}

// CSSURL returns the public URL for a CSS asset.
func (m *Manifest) CSSURL(assetBaseURL string, asset CSSAsset) string {
	return AssetURL(assetBaseURL, "css", asset.File)
}

// ImageURL returns the public URL for a build-time image variant.
func (m *Manifest) ImageURL(assetBaseURL string, asset ImageVariantAsset) string {
	return AssetURL(assetBaseURL, "images", asset.File)
}

// CSSAssetBySource returns the hashed CSS asset for a source-relative path, if any.
func (m *Manifest) CSSAssetBySource(source string) (CSSAsset, bool) {
	for _, asset := range m.CSS {
		if asset.Source == source {
			return asset, true
		}
	}
	return CSSAsset{}, false
}

// ImageAssetBySource returns the recorded image asset for a source's
// root-relative public URL, if gosx build generated variants for it. It
// returns false, always, for a manifest written before issue #200 (Images
// is nil).
func (m *Manifest) ImageAssetBySource(source string) (ImageAsset, bool) {
	if m == nil {
		return ImageAsset{}, false
	}
	for _, asset := range m.Images {
		if asset.Source == source {
			return asset, true
		}
	}
	return ImageAsset{}, false
}

// ImageVariant returns the hashed build-time output for a source image at
// the given width and, if named, format. An empty format matches the first
// variant recorded at width, in gosx build's own generation order -- by
// default that is the source's own native format (imagepipe ships no WebP
// encoder; see cmd/gosx's imagePipeNativeFormat and imagePipeExtraFormats),
// so a caller that never named an explicit format still lands on whatever
// gosx build actually produced for it. It returns false if source has no
// recorded ImageAsset, or that asset has no matching variant at exactly
// this width; gosx build never upscales, so a width above the source's
// intrinsic Width never matches.
func (m *Manifest) ImageVariant(source string, width int, format string) (ImageVariantAsset, bool) {
	format = strings.ToLower(strings.TrimSpace(format))
	asset, ok := m.ImageAssetBySource(source)
	if !ok {
		return ImageVariantAsset{}, false
	}
	for _, variant := range asset.Variants {
		if variant.Width != width {
			continue
		}
		if format == "" || variant.Format == format {
			return variant, true
		}
	}
	return ImageVariantAsset{}, false
}

// AssetURL joins the mounted public asset root with a build output file.
func AssetURL(assetBaseURL, bucket, file string) string {
	if file == "" {
		return ""
	}

	base := strings.TrimRight(assetBaseURL, "/")
	suffix := bucket + "/" + strings.TrimLeft(file, "/")
	if base == "" {
		return "/" + suffix
	}
	return base + "/" + suffix
}

// ExportFilePath returns the stable static-export file path for a route.
func ExportFilePath(routePath string) string {
	routePath = strings.TrimSpace(routePath)
	if routePath == "" || routePath == "/" {
		return "index.html"
	}
	clean := strings.Trim(routePath, "/")
	return filepath.Join(filepath.FromSlash(clean), "index.html")
}
