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
// never reach the CPU pixel. render/gpu/headless decodes base colour, opacity,
// emissive colour, emissive scale, the vertex-colour flag, and one base colour
// texture, and nothing else.
var ignoredMaterialFields = []string{
	"anisotropy",
	"clearcoat",
	"emissiveMap",
	"iridescence",
	"metalness",
	"metalnessMap",
	"normalMap",
	"roughness",
	"roughnessMap",
	"sheen",
	"transmission",
	"wireframe",
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
		countIgnoredMaterialFields(count, object.Roughness, object.Metalness, object.Clearcoat,
			object.Sheen, object.Transmission, object.Iridescence, object.Anisotropy,
			object.NormalMap, object.RoughnessMap, object.MetalnessMap, object.EmissiveMap,
			boolValue(object.Wireframe))
	}
	for _, mesh := range ir.InstancedMeshes {
		countIgnoredMaterialFields(count, mesh.Roughness, mesh.Metalness, mesh.Clearcoat,
			mesh.Sheen, mesh.Transmission, mesh.Iridescence, mesh.Anisotropy,
			mesh.NormalMap, mesh.RoughnessMap, mesh.MetalnessMap, mesh.EmissiveMap,
			boolValue(mesh.Wireframe))
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
		Message: "native preview shades from base color, opacity, emissive, and one base color texture; " +
			"these authored fields did not change the frame, with the number of records that set each: " +
			strings.Join(names, ", "),
	}, true
}

func countIgnoredMaterialFields(count func(string, bool), roughness, metalness, clearcoat, sheen,
	transmission, iridescence, anisotropy float64,
	normalMap, roughnessMap, metalnessMap, emissiveMap string, wireframe bool) {
	count("roughness", roughness != 0)
	count("metalness", metalness != 0)
	count("clearcoat", clearcoat != 0)
	count("sheen", sheen != 0)
	count("transmission", transmission != 0)
	count("iridescence", iridescence != 0)
	count("anisotropy", anisotropy != 0)
	count("normalMap", strings.TrimSpace(normalMap) != "")
	count("roughnessMap", strings.TrimSpace(roughnessMap) != "")
	count("metalnessMap", strings.TrimSpace(metalnessMap) != "")
	count("emissiveMap", strings.TrimSpace(emissiveMap) != "")
	count("wireframe", wireframe)
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
