package game

import (
	"fmt"
	"sort"
	"strings"
)

// AssetKind identifies the broad pipeline bucket for an asset.
type AssetKind string

const (
	AssetGLTF    AssetKind = "gltf"
	AssetGLB     AssetKind = "glb"
	AssetTexture AssetKind = "texture"
	AssetAudio   AssetKind = "audio"
	AssetShader  AssetKind = "shader"
	AssetData    AssetKind = "data"
	AssetFont    AssetKind = "font"
)

// AssetID is a stable asset handle.
type AssetID string

// AssetRef is a serializable handle in the runtime asset manifest.
type AssetRef struct {
	ID          AssetID           `json:"id"`
	Kind        AssetKind         `json:"kind"`
	URI         string            `json:"uri"`
	ContentType string            `json:"contentType,omitempty"`
	Preload     bool              `json:"preload,omitempty"`
	Variants    []AssetVariant    `json:"variants,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// AssetVariant describes an optional representation of an asset. Variants are
// ordered by preference; selection returns the first variant whose required
// capabilities are satisfied, otherwise the base AssetRef remains the fallback.
type AssetVariant struct {
	URI                  string            `json:"uri"`
	ContentType          string            `json:"contentType,omitempty"`
	Quality              string            `json:"quality,omitempty"`
	Compression          string            `json:"compression,omitempty"`
	Bytes                int64             `json:"bytes,omitempty"`
	RequiredCapabilities []string          `json:"requiredCapabilities,omitempty"`
	Metadata             map[string]string `json:"metadata,omitempty"`
}

// Asset creates a typed runtime asset reference.
func Asset(id AssetID, kind AssetKind, uri string) AssetRef {
	return AssetRef{ID: id, Kind: kind, URI: uri}
}

// GLTF creates a glTF scene/model asset reference.
func GLTF(id AssetID, uri string) AssetRef {
	return Asset(id, AssetGLTF, uri)
}

// GLB creates a binary glTF scene/model asset reference.
func GLB(id AssetID, uri string) AssetRef {
	return Asset(id, AssetGLB, uri)
}

// Texture creates a texture asset reference.
func Texture(id AssetID, uri string) AssetRef {
	return Asset(id, AssetTexture, uri)
}

// Audio creates an audio asset reference.
func Audio(id AssetID, uri string) AssetRef {
	return Asset(id, AssetAudio, uri)
}

// Shader creates a shader asset reference.
func Shader(id AssetID, uri string) AssetRef {
	return Asset(id, AssetShader, uri)
}

// Data creates a data asset reference.
func Data(id AssetID, uri string) AssetRef {
	return Asset(id, AssetData, uri)
}

// Font creates a font asset reference.
func Font(id AssetID, uri string) AssetRef {
	return Asset(id, AssetFont, uri)
}

// WithContentType returns a copy of ref with a MIME/content type.
func WithContentType(ref AssetRef, contentType string) AssetRef {
	ref.ContentType = strings.TrimSpace(contentType)
	return ref
}

// WithPreload returns a copy of ref marked for page/runtime preload.
func WithPreload(ref AssetRef) AssetRef {
	ref.Preload = true
	return ref
}

// WithMetadata returns a copy of ref with one metadata key/value set.
func WithMetadata(ref AssetRef, key, value string) AssetRef {
	key = strings.TrimSpace(key)
	if key == "" {
		return ref
	}
	if ref.Metadata == nil {
		ref.Metadata = make(map[string]string, 1)
	}
	ref.Metadata[key] = value
	return ref
}

// Variant creates an asset representation for WithVariant.
func Variant(uri string) AssetVariant {
	return AssetVariant{URI: uri}
}

// VariantContentType returns a copy of variant with a MIME/content type.
func VariantContentType(variant AssetVariant, contentType string) AssetVariant {
	variant.ContentType = strings.TrimSpace(contentType)
	return variant
}

// VariantQuality returns a copy of variant tagged with a quality label such as
// "ultra", "high", "medium", or "low".
func VariantQuality(variant AssetVariant, quality string) AssetVariant {
	variant.Quality = strings.TrimSpace(quality)
	return variant
}

// VariantCompression returns a copy of variant tagged with its compression
// family, for example "ktx2", "basis", "meshopt", or "draco".
func VariantCompression(variant AssetVariant, compression string) AssetVariant {
	variant.Compression = strings.TrimSpace(compression)
	return variant
}

// VariantBytes returns a copy of variant with an expected encoded byte size.
func VariantBytes(variant AssetVariant, bytes int64) AssetVariant {
	if bytes > 0 {
		variant.Bytes = bytes
	}
	return variant
}

// VariantCapabilities returns a copy of variant gated by browser/runtime
// capabilities. Use lowercase capability names such as "webgpu", "webgl2",
// "ktx2", or "meshopt".
func VariantCapabilities(variant AssetVariant, capabilities ...string) AssetVariant {
	variant.RequiredCapabilities = normalizeCapabilityStrings(capabilities)
	return variant
}

// VariantMetadata returns a copy of variant with one metadata key/value set.
func VariantMetadata(variant AssetVariant, key, value string) AssetVariant {
	key = strings.TrimSpace(key)
	if key == "" {
		return variant
	}
	if variant.Metadata == nil {
		variant.Metadata = make(map[string]string, 1)
	}
	variant.Metadata[key] = value
	return variant
}

// WithVariant appends an optional representation to ref. Variants are tried in
// order by SelectAssetVariant and ManifestFor.
func WithVariant(ref AssetRef, variant AssetVariant) AssetRef {
	ref.Variants = append(ref.Variants, variant)
	return ref
}

// Assets stores declared runtime assets by stable ID.
type Assets struct {
	byID map[AssetID]AssetRef
}

// NewAssets creates an empty asset registry.
func NewAssets() *Assets {
	return &Assets{byID: make(map[AssetID]AssetRef)}
}

// Register inserts or replaces an asset reference.
func (a *Assets) Register(ref AssetRef) (AssetRef, error) {
	if a == nil {
		return AssetRef{}, fmt.Errorf("game assets registry is nil")
	}
	ref.ID = AssetID(strings.TrimSpace(string(ref.ID)))
	ref.Kind = AssetKind(strings.ToLower(strings.TrimSpace(string(ref.Kind))))
	ref.URI = strings.TrimSpace(ref.URI)
	if ref.ID == "" {
		return AssetRef{}, fmt.Errorf("asset id is required")
	}
	if ref.Kind == "" {
		return AssetRef{}, fmt.Errorf("asset %q kind is required", ref.ID)
	}
	if ref.URI == "" {
		return AssetRef{}, fmt.Errorf("asset %q uri is required", ref.ID)
	}
	variants, err := normalizeAssetVariants(ref.Variants)
	if err != nil {
		return AssetRef{}, fmt.Errorf("asset %q: %w", ref.ID, err)
	}
	ref.Variants = variants
	if a.byID == nil {
		a.byID = make(map[AssetID]AssetRef)
	}
	a.byID[ref.ID] = cloneAssetRef(ref)
	return ref, nil
}

// MustRegister inserts an asset reference.
//
// Deprecated: use Register and check the returned error. This method no
// longer panics so invalid asset manifests cannot crash production requests.
func (a *Assets) MustRegister(ref AssetRef) (AssetRef, error) {
	return a.Register(ref)
}

// RegisterAll inserts or replaces multiple asset references.
func (a *Assets) RegisterAll(refs ...AssetRef) error {
	for _, ref := range refs {
		if _, err := a.Register(ref); err != nil {
			return err
		}
	}
	return nil
}

// MustRegisterAll inserts multiple asset references.
//
// Deprecated: use RegisterAll and check the returned error. This method no
// longer panics so invalid asset manifests cannot crash production requests.
func (a *Assets) MustRegisterAll(refs ...AssetRef) error {
	return a.RegisterAll(refs...)
}

// Resolve looks up an asset by ID.
func (a *Assets) Resolve(id AssetID) (AssetRef, bool) {
	if a == nil {
		return AssetRef{}, false
	}
	ref, ok := a.byID[AssetID(strings.TrimSpace(string(id)))]
	if !ok {
		return AssetRef{}, false
	}
	return cloneAssetRef(ref), true
}

// Manifest returns all asset references sorted by ID for stable transport.
func (a *Assets) Manifest() []AssetRef {
	if a == nil || len(a.byID) == 0 {
		return nil
	}
	ids := make([]string, 0, len(a.byID))
	for id := range a.byID {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	out := make([]AssetRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneAssetRef(a.byID[AssetID(id)]))
	}
	return out
}

// ManifestFor returns a concrete manifest selected for the supplied
// capabilities. The returned refs have their URI/content type replaced by the
// first compatible variant and omit the variant list so downstream runtimes see
// one resolved asset per ID.
func (a *Assets) ManifestFor(capabilities ...string) []AssetRef {
	if a == nil || len(a.byID) == 0 {
		return nil
	}
	ids := make([]string, 0, len(a.byID))
	for id := range a.byID {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	out := make([]AssetRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, SelectAssetVariant(a.byID[AssetID(id)], capabilities...))
	}
	return out
}

// ResolveFor looks up an asset and selects its best variant for capabilities.
func (a *Assets) ResolveFor(id AssetID, capabilities ...string) (AssetRef, bool) {
	ref, ok := a.Resolve(id)
	if !ok {
		return AssetRef{}, false
	}
	return SelectAssetVariant(ref, capabilities...), true
}

// ByKind returns all assets of kind sorted by ID.
func (a *Assets) ByKind(kind AssetKind) []AssetRef {
	if a == nil || len(a.byID) == 0 {
		return nil
	}
	kind = AssetKind(strings.ToLower(strings.TrimSpace(string(kind))))
	var out []AssetRef
	for _, ref := range a.byID {
		if ref.Kind == kind {
			out = append(out, cloneAssetRef(ref))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Preloads returns all assets explicitly marked for preload, sorted by ID.
func (a *Assets) Preloads() []AssetRef {
	if a == nil || len(a.byID) == 0 {
		return nil
	}
	var out []AssetRef
	for _, ref := range a.byID {
		if ref.Preload {
			out = append(out, cloneAssetRef(ref))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SelectAssetVariant resolves ref to the first compatible variant for the
// supplied capabilities. If no variant matches, the base ref is returned.
func SelectAssetVariant(ref AssetRef, capabilities ...string) AssetRef {
	selected := cloneAssetRef(ref)
	selected.Variants = nil
	capSet := capabilitySet(capabilities)
	for _, variant := range ref.Variants {
		if !variantCapabilitiesSatisfied(variant, capSet) {
			continue
		}
		selected.URI = variant.URI
		if variant.ContentType != "" {
			selected.ContentType = variant.ContentType
		}
		if len(variant.Metadata) > 0 {
			selected.Metadata = cloneStringMap(selected.Metadata)
			if selected.Metadata == nil {
				selected.Metadata = map[string]string{}
			}
			for key, value := range variant.Metadata {
				selected.Metadata[key] = value
			}
		}
		if variant.Quality != "" {
			selected = WithMetadata(selected, "quality", variant.Quality)
		}
		if variant.Compression != "" {
			selected = WithMetadata(selected, "compression", variant.Compression)
		}
		return selected
	}
	return selected
}

func cloneAssetRef(ref AssetRef) AssetRef {
	if len(ref.Variants) > 0 {
		ref.Variants = cloneAssetVariants(ref.Variants)
	}
	if len(ref.Metadata) > 0 {
		meta := make(map[string]string, len(ref.Metadata))
		for key, value := range ref.Metadata {
			meta[key] = value
		}
		ref.Metadata = meta
	}
	return ref
}

func normalizeAssetVariants(variants []AssetVariant) ([]AssetVariant, error) {
	if len(variants) == 0 {
		return nil, nil
	}
	out := make([]AssetVariant, 0, len(variants))
	for i, variant := range variants {
		variant.URI = strings.TrimSpace(variant.URI)
		if variant.URI == "" {
			return nil, fmt.Errorf("variant %d uri is required", i)
		}
		variant.ContentType = strings.TrimSpace(variant.ContentType)
		variant.Quality = strings.TrimSpace(variant.Quality)
		variant.Compression = strings.TrimSpace(variant.Compression)
		variant.RequiredCapabilities = normalizeCapabilityStrings(variant.RequiredCapabilities)
		if len(variant.Metadata) > 0 {
			variant.Metadata = cloneStringMap(variant.Metadata)
		}
		out = append(out, variant)
	}
	return out, nil
}

func cloneAssetVariants(in []AssetVariant) []AssetVariant {
	if len(in) == 0 {
		return nil
	}
	out := make([]AssetVariant, len(in))
	copy(out, in)
	for i := range out {
		if len(out[i].RequiredCapabilities) > 0 {
			out[i].RequiredCapabilities = append([]string(nil), out[i].RequiredCapabilities...)
		}
		if len(out[i].Metadata) > 0 {
			out[i].Metadata = cloneStringMap(out[i].Metadata)
		}
	}
	return out
}

func normalizeCapabilityStrings(capabilities []string) []string {
	if len(capabilities) == 0 {
		return nil
	}
	out := make([]string, 0, len(capabilities))
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		capability = strings.TrimSpace(strings.ToLower(capability))
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	return out
}

func capabilitySet(capabilities []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, capability := range normalizeCapabilityStrings(capabilities) {
		set[capability] = struct{}{}
	}
	return set
}

func variantCapabilitiesSatisfied(variant AssetVariant, capabilities map[string]struct{}) bool {
	if len(variant.RequiredCapabilities) == 0 {
		return true
	}
	for _, capability := range variant.RequiredCapabilities {
		if _, ok := capabilities[strings.TrimSpace(strings.ToLower(capability))]; !ok {
			return false
		}
	}
	return true
}
