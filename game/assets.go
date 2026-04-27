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
	Metadata    map[string]string `json:"metadata,omitempty"`
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
	if a.byID == nil {
		a.byID = make(map[AssetID]AssetRef)
	}
	a.byID[ref.ID] = cloneAssetRef(ref)
	return ref, nil
}

// MustRegister inserts an asset reference and panics on invalid input.
func (a *Assets) MustRegister(ref AssetRef) AssetRef {
	out, err := a.Register(ref)
	if err != nil {
		panic(err)
	}
	return out
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

func cloneAssetRef(ref AssetRef) AssetRef {
	if len(ref.Metadata) > 0 {
		meta := make(map[string]string, len(ref.Metadata))
		for key, value := range ref.Metadata {
			meta[key] = value
		}
		ref.Metadata = meta
	}
	return ref
}
