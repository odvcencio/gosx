// Package manifest defines the file `gosx assets build` writes and the runtime
// side reads. It depends on the standard library only, so the scene package
// and a js/wasm game build can import it without pulling in the asset
// pipeline.
package manifest

import (
	"encoding/json"
	"fmt"
	"sort"
)

// SchemaVersion versions the manifest contract.
const SchemaVersion = 1

// Manifest lists every model one build produced.
type Manifest struct {
	SchemaVersion int         `json:"schemaVersion"`
	Models        []Model     `json:"models"`
	Violations    []Violation `json:"violations,omitempty"`
	Warnings      []string    `json:"warnings,omitempty"`
}

// Model is one built model: the main GLB, its LOD chain, and its textures.
// URIs are relative to the directory that holds the manifest.
type Model struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	// URI is the full-detail GLB. It is LODs[0].URI.
	URI   string `json:"uri"`
	Bytes int64  `json:"bytes"`
	// Radius is the radius of the model's world-space bounding sphere around
	// its bounds center, in model units. Zero means the bounds were unknown.
	Radius float64 `json:"radius"`
	Stats  Stats   `json:"stats"`
	// Meshopt is set when the GLBs use EXT_meshopt_compression. It describes the
	// full-detail level.
	Meshopt  *Meshopt  `json:"meshopt,omitempty"`
	LODs     []LOD     `json:"lods"`
	Textures []Texture `json:"textures,omitempty"`
}

// Stats counts what one GLB asks of the renderer.
type Stats struct {
	Triangles int `json:"triangles"`
	Vertices  int `json:"vertices"`
	// DrawCalls counts one per mesh primitive instance reachable from the
	// default scene. An EXT_mesh_gpu_instancing node counts once.
	DrawCalls int `json:"drawCalls"`
	Materials int `json:"materials"`
	// Joints is the largest joint count of any skin.
	Joints int `json:"joints"`
	// TextureBytes estimates GPU memory for the model's images decoded to
	// RGBA8 with a full mip chain.
	TextureBytes int64 `json:"textureBytes"`
}

// Meshopt records the effect of mesh compression on the full-detail GLB.
type Meshopt struct {
	// Views counts the compressed buffer views; Skipped counts mesh views that
	// kept their bytes because no codec could hold them.
	Views   int `json:"views"`
	Skipped int `json:"skipped,omitempty"`
	// ViewBytesBefore and ViewBytesAfter are the bytes of the compressed views.
	ViewBytesBefore int `json:"viewBytesBefore"`
	ViewBytesAfter  int `json:"viewBytesAfter"`
}

// LOD is one level of detail. Level 0 is the full-detail model.
type LOD struct {
	Level     int    `json:"level"`
	URI       string `json:"uri"`
	Triangles int    `json:"triangles"`
	Bytes     int64  `json:"bytes"`
	// Ratio is Triangles divided by the level 0 triangle count.
	Ratio float64 `json:"ratio"`
	// Distance is the camera distance at which this level becomes active.
	// Level 0 is always 0.
	Distance float64 `json:"distance"`
}

// Texture records the KTX2 files built from one image of a model.
type Texture struct {
	Image    string           `json:"image"`
	Role     string           `json:"role,omitempty"`
	Width    int              `json:"width"`
	Height   int              `json:"height"`
	Variants []TextureVariant `json:"variants"`
}

// TextureVariant is one KTX2 file.
type TextureVariant struct {
	URI    string `json:"uri"`
	Tier   string `json:"tier"`
	Format string `json:"format"`
	Bytes  int64  `json:"bytes"`
}

// Violation records one budget a model exceeded.
type Violation struct {
	Model  string `json:"model"`
	Budget string `json:"budget"`
	Limit  int64  `json:"limit"`
	Actual int64  `json:"actual"`
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s is %d, budget is %d", v.Model, v.Budget, v.Actual, v.Limit)
}

// Parse decodes a manifest and checks the fields a consumer relies on.
func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	if m.SchemaVersion != SchemaVersion {
		return Manifest{}, fmt.Errorf("manifest: schema version %d, want %d", m.SchemaVersion, SchemaVersion)
	}
	for _, model := range m.Models {
		if model.Name == "" || len(model.LODs) == 0 {
			return Manifest{}, fmt.Errorf("manifest: model %q has no name or no LOD levels", model.Name)
		}
		if !sort.SliceIsSorted(model.LODs, func(i, j int) bool { return model.LODs[i].Distance < model.LODs[j].Distance }) {
			return Manifest{}, fmt.Errorf("manifest: model %q LOD distances are not ascending", model.Name)
		}
	}
	return m, nil
}

// Model returns the entry with the given name.
func (m Manifest) Model(name string) (Model, bool) {
	for _, model := range m.Models {
		if model.Name == name {
			return model, true
		}
	}
	return Model{}, false
}
