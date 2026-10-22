// Package sceneprobe reads the server decisions from a Scene3D payload once.
package sceneprobe

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

type Texture struct{ URI string }

func (m Record) Textures() []string {
	sources := []string{m.Texture, m.NormalMap, m.RoughnessMap, m.MetalnessMap, m.OcclusionMap, m.EmissiveMap, m.SpecularIntensityMap, m.SpecularColorMap}
	for i, name := range []string{"baseColor", "normal", "roughness", "metalness", "occlusion", "emissive", "specularIntensity", "specularColor"} {
		descriptor, ok := m.TextureDescriptors[name]
		if !ok {
			for key, value := range m.TextureDescriptors {
				if strings.EqualFold(key, name) {
					descriptor = value
					break
				}
			}
		}
		if strings.TrimSpace(descriptor.URI) != "" {
			sources[i] = descriptor.URI
		}
	}
	return sources
}

type Record struct {
	ID   any
	Kind string

	Src, PreviewSrc, FullSrc                                          string
	Progressive                                                       bool
	Animation, AnimationSeq                                           string
	Instances                                                         []struct{ Animation string }
	Texture, NormalMap, RoughnessMap, MetalnessMap                    string
	OcclusionMap, EmissiveMap, SpecularIntensityMap, SpecularColorMap string
	TileTexture, CubeMap                                              string
	TextureDescriptors                                                map[string]Texture

	Generator            Presence `json:"generator"`
	CompressedPositions  Presence `json:"compressedPositions"`
	CompressedSizes      Presence `json:"compressedSizes"`
	CompressedTransforms Presence `json:"compressedTransforms"`
	CompressedTimes      Presence `json:"compressedTimes"`
	CompressedValues     Presence `json:"compressedValues"`
	PreviewPositions     Presence `json:"previewPositions"`
	PreviewSizes         Presence `json:"previewSizes"`
	PreviewTransforms    Presence `json:"previewTransforms"`
	PreviewTimes         Presence `json:"previewTimes"`
	PreviewValues        Presence `json:"previewValues"`
}

func (rec Record) NeedsDecompress() bool {
	return rec.Generator.Present ||
		rec.CompressedPositions.Present ||
		rec.CompressedSizes.Present ||
		rec.CompressedTransforms.Present ||
		rec.CompressedTimes.Present ||
		rec.CompressedValues.Present ||
		rec.PreviewPositions.Present ||
		rec.PreviewSizes.Present ||
		rec.PreviewTransforms.Present ||
		rec.PreviewTimes.Present ||
		rec.PreviewValues.Present
}

type Clip struct {
	Channels []Record `json:"channels"`
}

// Probe reads the two shapes a Scene3D engine can carry: the wire
// IR under props.scene, and the flat props form the runtime also accepts.
// NeedsCompute reports whether the scene reaches
// bootstrap-feature-scene3d-compute.js. Two paths do: a compute particle
// system, which both renderers build through createSceneParticleSystem, and an
// instanced mesh, which the WebGPU renderer culls on the GPU with a kernel from
// the same file.
//
// Be permissive on purpose. A URL the runtime never uses costs about sixty
// bytes of HTML; a missing URL drops a particle system the author asked for.
func (p *Probe) NeedsCompute() bool {
	if p == nil {
		return false
	}
	if p.ParticleBursts || len(p.ComputeParticles) > 0 || len(p.InstancedMeshes) > 0 {
		return true
	}
	return p.Scene.NeedsCompute()
}

// NeedsDecompress reports whether the scene reaches
// bootstrap-feature-scene3d-decompress.js. A compression policy alone is
// enough: progressive and level-of-detail mode both drive the chunk after the
// first frame, even when the first payload arrives plain.
func (p *Probe) NeedsDecompress() bool {
	if p == nil {
		return false
	}
	if p.Compression.NonNull {
		return true
	}
	for i := range p.Points {
		if p.Points[i].NeedsDecompress() {
			return true
		}
	}
	for i := range p.InstancedMeshes {
		if p.InstancedMeshes[i].NeedsDecompress() {
			return true
		}
	}
	for i := range p.Animations {
		for j := range p.Animations[i].Channels {
			if p.Animations[i].Channels[j].NeedsDecompress() {
				return true
			}
		}
	}
	return p.Scene.NeedsDecompress()
}

type experienceFields struct {
	ExperienceRef, MetricsSignal                string
	Optics, Presentation, DayCycle, Exploration json.RawMessage
}

func (p *Probe) HasOptics() bool { return p != nil && len(p.Optics) > 0 && string(p.Optics) != "null" }
func (p *Probe) HasExperience() bool {
	if p == nil {
		return false
	}
	if strings.TrimSpace(p.ExperienceRef) != "" || strings.TrimSpace(p.MetricsSignal) != "" || p.HasOptics() {
		return true
	}
	for _, value := range []json.RawMessage{p.Presentation, p.DayCycle, p.Exploration} {
		if len(value) > 0 && string(value) != "null" {
			return true
		}
	}
	return false
}

type Probe struct {
	experienceFields
	ComputeChunk, DecompressChunk, WebGPU, WebGL bool            `json:"-"`
	MaterialMotion                               bool            `json:"-"`
	MaterialMotionProgram                        json.RawMessage `json:"materialMotionProgram,omitempty"`

	Controls string

	Scene                                                                                                        *Probe
	ParticleBursts, Timelines, ControlZoom, ModelPreparation                                                     bool
	ForceWebGL, RequireWebGL, PreferCanvas                                                                       bool
	PreferWebGL                                                                                                  *bool
	BackendCaps                                                                                                  *struct{ Capable []string }
	Compression                                                                                                  Presence
	ComputeParticles, InstancedMeshes, Points, Models, InstancedGLBMeshes, Objects, Sprites, WaterSystems, Nodes []Record
	Animations                                                                                                   []Clip
	Environment                                                                                                  struct {
		EnvMap string
		IBL    struct{ Radiance, Irradiance, BRDFLUT Texture }
	}
	Walk                      map[string]json.RawMessage
	Vessel                    *struct{ NodeID string }
	Poster                    string
	PosterWidth, PosterHeight float64
	PosterAlt                 string
	// Defer is the author's deferred-mount request; nil when absent.
	Defer *DeferProbe
	// DeferVariants are media-query alternatives of a deferred scene's props.
	DeferVariants                   []DeferVariantProbe
	Err                             error `json:"-"`
	ChunkErr, PreloadErr, PosterErr error `json:"-"`
}

// NeedsMaterialMotion identifies declared native material programs in either
// accepted props shape. Other optional-field errors do not erase this decision.
func (p *Probe) NeedsMaterialMotion() bool {
	if p == nil {
		return false
	}
	var program string
	return p.MaterialMotion ||
		len(p.MaterialMotionProgram) > 0 && json.Unmarshal(p.MaterialMotionProgram, &program) == nil && strings.TrimSpace(program) != "" ||
		p.Scene.NeedsMaterialMotion()
}

func Decode(raw json.RawMessage) *Probe {
	p := new(Probe)
	if len(raw) > 0 {
		p.Err = json.Unmarshal(raw, p)
	}
	if p.Err != nil {
		return decodeFallback(raw, p)
	}
	p.Poster = strings.TrimSpace(p.Poster)
	p.ComputeChunk = p.NeedsCompute()
	p.DecompressChunk = p.NeedsDecompress()
	p.WebGPU, p.WebGL = p.Backends()
	return p
}

func (p *Probe) Backends() (webgpu, webgl bool) {
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

// TruthyJSON applies JavaScript truthiness without decoding arrays or objects.
func TruthyJSON(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("false")) || bytes.Equal(raw, []byte(`""`)) {
		return false
	}
	if raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9' {
		n, err := strconv.ParseFloat(string(raw), 64)
		return err == nil && n != 0
	}
	return true
}

func (r Record) PerfNeedsDecompress() bool {
	for _, raw := range []Presence{r.Generator, r.CompressedPositions, r.CompressedSizes, r.CompressedTransforms, r.CompressedTimes, r.CompressedValues, r.PreviewPositions, r.PreviewSizes, r.PreviewTransforms, r.PreviewTimes, r.PreviewValues} {
		if raw.Truthy {
			return true
		}
	}
	return false
}

// Presence records gate decisions without copying large compressed arrays.
type Presence struct{ Present, NonNull, Truthy bool }

func (p *Presence) UnmarshalJSON(raw []byte) error {
	p.Present = true
	p.NonNull = !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	p.Truthy = TruthyJSON(raw)
	return nil
}

// DeferProbe mirrors scene.Defer without importing the scene package.
type DeferProbe struct {
	Until     string `json:"until"`
	TimeoutMS int    `json:"timeoutMs"`
	DelayMS   int    `json:"delayMs"`
	Inline    bool   `json:"inline"`
}

// DeferVariantProbe is one media-query alternative; Props is the variant's
// complete props JSON.
type DeferVariantProbe struct {
	Media string
	Props json.RawMessage
}
