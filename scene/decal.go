package scene

import (
	"encoding/json"
	"math"
	"strings"

	"m31labs.dev/gosx/det"
)

// DecalRecordStride is the float32 count of one packed decal record. Static
// ProjectedDecals lowered by the Decals post effect and DecalPool.Records use the same
// layout, so the browser has one decoder:
//
//	0..2   position (world space)
//	3      opacity, 0..1 (pool fade-out already applied)
//	4..7   orientation quaternion x, y, z, w; local +Z faces away from the surface
//	8..10  box size: width (local X), height (local Y), projection depth (local Z)
//	11     atlas frame index
//	12..15 tint r, g, b, a (normalized)
//	16     layer; lower layers draw first, so higher layers land on top
//	17     sequence; older decals draw first within a layer
//	18     material index into Decals.Materials
//	19     reserved, always 0
const DecalRecordStride = 20

// Default decal projection and fade values the browser also applies.
const (
	DefaultDecalAngleFadeStart = 65.0 // degrees: fully opaque up to this surface angle
	DefaultDecalAngleFadeEnd   = 88.0 // degrees: fully faded at this surface angle
	DefaultDecalEdgeFade       = 0.1  // fraction of the box that fades at each edge
	DefaultDecalRoughness      = 0.8
	DefaultDecalMaxDistance    = 120.0 // world units from the camera; farther decals are skipped
	maxDecalAtlasCells         = 1024
)

// DecalMaterial is the surface a decal paints. Textures are image URLs. The
// albedo texture is an atlas of Columns x Rows equal cells; a decal picks a
// cell with Frame. The optional normal, roughness and emissive textures use the
// same atlas layout. The albedo alpha channel is the decal coverage.
//
// A decal's color replaces the lit color under it and does not add light, so a
// decal on a lit surface looks lit by the scene's ambient and first directional
// light. Normal maps bend that lighting; roughness drives a sun highlight;
// emissive adds light that ignores the scene lights.
//
// Zero numeric fields take the documented defaults.
type DecalMaterial struct {
	Name         string `json:"name"`
	Albedo       string `json:"albedo,omitempty"`
	NormalMap    string `json:"normalMap,omitempty"`
	RoughnessMap string `json:"roughnessMap,omitempty"`
	EmissiveMap  string `json:"emissiveMap,omitempty"`
	// Columns and Rows describe the atlas grid; zero means 1.
	Columns int `json:"columns,omitempty"`
	Rows    int `json:"rows,omitempty"`
	// Color multiplies the albedo texture ("#rrggbb"; empty means white).
	Color string `json:"color,omitempty"`
	// Roughness scales the roughness map, or is the constant when there is no
	// map. Default 0.8.
	Roughness float64 `json:"roughness,omitempty"`
	// EmissiveColor and EmissiveIntensity add light. A zero intensity with an
	// emissive map means 1; a zero intensity without a map means no emission.
	EmissiveColor     string  `json:"emissiveColor,omitempty"`
	EmissiveIntensity float64 `json:"emissiveIntensity,omitempty"`
	// NormalStrength scales the normal map. Default 1.
	NormalStrength float64 `json:"normalStrength,omitempty"`
	// AngleFadeStart and AngleFadeEnd are degrees between the decal's facing
	// direction and the surface normal under it. Opaque up to the start, gone at
	// the end. Defaults 65 and 88.
	AngleFadeStart float64 `json:"angleFadeStart,omitempty"`
	AngleFadeEnd   float64 `json:"angleFadeEnd,omitempty"`
	// EdgeFade is the fraction of the box (0..0.5) that fades out at each edge
	// and at the front and back faces. Default 0.1. Use a tiny value such as
	// 0.001 for hard edges.
	EdgeFade float64 `json:"edgeFade,omitempty"`
	// Blend is "lit" (default) or "multiply". Lit decals paint their own color,
	// lit by the ambient and first directional light, and ignore shadows.
	// Multiply decals darken or tint the scene color already drawn, so shadows
	// and every light still show through; use them for scorch, dirt, blood and
	// bullet holes. Multiply ignores emissive.
	Blend string `json:"blend,omitempty"`
}

// ProjectedDecal is one box-projected decal. It is not the Decal node, which
// is a flat alpha-blended quad. The box is centered on Position. Its local
// +Z axis points along Normal, away from the surface, and the projection runs
// along local Z through Size.Z. Anything the box covers that faces the decal
// receives it, whatever the surface shape.
type ProjectedDecal struct {
	// Material names an entry of Decals.Materials.
	Material string
	Position Vector3
	// Normal is the direction the decal faces: normally the surface normal. The
	// zero vector means +Y.
	Normal Vector3
	// Rotation spins the decal about Normal, in radians.
	Rotation float64
	// Size is width (X), height (Y) and projection depth (Z) in world units.
	// A zero axis means 1.
	Size Vector3
	// Frame picks the atlas cell, row-major from the top left.
	Frame int
	// Opacity is 0..1; zero means 1.
	Opacity float64
	// Tint multiplies the material color ("#rrggbb"; empty means white).
	Tint string
	// Layer orders overlapping decals: higher layers land on top.
	Layer int
}

// Decals projects decals onto the scene geometry that the depth buffer holds.
// It is a post effect: it reads the scene depth, and it paints the HDR scene
// color. Place it before Tonemap, Bloom and any other effect that should see
// the decals. WebGPU and WebGL2 both run it; the browser loads its code only
// for scenes that use it.
//
// Depth does not hold surface normals, so the pass rebuilds them from depth.
// Nothing here writes normals or lighting into the G-buffer.
type Decals struct {
	Materials []DecalMaterial
	// Items are the fixed decals declared with the scene.
	Items []ProjectedDecal
	// Pools names DecalPool streams to draw. Pools fill from Go at run time.
	Pools []string
	// MaxDistance skips decals beyond this distance from the camera, in world
	// units. Default 120.
	MaxDistance float64
}

func (Decals) isPostEffect() {}

// DecalsIR is the wire form of Decals: materials with defaults applied and the
// fixed decals packed as DecalRecordStride-float records.
type DecalsIR struct {
	Materials   []DecalMaterial `json:"materials,omitempty"`
	Records     []float32       `json:"records,omitempty"`
	Pools       []string        `json:"pools,omitempty"`
	MaxDistance float64         `json:"maxDistance,omitempty"`
}

func (ir DecalsIR) MarshalJSON() ([]byte, error) {
	type wire DecalsIR
	return json.Marshal(struct {
		Kind string `json:"kind"`
		wire
	}{PostEffectKindDecals, wire(ir)})
}

func (ir DecalsIR) legacyProps() map[string]any {
	b, _ := ir.MarshalJSON()
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

// lower resolves defaults and packs Items. A decal that names no material is
// dropped because it has nothing to paint.
func (d Decals) lower() DecalsIR {
	ir := DecalsIR{Pools: append([]string(nil), d.Pools...), MaxDistance: clampAtmosphereParam(d.MaxDistance, 0, 100000)}
	if ir.MaxDistance == 0 {
		ir.MaxDistance = DefaultDecalMaxDistance
	}
	ir.Materials = make([]DecalMaterial, 0, len(d.Materials))
	index := make(map[string]int, len(d.Materials))
	for _, material := range d.Materials {
		material = material.resolved()
		if _, dup := index[material.Name]; dup {
			continue
		}
		index[material.Name] = len(ir.Materials)
		ir.Materials = append(ir.Materials, material)
	}
	seq := float32(0)
	for _, item := range d.Items {
		materialIndex, ok := index[strings.TrimSpace(item.Material)]
		if !ok {
			continue
		}
		record := item.Record(materialIndex, seq)
		ir.Records = append(ir.Records, record[:]...)
		seq++
	}
	return ir
}

// resolved returns the material with defaults filled and ranges clamped.
func (m DecalMaterial) resolved() DecalMaterial {
	m.Name = strings.TrimSpace(m.Name)
	m.Columns = clampDecalInt(m.Columns, 1, 32)
	m.Rows = clampDecalInt(m.Rows, 1, 32)
	if m.Columns*m.Rows > maxDecalAtlasCells {
		m.Rows = 1
		m.Columns = 1
	}
	if m.Roughness <= 0 {
		m.Roughness = DefaultDecalRoughness
	}
	m.Roughness = clampAtmosphereParam(m.Roughness, 0.02, 1)
	if m.EmissiveIntensity <= 0 && m.EmissiveMap != "" {
		m.EmissiveIntensity = 1
	}
	m.EmissiveIntensity = clampAtmosphereParam(m.EmissiveIntensity, 0, 64)
	if m.NormalStrength <= 0 {
		m.NormalStrength = 1
	}
	m.NormalStrength = clampAtmosphereParam(m.NormalStrength, 0, 4)
	if m.AngleFadeStart <= 0 {
		m.AngleFadeStart = DefaultDecalAngleFadeStart
	}
	if m.AngleFadeEnd <= 0 {
		m.AngleFadeEnd = DefaultDecalAngleFadeEnd
	}
	m.AngleFadeStart = clampAtmosphereParam(m.AngleFadeStart, 0, 89)
	m.AngleFadeEnd = clampAtmosphereParam(m.AngleFadeEnd, m.AngleFadeStart+0.5, 90)
	if m.EdgeFade <= 0 {
		m.EdgeFade = DefaultDecalEdgeFade
	}
	m.EdgeFade = clampAtmosphereParam(m.EdgeFade, 0.001, 0.5)
	m.Blend = strings.ToLower(strings.TrimSpace(m.Blend))
	if m.Blend != "multiply" {
		m.Blend = "lit"
	}
	return m
}

func clampDecalInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Record packs the decal for the browser. materialIndex indexes the owning
// Decals.Materials and seq orders decals of one layer (lower draws first).
func (d ProjectedDecal) Record(materialIndex int, seq float32) [DecalRecordStride]float32 {
	size := d.Size
	if size.X <= 0 {
		size.X = 1
	}
	if size.Y <= 0 {
		size.Y = 1
	}
	if size.Z <= 0 {
		size.Z = 1
	}
	opacity := d.Opacity
	if opacity <= 0 {
		opacity = 1
	}
	return packDecalRecord(d.Position, d.Normal, d.Rotation, size, d.Frame, clampAtmosphereParam(opacity, 0, 1), decalTint(d.Tint), d.Layer, seq, materialIndex)
}

// decalTint parses "#rrggbb" or "#rrggbbaa". Anything else is white.
func decalTint(s string) [4]float32 {
	out := [4]float32{1, 1, 1, 1}
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 && len(s) != 8 {
		return out
	}
	for i := 0; i < len(s)/2; i++ {
		hi, ok1 := hexNibble(s[2*i])
		lo, ok2 := hexNibble(s[2*i+1])
		if !ok1 || !ok2 {
			return [4]float32{1, 1, 1, 1}
		}
		out[i] = float32(hi*16+lo) / 255
	}
	return out
}

func hexNibble(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

// packDecalRecord is shared by ProjectedDecal.Record and DecalPool so both produce the
// same bytes. It does not allocate.
func packDecalRecord(pos, normal Vector3, spin float64, size Vector3, frame int, opacity float64, tint [4]float32, layer int, seq float32, material int) [DecalRecordStride]float32 {
	qx, qy, qz, qw := decalOrientation(normal, spin)
	if frame < 0 {
		frame = 0
	}
	return [DecalRecordStride]float32{
		float32(pos.X), float32(pos.Y), float32(pos.Z), float32(opacity),
		qx, qy, qz, qw,
		float32(size.X), float32(size.Y), float32(size.Z), float32(frame),
		tint[0], tint[1], tint[2], tint[3],
		float32(layer), seq, float32(material), 0,
	}
}

// decalOrientation returns the quaternion that turns local +Z onto normal and
// then spins the box about normal by spin radians.
func decalOrientation(normal Vector3, spin float64) (x, y, z, w float32) {
	nx, ny, nz := normal.X, normal.Y, normal.Z
	length := math.Sqrt(float64(nx*nx) + float64(ny*ny) + float64(nz*nz))
	if length < 1e-9 || math.IsNaN(length) || math.IsInf(length, 0) {
		nx, ny, nz, length = 0, 1, 0, 1
	}
	nx, ny, nz = nx/length, ny/length, nz/length
	// Shortest arc from (0,0,1) to n.
	var ax, ay, az, aw float64
	switch {
	case nz > 0.999999:
		ax, ay, az, aw = 0, 0, 0, 1
	case nz < -0.999999:
		ax, ay, az, aw = 1, 0, 0, 0 // half turn about X
	default:
		// cross((0,0,1), n) = (-ny, nx, 0); w = 1 + dot.
		ax, ay, az, aw = -ny, nx, 0, 1+nz
		l := math.Sqrt(float64(ax*ax) + float64(ay*ay) + float64(az*az) + float64(aw*aw))
		ax, ay, az, aw = ax/l, ay/l, az/l, aw/l
	}
	// Spin about the new local Z (post-multiply: q = arc * spinZ).
	s, c := det.Sin(spin/2), det.Cos(spin/2)
	// The spin quaternion is (0, 0, s, c), so the product reduces to:
	rx := float64(ax*c) + float64(ay*s)
	ry := float64(ay*c) - float64(ax*s)
	rz := float64(aw*s) + float64(az*c)
	rw := float64(aw*c) - float64(az*s)
	return float32(rx), float32(ry), float32(rz), float32(rw)
}
