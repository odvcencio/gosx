package preview

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
)

// This file keeps the preview honest about materials, and makes base-colour
// textures actually visible.
//
// The CPU rasterizer shades from base colour, opacity, emissive, and one base
// colour texture. It reads no other material field. An author who changes
// roughness and sees an unchanged PNG deserves to be told why, so the frame
// carries one compact note listing the fields the frame did not use.
//
// The rasterizer also loads a texture by treating its source as a filesystem
// path. A web-root path such as "/textures/floor.png" never exists on disk, so
// the frame silently showed a placeholder checker. AssetRoots resolves those
// paths against real directories before the frame is built.

// ignoredMaterialFields lists the material fields that reach the renderer and
// never reach the CPU pixel.
//
// The list used to hold thirteen entries and was accurate then: the rasterizer
// shaded a Lambert term and read base colour, opacity, emissive and one base
// colour texture. render/gpu/headless now runs the whole litWGSL fragment
// stage, so roughness, metalness, clearcoat, sheen, iridescence, anisotropy,
// transmission, the normal map, the roughness map, the metalness map and the
// emissive map all reach a pixel. Keeping them here told an author their
// material was ignored when it was not, which is the more expensive direction
// of a wrong diagnostic: it invites deleting work that functions.
//
// wireframe left the list too: vs_main writes a barycentric coordinate from
// vertex_index % 3 (every mesh here draws non-indexed triangle soup), and
// fs_main discards fragments away from a triangle edge when
// material.textureParams2.z is set. render/gpu/headless mirrors the discard
// from the same perspective-correct weights the software rasterizer already
// carries per fragment. See TestPhysicallyBasedMaterialFieldsReachThePixels
// and TestWireframeDiscardsInteriorNotEdges in
// render/gpu/headless/material_gap_test.go.
//
// One remains.
//
// materialKind: render/bundle drops it before the GPU. materialFromRender reads
// colour, opacity, the physical scalars and the map slots, and never reads Kind.
// So a flat, ghost, glass, glow, matte or custom material still produces pixels
// byte-identical to a standard material with the same colour and opacity.
// TestMaterialKindsShadeIdentically pins that.
//
// Note on metalnessMap and emissiveMap: both are MODULATING maps. They scale a
// factor, so they change nothing when that factor is zero. They are sampled, and
// therefore not ignored — but an author who sees no change should check the
// scalar before suspecting the map.
var ignoredMaterialFields = []string{
	"materialKind",
}

// cpuBaselineMaterialKind is the one authored material kind that describes what
// the CPU rasterizer actually does. Any other kind changes nothing, so the frame
// says so; this kind changes nothing because nothing else was promised.
const cpuBaselineMaterialKind = "standard"

// materialKindIgnored reports whether an authored material kind promises a look
// the CPU rasterizer does not produce. An empty kind lowers to "standard", so it
// promises nothing and raises no note.
func materialKindIgnored(kind string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(kind))
	return trimmed != "" && trimmed != cpuBaselineMaterialKind
}

// IgnoredMaterialFields returns the authored material fields that the CPU
// preview does not read, in sorted order.
func IgnoredMaterialFields() []string {
	out := append([]string(nil), ignoredMaterialFields...)
	sort.Strings(out)
	return out
}

// materialCoverageDiagnostic returns one compact note naming the material
// fields the frame carried and the CPU rasterizer did not read. It returns
// ok=false when the scene sets none of them.
//
// The note is one diagnostic for the whole frame, not one per record. A typical
// scene sets roughness on every mesh, and a warning on each would bury the
// diagnostics that name a broken record.
func materialCoverageDiagnostic(ir scene.SceneIR) (engine.RenderDiagnostic, bool) {
	used := map[string]int{}
	count := func(name string, set bool) {
		if set {
			used[name]++
		}
	}
	for _, object := range ir.Objects {
		count("materialKind", materialKindIgnored(object.MaterialKind))
	}
	for _, mesh := range ir.InstancedMeshes {
		count("materialKind", materialKindIgnored(mesh.MaterialKind))
	}
	if len(used) == 0 {
		return engine.RenderDiagnostic{}, false
	}
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, fmt.Sprintf("%s(%d)", name, used[name]))
	}
	sort.Strings(names)
	return engine.RenderDiagnostic{
		Severity: "info",
		Code:     "scene.preview.material_fields_ignored",
		Backend:  "headless",
		Message: "native preview runs the full physically-based fragment stage; " +
			"these authored fields still did not change the frame, with the number of records that set each: " +
			strings.Join(names, ", "),
	}, true
}

// textureResolver rewrites base colour texture sources onto real files.
type textureResolver struct {
	roots   []string
	misses  map[string]string
	rewrote map[string]string
}

func newTextureResolver(roots []string) *textureResolver {
	clean := make([]string, 0, len(roots))
	for _, root := range roots {
		if trimmed := strings.TrimSpace(root); trimmed != "" {
			clean = append(clean, filepath.Clean(trimmed))
		}
	}
	sort.Strings(clean)
	return &textureResolver{roots: clean, misses: map[string]string{}, rewrote: map[string]string{}}
}

// resolve returns the on-disk path for a texture source when one root holds it.
// It returns the original source unchanged for remote sources, inline data
// sources, and sources that already name an existing file.
func (r *textureResolver) resolve(source string) string {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" || len(r.roots) == 0 {
		return source
	}
	lowered := strings.ToLower(trimmed)
	for _, prefix := range []string{"http://", "https://", "data:", "blob:", "gosx-html://", "file://", "var("} {
		if strings.HasPrefix(lowered, prefix) {
			return source
		}
	}
	if cached, ok := r.rewrote[trimmed]; ok {
		return cached
	}
	if info, err := os.Stat(trimmed); err == nil && !info.IsDir() {
		r.rewrote[trimmed] = trimmed
		return trimmed
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(stripTextureQuery(trimmed), "./"), "/")
	for _, root := range r.roots {
		candidate := filepath.Join(root, filepath.FromSlash(relative))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			r.rewrote[trimmed] = candidate
			return candidate
		}
	}
	r.misses[trimmed] = trimmed
	r.rewrote[trimmed] = source
	return source
}

// diagnostics reports every base colour texture that no root could supply. The
// rasterizer draws a placeholder checker for those, so the frame must say so.
func (r *textureResolver) diagnostics() []engine.RenderDiagnostic {
	if len(r.misses) == 0 {
		return nil
	}
	sources := make([]string, 0, len(r.misses))
	for source := range r.misses {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	out := make([]engine.RenderDiagnostic, 0, len(sources))
	for _, source := range sources {
		out = append(out, engine.RenderDiagnostic{
			Severity: "warning",
			Code:     "scene.preview.unresolved_texture",
			Backend:  "headless",
			Target:   source,
			Message: fmt.Sprintf("no asset root supplies %s, so the frame shows a placeholder checker instead of the image; roots searched: %s",
				source, strings.Join(r.roots, ", ")),
		})
	}
	return out
}

func stripTextureQuery(source string) string {
	if index := strings.IndexAny(source, "?#"); index >= 0 {
		return source[:index]
	}
	return source
}
