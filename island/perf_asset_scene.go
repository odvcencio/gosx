package island

import (
	"encoding/json"
	"m31labs.dev/gosx/internal/sceneprobe"
	"net/url"
	"path"
	"regexp"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
)

var perfKTX2Source = regexp.MustCompile(`(?i)\.ktx2(?:[?#]|$)`)

// These mount gates supplement the emitted scripts/preloads, which can start
// a request even when a scene's mount does not use the advertised feature.
func (r *Renderer) legacyPerfSceneStartupGates() (map[string]bool, bool, error) {
	gates, models := map[string]bool{}, false
	for _, entry := range r.manifest.Engines {
		if !strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D") {
			continue
		}
		props := map[string]any{}
		if len(entry.Props) != 0 && json.Unmarshal(entry.Props, &props) != nil {
			return nil, false, &buildmanifest.PerfAssetError{Code: "unknown-reachability", Pointer: "/scene3d/props"}
		}
		scene, _ := props["scene"].(map[string]any)
		list := func(name string) []any {
			if values, ok := scene[name].([]any); ok {
				return values
			}
			values, _ := props[name].([]any)
			return values
		}
		gates["zoom"] = gates["zoom"] || props["controlZoom"] == true
		controls, _ := props["controls"].(string)
		_, walk := props["walk"].(map[string]any)
		mode := strings.ToLower(strings.TrimSpace(controls))
		gates["walk"] = gates["walk"] || walk && (mode == "first-person" || mode == "firstperson" || mode == "fps")
		vessel, _ := props["vessel"].(map[string]any)
		node, _ := vessel["nodeId"].(string)
		gates["vessel"] = gates["vessel"] || node != ""
		gates["ocean-query"] = gates["vessel"]
		gates["compute"] = gates["compute"] || len(list("computeParticles")) > 0 || len(list("instancedMeshes")) > 0
		gates["decompress"] = gates["decompress"] || perfJSTruthy(props["compression"])
		compressed := func(values []any) bool {
			for _, value := range values {
				row, _ := value.(map[string]any)
				for _, field := range []string{"generator", "compressedPositions", "compressedSizes", "compressedTransforms", "compressedTimes", "compressedValues", "previewPositions", "previewSizes", "previewTransforms", "previewTimes", "previewValues"} {
					if perfJSTruthy(row[field]) {
						return true
					}
				}
			}
			return false
		}
		gates["decompress"] = gates["decompress"] || compressed(list("points")) || compressed(list("instancedMeshes"))
		for _, value := range list("animations") {
			clip, _ := value.(map[string]any)
			channels, _ := clip["channels"].([]any)
			gates["decompress"] = gates["decompress"] || compressed(channels)
		}
		for _, field := range []string{"models", "instancedGLBMeshes"} {
			for _, value := range list(field) {
				model, _ := value.(map[string]any)
				src, _ := model["src"].(string)
				preview, _ := model["previewSrc"].(string)
				full, _ := model["fullSrc"].(string)
				if perfJSTruthy(model["progressive"]) && strings.TrimSpace(preview) != "" && strings.TrimSpace(full) != "" {
					src = preview
				}
				if strings.TrimSpace(src) == "" {
					continue
				}
				models = true
				pathname := strings.SplitN(strings.SplitN(strings.TrimSpace(src), "?", 2)[0], "#", 2)[0]
				if parsed, err := url.Parse(strings.TrimSpace(src)); err == nil {
					pathname = parsed.Path
				}
				ext := strings.ToLower(path.Ext(pathname))
				gates["gltf"] = gates["gltf"] || ext == ".glb" || ext == ".gltf"
			}
		}
		// The IBL/KTX2 helpers choose the nested scene without merging props.
		if scene == nil {
			scene = props
		}
		environment, _ := scene["environment"].(map[string]any)
		ibl, _ := environment["ibl"].(map[string]any)
		complete := true
		for _, field := range []string{"radiance", "irradiance", "brdfLUT"} {
			product, _ := ibl[field].(map[string]any)
			uri, _ := product["uri"].(string)
			complete = complete && strings.TrimSpace(uri) != ""
		}
		ktx2 := func(value any) bool {
			src, _ := value.(string)
			return perfKTX2Source.MatchString(strings.TrimSpace(src))
		}
		gates["gltf"] = gates["gltf"] || complete || ktx2(environment["envMap"])
		for _, field := range []string{"objects", "models", "instancedMeshes", "points", "sprites"} {
			values, _ := scene[field].([]any)
			for _, value := range values {
				node, _ := value.(map[string]any)
				for _, key := range []string{"texture", "normalMap", "roughnessMap", "metalnessMap", "occlusionMap", "emissiveMap", "specularIntensityMap", "specularColorMap"} {
					gates["gltf"] = gates["gltf"] || ktx2(node[key])
				}
				descriptors, _ := node["textureDescriptors"].(map[string]any)
				for _, descriptor := range descriptors {
					texture, _ := descriptor.(map[string]any)
					gates["gltf"] = gates["gltf"] || ktx2(texture["uri"])
				}
			}
		}
	}
	return gates, models, nil
}

func perfJSTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	default:
		return true
	}
}

// Healthy Scene3D entries reuse the same decoded snapshot as preload emission.
func (r *Renderer) perfSceneStartupGates() (map[string]bool, bool, error) {
	gates, models := map[string]bool{}, false
	for i := range r.manifest.Engines {
		entry := &r.manifest.Engines[i]
		if !strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D") {
			continue
		}
		p := sceneProbe(entry)
		if p.Err != nil {
			return r.legacyPerfSceneStartupGates()
		}
		list := func(top, nested []sceneprobe.Record) []sceneprobe.Record {
			if nested != nil {
				return nested
			}
			return top
		}
		scene := p.Scene
		if scene == nil {
			scene = p
		}
		mode := strings.ToLower(strings.TrimSpace(p.Controls))
		gates["zoom"] = gates["zoom"] || p.ControlZoom
		gates["walk"] = gates["walk"] || p.Walk != nil && (mode == "first-person" || mode == "firstperson" || mode == "fps")
		gates["vessel"] = gates["vessel"] || p.Vessel != nil && p.Vessel.NodeID != ""
		gates["ocean-query"] = gates["vessel"]
		particles, meshes := p.ComputeParticles, p.InstancedMeshes
		points, animations := p.Points, p.Animations
		regular, crowds := p.Models, p.InstancedGLBMeshes
		if p.Scene != nil {
			particles = list(particles, p.Scene.ComputeParticles)
			meshes = list(meshes, p.Scene.InstancedMeshes)
			points = list(points, p.Scene.Points)
			if p.Scene.Animations != nil {
				animations = p.Scene.Animations
			}
			regular = list(regular, p.Scene.Models)
			crowds = list(crowds, p.Scene.InstancedGLBMeshes)
		}
		gates["compute"] = gates["compute"] || len(particles) > 0 || len(meshes) > 0
		gates["decompress"] = gates["decompress"] || p.Compression.Truthy
		compressed := func(records []sceneprobe.Record) bool {
			for _, row := range records {
				if row.PerfNeedsDecompress() {
					return true
				}
			}
			return false
		}
		gates["decompress"] = gates["decompress"] || compressed(points) || compressed(meshes)
		for _, clip := range animations {
			gates["decompress"] = gates["decompress"] || compressed(clip.Channels)
		}
		for _, records := range [][]sceneprobe.Record{regular, crowds} {
			for _, model := range records {
				src := model.Src
				if model.Progressive && strings.TrimSpace(model.PreviewSrc) != "" && strings.TrimSpace(model.FullSrc) != "" {
					src = model.PreviewSrc
				}
				if strings.TrimSpace(src) == "" {
					continue
				}
				models = true
				pathname := strings.SplitN(strings.SplitN(strings.TrimSpace(src), "?", 2)[0], "#", 2)[0]
				if parsed, err := url.Parse(strings.TrimSpace(src)); err == nil {
					pathname = parsed.Path
				}
				ext := strings.ToLower(path.Ext(pathname))
				gates["gltf"] = gates["gltf"] || ext == ".glb" || ext == ".gltf"
			}
		}
		ibl := scene.Environment.IBL
		ktx := func(src string) bool { return perfKTX2Source.MatchString(strings.TrimSpace(src)) }
		gates["gltf"] = gates["gltf"] || strings.TrimSpace(ibl.Radiance.URI) != "" && strings.TrimSpace(ibl.Irradiance.URI) != "" && strings.TrimSpace(ibl.BRDFLUT.URI) != "" || ktx(scene.Environment.EnvMap)
		for _, records := range [][]sceneprobe.Record{scene.Objects, scene.Models, scene.InstancedMeshes, scene.Points, scene.Sprites} {
			for _, record := range records {
				for _, src := range []string{record.Texture, record.NormalMap, record.RoughnessMap, record.MetalnessMap, record.OcclusionMap, record.EmissiveMap, record.SpecularIntensityMap, record.SpecularColorMap} {
					gates["gltf"] = gates["gltf"] || ktx(src)
				}
				for _, descriptor := range record.TextureDescriptors {
					gates["gltf"] = gates["gltf"] || ktx(descriptor.URI)
				}
			}
		}
	}
	return gates, models, nil
}
