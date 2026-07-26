package bundle

import (
	"math"

	"m31labs.dev/gosx/scene/geom"
)

// This file is the GPU-side adapter over package scene/geom. It holds no
// generator of its own.
//
// A second copy of a generator is what produced the torusknot defect: the
// browser drew a knot, this file did not know the name, normalizePrimitiveKind
// returned the empty string, primitiveCacheKey returned the empty string,
// ensurePrimitive returned nil, and the draw disappeared with no diagnostic.
// The desktop renderer and the headless PNG oracle drew a different scene than
// the browser. Add a new kind in scene/geom, never here.

// primitiveGeometry is the CPU-side geometry for a named primitive Kind.
// positions, colors, and normals hold three floats per vertex; uvs hold two.
//
// The renderer keeps primitives non-indexed. That matches the current WebGPU
// vertex layout, lets flat and smooth normals live side by side without an
// index-split pass, and keeps every native primitive upload as four tightly
// packed vertex buffers: positions, colors, normals, and uvs.
type primitiveGeometry struct {
	positions   []float32
	colors      []float32
	normals     []float32
	uvs         []float32
	vertexCount int
}

// primitiveParams mirrors the authored numbers on engine.RenderInstancedMesh.
// It is the bundle-side spelling of geom.Params.
type primitiveParams struct {
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

func (p primitiveParams) geom() geom.Params {
	return geom.Params{
		Kind:            p.Kind,
		Size:            p.Size,
		Width:           p.Width,
		Height:          p.Height,
		Depth:           p.Depth,
		Radius:          p.Radius,
		RadiusTop:       p.RadiusTop,
		RadiusBottom:    p.RadiusBottom,
		Tube:            p.Tube,
		Segments:        p.Segments,
		RadialSegments:  p.RadialSegments,
		TubularSegments: p.TubularSegments,
	}
}

func primitiveParamsFromGeom(p geom.Params) primitiveParams {
	return primitiveParams{
		Kind:            p.Kind,
		Size:            p.Size,
		Width:           p.Width,
		Height:          p.Height,
		Depth:           p.Depth,
		Radius:          p.Radius,
		RadiusTop:       p.RadiusTop,
		RadiusBottom:    p.RadiusBottom,
		Tube:            p.Tube,
		Segments:        p.Segments,
		RadialSegments:  p.RadialSegments,
		TubularSegments: p.TubularSegments,
	}
}

// primitiveForKind returns native geometry for one Scene3D built-in mesh
// primitive kind, at that kind's default size. An unknown kind returns nil and
// the caller skips the draw.
func primitiveForKind(kind string) *primitiveGeometry {
	return primitiveForParams(primitiveParams{Kind: kind})
}

// primitiveForParams tessellates one primitive and narrows it to the float32
// buffers the vertex layout wants. An indexed mesh, such as the torus knot, is
// expanded to a flat triangle list first.
func primitiveForParams(params primitiveParams) *primitiveGeometry {
	mesh := geom.Build(params.geom(), geom.AllAttributes)
	if mesh == nil {
		return nil
	}
	return narrowMesh(mesh.Expanded())
}

// narrowMesh converts a geom.Mesh to the renderer's float32 buffers.
func narrowMesh(mesh *geom.Mesh) *primitiveGeometry {
	count := mesh.VertexCount()
	if count == 0 {
		return nil
	}
	return &primitiveGeometry{
		positions:   narrowFloats(mesh.Positions),
		colors:      narrowFloats(mesh.Colors),
		normals:     narrowFloats(mesh.Normals),
		uvs:         narrowFloats(mesh.UVs),
		vertexCount: count,
	}
}

func narrowFloats(src []float64) []float32 {
	if len(src) == 0 {
		return nil
	}
	out := make([]float32, len(src))
	for i, v := range src {
		out[i] = float32(v)
	}
	return out
}

func normalizePrimitiveParams(params primitiveParams) primitiveParams {
	return primitiveParamsFromGeom(geom.Normalize(params.geom()))
}

func normalizePrimitiveKind(kind string) string {
	return geom.NormalizeKind(kind)
}

func primitiveCacheKey(params primitiveParams) string {
	return geom.CacheKey(params.geom())
}

// sphereGeometry, cylinderGeometry and torusGeometry name the three curved
// bodies by their own parameters. They exist so a caller that already knows the
// shape does not have to fill a params struct.
func sphereGeometry(radius float64, longitudes, latitudes int) *primitiveGeometry {
	_ = latitudes // geom derives the latitude count from the longitude count.
	return primitiveForParams(primitiveParams{Kind: geom.KindSphere, Radius: radius, Segments: longitudes})
}

func cylinderGeometry(radiusTop, radiusBottom, height float64, segments int) *primitiveGeometry {
	if radiusTop <= 0 {
		return primitiveForParams(primitiveParams{
			Kind: geom.KindCone, RadiusBottom: radiusBottom, Height: height, Segments: segments,
		})
	}
	return primitiveForParams(primitiveParams{
		Kind: geom.KindCylinder, RadiusTop: radiusTop, RadiusBottom: radiusBottom, Height: height, Segments: segments,
	})
}

func torusGeometry(majorRadius, tubeRadius float64, radialSegments, tubularSegments int) *primitiveGeometry {
	return primitiveForParams(primitiveParams{
		Kind: geom.KindTorus, Radius: majorRadius, Tube: tubeRadius,
		RadialSegments: radialSegments, TubularSegments: tubularSegments,
	})
}

// instanceCullRadius scales a primitive's bounding radius by the scale baked
// into one instance transform. The renderer stores unscaled radii per
// primitive, so an instance scaled up 10x needs a radius 10x larger or the cull
// drops it while it is still on screen.
//
// The largest of the three column lengths is the conservative choice for
// non-uniform scale: it never under-estimates the sphere, so an instance is
// never wrongly culled. Skew from a sheared matrix inflates the radius, which
// is safe.
//
// cullWGSL runs the same calculation per thread on the GPU. Keep the two in
// step: this function is the CPU oracle the headless backend and the pick
// bounding test share.
func instanceCullRadius(baseRadius float32, model mat4) float32 {
	sx := columnLength(model[0], model[1], model[2])
	sy := columnLength(model[4], model[5], model[6])
	sz := columnLength(model[8], model[9], model[10])
	scale := sx
	if sy > scale {
		scale = sy
	}
	if sz > scale {
		scale = sz
	}
	if scale <= 0 {
		return baseRadius
	}
	return baseRadius * scale
}

func columnLength(x, y, z float32) float32 {
	return float32(math.Sqrt(float64(x*x + y*y + z*z)))
}

// primitiveCullRadiusMargin pads the tight bounding sphere. The pad covers the
// difference between the ideal surface and the chorded tessellation, plus the
// float32 rounding at upload.
const primitiveCullRadiusMargin = 1.05

// primitiveCullRadius returns the cull sphere of one primitive at unit scale.
// An unknown kind returns 2, which holds every default-sized built-in body, so
// an unrecognized draw is never culled by mistake.
func primitiveCullRadius(params primitiveParams) float32 {
	radius := geom.BoundingRadius(params.geom())
	if radius <= 0 {
		return 2
	}
	return float32(radius * primitiveCullRadiusMargin)
}

func triangleNormal(a, b, c [3]float32) [3]float32 {
	ab := [3]float32{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	ac := [3]float32{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
	return normalize3(
		ab[1]*ac[2]-ab[2]*ac[1],
		ab[2]*ac[0]-ab[0]*ac[2],
		ab[0]*ac[1]-ab[1]*ac[0],
	)
}

func normalize3(x, y, z float32) [3]float32 {
	length := math.Sqrt(float64(x*x + y*y + z*z))
	if length <= 0 || math.IsNaN(length) || math.IsInf(length, 0) {
		return [3]float32{0, 1, 0}
	}
	inv := float32(1 / length)
	return [3]float32{x * inv, y * inv, z * inv}
}
