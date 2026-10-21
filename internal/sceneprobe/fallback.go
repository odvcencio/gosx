package sceneprobe

import (
	"encoding/json"
	"strings"
)

// Malformed optional fields retain the independent reader behavior. The normal
// path decodes only Probe; this fallback runs only on a type error.
type legacyChunkRecord struct {
	Generator            json.RawMessage `json:"generator"`
	CompressedPositions  json.RawMessage `json:"compressedPositions"`
	CompressedSizes      json.RawMessage `json:"compressedSizes"`
	CompressedTransforms json.RawMessage `json:"compressedTransforms"`
	CompressedTimes      json.RawMessage `json:"compressedTimes"`
	CompressedValues     json.RawMessage `json:"compressedValues"`
	PreviewPositions     json.RawMessage `json:"previewPositions"`
	PreviewSizes         json.RawMessage `json:"previewSizes"`
	PreviewTransforms    json.RawMessage `json:"previewTransforms"`
	PreviewTimes         json.RawMessage `json:"previewTimes"`
	PreviewValues        json.RawMessage `json:"previewValues"`
}

func (rec legacyChunkRecord) needsDecompress() bool {
	return len(rec.Generator) > 0 ||
		len(rec.CompressedPositions) > 0 ||
		len(rec.CompressedSizes) > 0 ||
		len(rec.CompressedTransforms) > 0 ||
		len(rec.CompressedTimes) > 0 ||
		len(rec.CompressedValues) > 0 ||
		len(rec.PreviewPositions) > 0 ||
		len(rec.PreviewSizes) > 0 ||
		len(rec.PreviewTransforms) > 0 ||
		len(rec.PreviewTimes) > 0 ||
		len(rec.PreviewValues) > 0
}

type legacyChunkClip struct {
	Channels []legacyChunkRecord `json:"channels"`
}

// legacyChunkProbe reads the two shapes a Scene3D engine can carry: the wire
// IR under props.scene, and the flat props form the runtime also accepts.
type legacyChunkProbe struct {
	ParticleBursts   bool                `json:"particleBursts"`
	Scene            *legacyChunkProbe   `json:"scene"`
	Compression      json.RawMessage     `json:"compression"`
	ComputeParticles []json.RawMessage   `json:"computeParticles"`
	InstancedMeshes  []legacyChunkRecord `json:"instancedMeshes"`
	Points           []legacyChunkRecord `json:"points"`
	Animations       []legacyChunkClip   `json:"animations"`
}

// needsComputeChunk reports whether the scene reaches
// bootstrap-feature-scene3d-compute.js. Two paths do: a compute particle
// system, which both renderers build through createSceneParticleSystem, and an
// instanced mesh, which the WebGPU renderer culls on the GPU with a kernel from
// the same file.
//
// Be permissive on purpose. A URL the runtime never uses costs about sixty
// bytes of HTML; a missing URL drops a particle system the author asked for.
func (p *legacyChunkProbe) needsComputeChunk() bool {
	if p == nil {
		return false
	}
	if p.ParticleBursts || len(p.ComputeParticles) > 0 || len(p.InstancedMeshes) > 0 {
		return true
	}
	return p.Scene.needsComputeChunk()
}

// needsDecompressChunk reports whether the scene reaches
// bootstrap-feature-scene3d-decompress.js. A compression policy alone is
// enough: progressive and level-of-detail mode both drive the chunk after the
// first frame, even when the first payload arrives plain.
func (p *legacyChunkProbe) needsDecompressChunk() bool {
	if p == nil {
		return false
	}
	if len(p.Compression) > 0 && string(p.Compression) != "null" {
		return true
	}
	for i := range p.Points {
		if p.Points[i].needsDecompress() {
			return true
		}
	}
	for i := range p.InstancedMeshes {
		if p.InstancedMeshes[i].needsDecompress() {
			return true
		}
	}
	for i := range p.Animations {
		for j := range p.Animations[i].Channels {
			if p.Animations[i].Channels[j].needsDecompress() {
				return true
			}
		}
	}
	return p.Scene.needsDecompressChunk()
}

type legacyTexture struct{ URI string }

type legacyRecord struct {
	Src, PreviewSrc, FullSrc                                          string
	Progressive                                                       bool
	Animation, AnimationSeq                                           string
	Instances                                                         []struct{ Animation string }
	Texture, NormalMap, RoughnessMap, MetalnessMap                    string
	OcclusionMap, EmissiveMap, SpecularIntensityMap, SpecularColorMap string
	TileTexture, CubeMap                                              string
	TextureDescriptors                                                struct {
		BaseColor, Normal, Roughness, Metalness, Occlusion, Emissive legacyTexture
		SpecularIntensity, SpecularColor                             legacyTexture
	}
}

type legacyPreload struct {
	Scene                                  *legacyPreload
	ForceWebGL, RequireWebGL, PreferCanvas bool
	PreferWebGL                            *bool
	BackendCaps                            *struct{ Capable []string }
	Models                                 []legacyRecord
	InstancedGLBMeshes                     []legacyRecord
	Objects                                []legacyRecord
	InstancedMeshes                        []legacyRecord
	Points                                 []legacyRecord
	Sprites                                []legacyRecord
	WaterSystems                           []legacyRecord
	Animations                             []json.RawMessage
	Environment                            struct {
		EnvMap string
		IBL    struct{ Radiance, Irradiance, BRDFLUT legacyTexture }
	}
}

func decodeFallback(raw json.RawMessage, original *Probe) *Probe {
	var preload legacyPreload
	preloadErr := json.Unmarshal(raw, &preload)
	p := new(Probe)
	p.MaterialMotion = original.NeedsMaterialMotion()
	if preloadErr == nil {
		filtered, _ := json.Marshal(preload)
		_ = json.Unmarshal(filtered, p)
	}
	p.Err = original.Err
	p.PreloadErr = preloadErr
	var chunks legacyChunkProbe
	p.ChunkErr = json.Unmarshal(raw, &chunks)
	p.ComputeChunk = chunks.needsComputeChunk()
	p.DecompressChunk = chunks.needsDecompressChunk()
	if p.ChunkErr != nil {
		p.ComputeChunk = true
		p.DecompressChunk = true
	}
	p.WebGPU, p.WebGL = p.Backends()
	var poster struct {
		Poster                    string
		PosterWidth, PosterHeight float64
		PosterAlt                 string
	}
	p.PosterErr = json.Unmarshal(raw, &poster)
	if p.PosterErr == nil {
		p.Poster = strings.TrimSpace(poster.Poster)
		p.PosterWidth = poster.PosterWidth
		p.PosterHeight = poster.PosterHeight
		p.PosterAlt = poster.PosterAlt
	}
	var timeline struct{ Timelines bool }
	if json.Unmarshal(raw, &timeline) == nil {
		p.Timelines = timeline.Timelines
	}
	var bursts struct{ ParticleBursts bool }
	if json.Unmarshal(raw, &bursts) == nil {
		p.ParticleBursts = bursts.ParticleBursts
	}
	_ = json.Unmarshal(raw, &p.experienceFields)
	var zoom struct{ ControlZoom bool }
	if json.Unmarshal(raw, &zoom) == nil {
		p.ControlZoom = zoom.ControlZoom
	}
	var prep struct{ ModelPreparation bool }
	if json.Unmarshal(raw, &prep) == nil {
		p.ModelPreparation = prep.ModelPreparation
	}
	var walk struct{ Walk map[string]json.RawMessage }
	if json.Unmarshal(raw, &walk) == nil {
		p.Walk = walk.Walk
	}
	var vessel struct{ Vessel *struct{ NodeID string } }
	if json.Unmarshal(raw, &vessel) == nil {
		p.Vessel = vessel.Vessel
	}
	return p
}
