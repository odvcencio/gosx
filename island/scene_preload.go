package island

import (
	"fmt"
	"html"
	"m31labs.dev/gosx/internal/sceneprobe"
	"net/url"
	"strings"
)

func scenePreloadTextureDestination(src string) string {
	parsed, err := url.Parse(src)
	if err == nil {
		lower := strings.ToLower(parsed.Path)
		if strings.HasSuffix(lower, ".hdr") || strings.HasSuffix(lower, ".ktx2") {
			return "fetch"
		}
	}
	return "image"
}

func (r *Renderer) scene3DCanUseWebGPU() bool {
	for i := range r.manifest.Engines {
		entry := &r.manifest.Engines[i]
		if !strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D") {
			continue
		}
		props := sceneProbe(entry)
		if props.PreloadErr != nil {
			return true
		}
		if gpu, _ := props.WebGPU, props.WebGL; gpu {
			return true
		}
	}
	return false
}

func (r *Renderer) writeScene3DPreloads(b *strings.Builder) {
	seen := map[string]bool{}
	add := func(path, as string) {
		path = strings.TrimSpace(path)
		parsed, err := url.Parse(path)
		if path == "" || err != nil || (parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https") {
			return
		}
		key := as + ":" + path
		if seen[key] {
			return
		}
		seen[key] = true
		fmt.Fprintf(b, `<link rel="preload" href="%s" as="%s" crossorigin="anonymous"`, html.EscapeString(path), as)
		if as == "script" {
			b.WriteString(` referrerpolicy="no-referrer"`)
		}
		b.WriteString(">\n")
	}
	for i := range r.manifest.Engines {
		entry := &r.manifest.Engines[i]
		if !strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D") {
			continue
		}
		props := sceneProbe(entry)
		if props.PreloadErr != nil {
			continue
		}
		gpu, gl := props.WebGPU, props.WebGL
		if r.usesSelectiveRuntimeBootstrap() {
			if entry.ProgramRef != "" {
				add(r.bootstrapFeatureScene3dCommandPath, "script")
			}
			// Hint a renderer chunk only when the scene allows one backend.
			// With both candidates the browser picks at runtime, and a hint
			// for each would make every page download a renderer it never
			// runs; the chosen chunk then loads on demand.
			if gpu && !gl {
				add(r.bootstrapFeatureScene3dWebGPUPath, "script")
			}
			if gl && !gpu {
				add(r.bootstrapFeatureScene3dWebGLPath, "script")
			}
			chunks := props
			if chunks.ChunkErr == nil {
				if chunks.ComputeChunk {
					add(r.bootstrapFeatureScene3dComputePath, "script")
				}
				if chunks.DecompressChunk {
					add(r.bootstrapFeatureScene3dDecompressPath, "script")
				}
			}
		}
		s := props
		if props.Scene != nil {
			s = props.Scene
		}
		ibl := s.Environment.IBL
		if r.usesSelectiveRuntimeBootstrap() {
			if len(s.Models) > 0 || len(s.InstancedGLBMeshes) > 0 || (ibl.Radiance.URI != "" && ibl.Irradiance.URI != "" && ibl.BRDFLUT.URI != "") {
				add(r.bootstrapFeatureScene3dGLTFPath, "script")
			}
			animated := len(s.Animations) > 0
			for _, list := range [][]sceneprobe.Record{s.Models, s.InstancedGLBMeshes} {
				for _, model := range list {
					animated = animated || model.Animation != "" || model.AnimationSeq != ""
					for _, instance := range model.Instances {
						animated = animated || instance.Animation != ""
					}
				}
			}
			if animated {
				add(r.bootstrapFeatureScene3dAnimationPath, "script")
			}
		}
		if r.usesSelectiveRuntimeBootstrap() {
			for _, list := range [][]sceneprobe.Record{s.Objects, s.Models, s.InstancedGLBMeshes, s.InstancedMeshes, s.Points, s.Sprites} {
				for _, m := range list {
					for _, src := range m.Textures() {
						parsed, err := url.Parse(src)
						if err == nil && strings.HasSuffix(strings.ToLower(parsed.Path), ".ktx2") {
							add(r.bootstrapFeatureScene3dGLTFPath, "script")
						}
					}
				}
			}
		}

		parsedEnv, envErr := url.Parse(s.Environment.EnvMap)
		if r.usesSelectiveRuntimeBootstrap() && envErr == nil && strings.HasSuffix(strings.ToLower(parsedEnv.Path), ".ktx2") {
			add(r.bootstrapFeatureScene3dGLTFPath, "script")
		}

		// Preload one model per scene, preferring the initial progressive asset.
		models := s.Models
		if len(models) == 0 {
			models = s.InstancedGLBMeshes
		}
		if len(models) > 0 {
			model := models[0]
			src := model.Src
			if model.Progressive && model.PreviewSrc != "" && model.FullSrc != "" {
				src = model.PreviewSrc
			}
			add(src, "fetch")
		}
		// Bound texture hints to the first material in each visible node class,
		// plus the scene-wide lighting and first water system. Do not preload
		// full progressive models, animation payloads, or later materials.
		for _, list := range [][]sceneprobe.Record{s.Objects, s.Models, s.InstancedGLBMeshes, s.InstancedMeshes, s.Points, s.Sprites} {
			if len(list) == 0 {
				continue
			}
			m := list[0]
			for _, src := range m.Textures() {
				add(src, scenePreloadTextureDestination(src))
			}
		}
		if len(s.Sprites) > 0 {
			add(s.Sprites[0].Src, "image")
		}
		add(s.Environment.EnvMap, scenePreloadTextureDestination(s.Environment.EnvMap))
		if ibl.Radiance.URI != "" && ibl.Irradiance.URI != "" && ibl.BRDFLUT.URI != "" {
			for _, src := range []string{ibl.Radiance.URI, ibl.Irradiance.URI, ibl.BRDFLUT.URI} {
				add(src, "fetch")
			}
		}
		if len(s.WaterSystems) > 0 {
			water := s.WaterSystems[0]
			add(water.TileTexture, "image")
			base := strings.TrimSpace(water.CubeMap)
			if base != "" {
				for _, face := range []string{"xpos", "xneg", "ypos", "zpos", "zneg"} {
					src := strings.Replace(base, "{face}", face, 1)
					if !strings.Contains(base, "{face}") {
						src = strings.TrimRight(base, "/") + "/" + face + ".jpg"
					}
					add(src, "image")
				}
			}
		}
	}
}
