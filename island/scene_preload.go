package island

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"
)

// Decode only preload-relevant fields; geometry and shader payloads stay raw.
type scenePreloadTexture struct{ URI string }

type scenePreloadRecord struct {
	Src, PreviewSrc, FullSrc                                          string
	Progressive                                                       bool
	Animation, AnimationSeq                                           string
	Instances                                                         []struct{ Animation string }
	Texture, NormalMap, RoughnessMap, MetalnessMap                    string
	OcclusionMap, EmissiveMap, SpecularIntensityMap, SpecularColorMap string
	TileTexture, CubeMap                                              string
	TextureDescriptors                                                struct {
		BaseColor, Normal, Roughness, Metalness, Occlusion, Emissive scenePreloadTexture
		SpecularIntensity, SpecularColor                             scenePreloadTexture
	}
}

func (m scenePreloadRecord) textures() []string {
	descriptors := m.TextureDescriptors
	sources := []string{m.Texture, m.NormalMap, m.RoughnessMap, m.MetalnessMap, m.OcclusionMap, m.EmissiveMap, m.SpecularIntensityMap, m.SpecularColorMap}
	for i, descriptor := range []scenePreloadTexture{descriptors.BaseColor, descriptors.Normal, descriptors.Roughness, descriptors.Metalness, descriptors.Occlusion, descriptors.Emissive, descriptors.SpecularIntensity, descriptors.SpecularColor} {
		if strings.TrimSpace(descriptor.URI) != "" {
			sources[i] = descriptor.URI
		}
	}
	return sources
}

type scenePreloadProbe struct {
	Scene                                  *scenePreloadProbe
	ForceWebGL, RequireWebGL, PreferCanvas bool
	PreferWebGL                            *bool
	BackendCaps                            *struct{ Capable []string }
	Models                                 []scenePreloadRecord
	InstancedGLBMeshes                     []scenePreloadRecord
	Objects                                []scenePreloadRecord
	InstancedMeshes                        []scenePreloadRecord
	Points                                 []scenePreloadRecord
	Sprites                                []scenePreloadRecord
	WaterSystems                           []scenePreloadRecord
	Animations                             []json.RawMessage
	Environment                            struct {
		EnvMap string
		IBL    struct{ Radiance, Irradiance, BRDFLUT scenePreloadTexture }
	}
}

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
	for _, entry := range r.manifest.Engines {
		if !strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D") {
			continue
		}
		var props scenePreloadProbe
		if len(entry.Props) != 0 && json.Unmarshal(entry.Props, &props) != nil {
			return true
		}
		if gpu, _ := props.backends(); gpu {
			return true
		}
	}
	return false
}

func (p *scenePreloadProbe) backends() (webgpu, webgl bool) {
	webgpu, webgl = true, p.PreferWebGL == nil || *p.PreferWebGL
	if p.ForceWebGL || p.RequireWebGL {
		return false, true
	}
	if p.PreferCanvas {
		return false, false
	}
	caps := p.BackendCaps
	if p.Scene != nil && p.Scene.BackendCaps != nil {
		caps = p.Scene.BackendCaps
	}
	if caps != nil && caps.Capable != nil {
		gpuAllowed, glAllowed := false, false
		for _, backend := range caps.Capable {
			backend = strings.ToLower(backend)
			gpuAllowed = gpuAllowed || backend == "webgpu"
			glAllowed = glAllowed || backend == "webgl" || backend == "webgl2"
		}
		webgpu, webgl = webgpu && gpuAllowed, webgl && glAllowed
	}
	return
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
	for _, entry := range r.manifest.Engines {
		if !strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D") {
			continue
		}
		var props scenePreloadProbe
		if len(entry.Props) != 0 && json.Unmarshal(entry.Props, &props) != nil {
			continue
		}
		gpu, gl := props.backends()
		if r.usesSelectiveRuntimeBootstrap() {
			if entry.ProgramRef != "" {
				add(r.bootstrapFeatureScene3dCommandPath, "script")
			}
			if gpu {
				add(r.bootstrapFeatureScene3dWebGPUPath, "script")
			}
			if gl {
				add(r.bootstrapFeatureScene3dWebGLPath, "script")
			}
			var chunks scene3DChunkProbe
			if json.Unmarshal(entry.Props, &chunks) == nil {
				if chunks.needsComputeChunk() {
					add(r.bootstrapFeatureScene3dComputePath, "script")
				}
				if chunks.needsDecompressChunk() {
					add(r.bootstrapFeatureScene3dDecompressPath, "script")
				}
			}
		}
		s := &props
		if props.Scene != nil {
			s = props.Scene
		}
		ibl := s.Environment.IBL
		if r.usesSelectiveRuntimeBootstrap() {
			if len(s.Models) > 0 || len(s.InstancedGLBMeshes) > 0 || (ibl.Radiance.URI != "" && ibl.Irradiance.URI != "" && ibl.BRDFLUT.URI != "") {
				add(r.bootstrapFeatureScene3dGLTFPath, "script")
			}
			animated := len(s.Animations) > 0
			for _, list := range [][]scenePreloadRecord{s.Models, s.InstancedGLBMeshes} {
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
			for _, list := range [][]scenePreloadRecord{s.Objects, s.Models, s.InstancedGLBMeshes, s.InstancedMeshes, s.Points, s.Sprites} {
				for _, m := range list {
					for _, src := range m.textures() {
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
		for _, list := range [][]scenePreloadRecord{s.Objects, s.Models, s.InstancedGLBMeshes, s.InstancedMeshes, s.Points, s.Sprites} {
			if len(list) == 0 {
				continue
			}
			m := list[0]
			for _, src := range m.textures() {
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
