// Package geom generates the triangles of every built-in GoSX geometry.
//
// One generator serves three consumers:
//
//   - The exact raycaster in package scene, which tests the drawn triangles.
//   - The native renderer in package render/bundle, which uploads them.
//   - The headless PNG oracle, which draws through the native renderer.
//
// The package holds no dependency on package scene and no dependency on the
// renderer, so both sides import it without a cycle. Keep it that way. A second
// copy of a generator makes the browser, the desktop renderer and the picker
// disagree, and no test can see the difference.
//
// All arithmetic runs in float64. The renderer narrows to float32 at upload
// time, and the raycaster keeps the full precision.
package geom

import (
	"math"
	"strconv"
	"strings"
)

// Attribute selects the vertex streams a build fills. Positions are always
// filled. A consumer that reads only positions, such as the raycaster, asks for
// nothing else and pays for nothing else.
type Attribute uint8

const (
	// AttrNormals fills Mesh.Normals with one unit vector per vertex.
	AttrNormals Attribute = 1 << iota
	// AttrUVs fills Mesh.UVs with one texture coordinate pair per vertex.
	AttrUVs
	// AttrColors fills Mesh.Colors with one RGB triple per vertex.
	AttrColors
)

// AllAttributes fills every stream. The renderer asks for this set.
const AllAttributes = AttrNormals | AttrUVs | AttrColors

// PositionsOnly fills positions and indices only. The raycaster asks for this
// set, because a triangle test reads no other stream.
const PositionsOnly Attribute = 0

// Mesh is a triangle mesh in flat buffers. Positions, Normals and Colors hold
// three numbers per vertex. UVs holds two. Indices holds three vertex numbers
// per triangle; an empty Indices means the positions already run as a triangle
// list.
//
// A stream the caller did not request stays nil.
type Mesh struct {
	Positions []float64
	Normals   []float64
	UVs       []float64
	Colors    []float64
	Indices   []int
}

// VertexCount returns the number of vertices the mesh holds.
func (m *Mesh) VertexCount() int {
	if m == nil {
		return 0
	}
	return len(m.Positions) / 3
}

// TriangleCount returns the number of triangles the mesh draws.
func (m *Mesh) TriangleCount() int {
	if m == nil {
		return 0
	}
	if len(m.Indices) > 0 {
		return len(m.Indices) / 3
	}
	return len(m.Positions) / 9
}

// Expanded returns a non-indexed copy of the mesh. An already non-indexed mesh
// comes back unchanged. The native renderer draws non-indexed buffers, so it
// calls this before upload.
func (m *Mesh) Expanded() *Mesh {
	if m == nil {
		return nil
	}
	if len(m.Indices) == 0 {
		return m
	}
	out := &Mesh{}
	out.Positions = expandAttr(m.Positions, m.Indices, 3)
	out.Normals = expandAttr(m.Normals, m.Indices, 3)
	out.UVs = expandAttr(m.UVs, m.Indices, 2)
	out.Colors = expandAttr(m.Colors, m.Indices, 3)
	return out
}

// expandAttr repeats one indexed attribute per index. An index that reaches
// past the source is skipped, which matches how the lowerer treats malformed
// index data.
func expandAttr(src []float64, indices []int, stride int) []float64 {
	if len(src) == 0 || stride <= 0 {
		return nil
	}
	out := make([]float64, 0, len(indices)*stride)
	for _, index := range indices {
		base := index * stride
		if index < 0 || base+stride > len(src) {
			continue
		}
		out = append(out, src[base:base+stride]...)
	}
	return out
}

// Params names one parametric geometry. Kind selects the generator; the other
// fields carry the authored numbers. Normalize resolves the defaults and the
// limits, so a caller never has to.
type Params struct {
	Kind            string
	Size            float64
	Width           float64
	Height          float64
	Depth           float64
	Radius          float64
	RadiusTop       float64
	RadiusBottom    float64
	Tube            float64
	Segments        int
	RadialSegments  int
	TubularSegments int
}

// Kind lists every generator this package answers for. Compare against these
// constants instead of writing the strings again.
const (
	KindCube      = "cube"
	KindBox       = "box"
	KindPlane     = "plane"
	KindPyramid   = "pyramid"
	KindSphere    = "sphere"
	KindCylinder  = "cylinder"
	KindCone      = "cone"
	KindTorus     = "torus"
	KindTorusKnot = "torusknot"
)

// NormalizeKind maps every authored spelling of a geometry name onto one
// canonical name. The public scene package uses Go type names such as
// BoxGeometry; the browser bridge uses lowercase names such as "box". An
// unknown name returns the empty string, and the caller must then treat the
// geometry as one this package cannot build.
func NormalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "cube", "cubegeometry":
		return KindCube
	case "box", "boxgeometry":
		return KindBox
	case "plane", "planegeometry", "quad", "quadgeometry":
		return KindPlane
	case "pyramid", "pyramidgeometry":
		return KindPyramid
	case "sphere", "spheregeometry", "uvsphere", "uvspheregeometry":
		return KindSphere
	case "cylinder", "cylindergeometry":
		return KindCylinder
	case "cone", "conegeometry":
		return KindCone
	case "torus", "torusgeometry":
		return KindTorus
	case "torusknot", "torusknotgeometry", "torus-knot", "torusknotgeo":
		return KindTorusKnot
	}
	return ""
}

// Normalize resolves the kind name, the defaults and the segment limits. Call
// it before CacheKey, BoundingRadius or Build; each of those calls it again, so
// a second call is safe and changes nothing.
//
// The torus knot defaults match torusKnotTriangleMesh in the browser runtime.
// The other defaults match the renderer's own unit envelope.
func Normalize(params Params) Params {
	params.Kind = NormalizeKind(params.Kind)
	switch params.Kind {
	case KindCube:
		params.Size = PositiveOr(params.Size, 2)
	case KindBox:
		params.Width = PositiveOr(params.Width, 2)
		params.Height = PositiveOr(params.Height, 2)
		params.Depth = PositiveOr(params.Depth, 2)
	case KindPlane:
		params.Width = PositiveOr(params.Width, 2)
		params.Height = PositiveOr(FirstPositive(params.Height, params.Depth), 2)
	case KindPyramid:
		params.Width = PositiveOr(params.Width, 2)
		params.Height = PositiveOr(params.Height, 2)
		params.Depth = PositiveOr(params.Depth, 2)
	case KindSphere:
		params.Radius = PositiveOr(params.Radius, 1)
		params.Segments = ClampInt(params.Segments, 32, 3, 256)
	case KindCylinder:
		params.RadiusTop = PositiveOr(params.RadiusTop, 1)
		params.RadiusBottom = PositiveOr(params.RadiusBottom, 1)
		params.Height = PositiveOr(params.Height, 2)
		params.Segments = ClampInt(params.Segments, 32, 3, 256)
	case KindCone:
		params.RadiusBottom = PositiveOr(FirstPositive(params.RadiusBottom, params.Radius), 1)
		params.Height = PositiveOr(params.Height, 2)
		params.Segments = ClampInt(params.Segments, 32, 3, 256)
	case KindTorus:
		params.Radius = PositiveOr(params.Radius, 0.70)
		params.Tube = PositiveOr(params.Tube, 0.30)
		params.RadialSegments = ClampInt(params.RadialSegments, 32, 3, 256)
		params.TubularSegments = ClampInt(params.TubularSegments, 16, 3, 128)
	case KindTorusKnot:
		params.Radius = PositiveOr(params.Radius, 0.17)
		params.Tube = PositiveOr(params.Tube, 0.045)
		params.RadialSegments = ClampInt(params.RadialSegments, 16, 3, 64)
		params.TubularSegments = ClampInt(params.TubularSegments, 128, 8, 512)
	}
	return params
}

// CacheKey names one tessellation. Two parameter sets with the same key hold
// the same triangles, so a renderer or a picker can share one build between
// them. An unknown kind returns the empty string.
func CacheKey(params Params) string {
	params = Normalize(params)
	if params.Kind == "" {
		return ""
	}
	parts := []string{params.Kind}
	appendFloat := func(name string, value float64) {
		if value > 0 {
			parts = append(parts, name+"="+strconv.FormatFloat(value, 'g', -1, 64))
		}
	}
	appendInt := func(name string, value int) {
		if value > 0 {
			parts = append(parts, name+"="+strconv.Itoa(value))
		}
	}
	switch params.Kind {
	case KindCube:
		appendFloat("size", params.Size)
	case KindBox, KindPyramid:
		appendFloat("w", params.Width)
		appendFloat("h", params.Height)
		appendFloat("d", params.Depth)
	case KindPlane:
		appendFloat("w", params.Width)
		appendFloat("h", params.Height)
	case KindSphere:
		appendFloat("r", params.Radius)
		appendInt("seg", params.Segments)
	case KindCylinder:
		appendFloat("rt", params.RadiusTop)
		appendFloat("rb", params.RadiusBottom)
		appendFloat("h", params.Height)
		appendInt("seg", params.Segments)
	case KindCone:
		appendFloat("rb", params.RadiusBottom)
		appendFloat("h", params.Height)
		appendInt("seg", params.Segments)
	case KindTorus, KindTorusKnot:
		appendFloat("r", params.Radius)
		appendFloat("tube", params.Tube)
		appendInt("rad", params.RadialSegments)
		appendInt("tubeSeg", params.TubularSegments)
	}
	return strings.Join(parts, "|")
}

// BoundingRadius returns the radius of a sphere at the origin that holds the
// whole geometry. The value never understates the geometry, so a cull or a
// broad phase never drops a real hit. An unknown kind returns zero.
func BoundingRadius(params Params) float64 {
	params = Normalize(params)
	switch params.Kind {
	case KindCube:
		return math.Sqrt(3*params.Size*params.Size) * 0.5
	case KindBox, KindPyramid:
		return math.Sqrt(params.Width*params.Width+params.Height*params.Height+params.Depth*params.Depth) * 0.5
	case KindPlane:
		return math.Sqrt(params.Width*params.Width+params.Height*params.Height) * 0.5
	case KindSphere:
		return params.Radius
	case KindCylinder, KindCone:
		r := math.Max(params.RadiusTop, params.RadiusBottom)
		return math.Hypot(r, params.Height*0.5)
	case KindTorus:
		return params.Radius + params.Tube
	case KindTorusKnot:
		// The center curve reaches 1.5*radius from the origin, at the turn where
		// it leaves the Z axis, so the tube adds its whole radius.
		return params.Radius*1.5 + params.Tube
	}
	return 0
}

// Build tessellates one parametric geometry. want selects the vertex streams to
// fill. An unknown kind returns nil, and the caller must then report that it
// cannot draw the geometry instead of dropping the draw without a word.
func Build(params Params, want Attribute) *Mesh {
	params = Normalize(params)
	switch params.Kind {
	case KindCube:
		return buildBox(params.Size, params.Size, params.Size, want)
	case KindBox:
		return buildBox(params.Width, params.Height, params.Depth, want)
	case KindPlane:
		return buildPlane(params.Width, params.Height, want)
	case KindPyramid:
		return buildPyramid(params.Width, params.Height, params.Depth, want)
	case KindSphere:
		return buildSphere(params.Radius, params.Segments, maxInt(2, params.Segments/2), want)
	case KindCylinder:
		return buildCylinder(params.RadiusTop, params.RadiusBottom, params.Height, params.Segments, want)
	case KindCone:
		return buildCylinder(0, params.RadiusBottom, params.Height, params.Segments, want)
	case KindTorus:
		return buildTorus(params.Radius, params.Tube, params.RadialSegments, params.TubularSegments, want)
	case KindTorusKnot:
		return buildTorusKnot(params.Radius, params.Tube, params.RadialSegments, params.TubularSegments, want)
	}
	return nil
}

// DrawVertexCount returns how many vertices a renderer uploads and a wire
// payload ships, without building.
//
// This is the count after Expanded runs, so an indexed body such as the torus
// knot reports three vertices per triangle, not its smaller shared-vertex count.
// Memory reporting and size budgets read this number. An unknown kind returns
// zero.
func DrawVertexCount(params Params) int {
	params = Normalize(params)
	if params.Kind == KindTorusKnot {
		return params.RadialSegments * params.TubularSegments * 6
	}
	return VertexCount(params)
}

// VertexCount returns how many vertices Build emits, without building. An
// indexed body reports its shared-vertex count; call DrawVertexCount for the
// count a GPU upload or a wire payload carries. An unknown kind returns zero.
func VertexCount(params Params) int {
	params = Normalize(params)
	switch params.Kind {
	case KindCube, KindBox:
		return 36
	case KindPlane:
		return 6
	case KindPyramid:
		return 18
	case KindSphere:
		return params.Segments * maxInt(2, params.Segments/2) * 6
	case KindCylinder:
		return params.Segments * 12
	case KindCone:
		return params.Segments * 6
	case KindTorus:
		return params.RadialSegments * params.TubularSegments * 6
	case KindTorusKnot:
		// The knot grid keeps the wrap row and column separate.
		return (params.TubularSegments + 1) * (params.RadialSegments + 1)
	}
	return 0
}

// PositiveOr returns value when it is a positive finite number, and fallback
// otherwise.
func PositiveOr(value, fallback float64) float64 {
	if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
		return value
	}
	return fallback
}

// FirstPositive returns the first positive finite value, or zero when there is
// none.
func FirstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value
		}
	}
	return 0
}

// ClampInt resolves an authored count. A value at or below zero selects the
// fallback, and the result stays inside [minValue, maxValue].
func ClampInt(value, fallback, minValue, maxValue int) int {
	if value <= 0 {
		value = fallback
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
