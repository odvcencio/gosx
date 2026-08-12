package scene

import "m31labs.dev/gosx/scene/geom"

// This file holds the mesh generators that produce a BufferGeometry instead of a
// named primitive kind.
//
// A generator here costs zero browser bytes. A Mesh with a BufferGeometry lowers
// to a "gltf-mesh" scene object that carries its vertices inline, and
// 10-runtime-scene-core.ts skips its own generation when an object already has
// vertices. So the runtime bundle needs no code for any shape in this file.
// Exact raycasting already works too, through the mesh-triangle path.
//
// The trade is wire bytes. A parametric kind ships about ten numbers; a
// generated mesh ships every vertex. Prefer a named kind for a shape that
// already has one, and reach for a generator when the shape has none.
//
// PolygonGeometry in earcut_geometry.go is the older member of this family.

// bufferFromMesh converts a generated mesh into a BufferGeometry. It returns a
// zero-value BufferGeometry for a nil or empty mesh, which lowers to an object
// with no vertices.
func bufferFromMesh(mesh *geom.Mesh) BufferGeometry {
	if mesh == nil || mesh.VertexCount() == 0 {
		return BufferGeometry{}
	}
	return BufferGeometry{
		Positions: mesh.Positions,
		Normals:   mesh.Normals,
		UVs:       mesh.UVs,
		Indices:   mesh.Indices,
	}
}

// generatorAttributes names the streams a wire-bound mesh carries. Colors stay
// out: a BufferGeometry has no color field, and the material decides the color.
const generatorAttributes = geom.AttrNormals | geom.AttrUVs

// PolyhedronGeometry builds a mesh from a base hull of triangles, pushed onto
// the sphere of the given radius.
//
// vertices holds the hull points as flat xyz triples. indices holds three point
// numbers per hull face, wound counter-clockwise as seen from outside. detail is
// the number of times each edge is split; zero keeps the hull faces. The result
// is capped at a detail of 5.
//
// A detail of zero gives flat normals, so the solid reads as facets. A detail
// above zero gives smooth normals, because every point sits on the sphere.
//
// The four named solids below all call this function. Call it directly to build
// a solid it does not name.
func PolyhedronGeometry(vertices []float64, indices []int, radius float64, detail int) BufferGeometry {
	return bufferFromMesh(geom.Polyhedron(vertices, indices, radius, detail, generatorAttributes))
}

// TetrahedronGeometry builds a regular tetrahedron on the sphere of the given
// radius. A detail above zero subdivides it toward a sphere.
func TetrahedronGeometry(radius float64, detail int) BufferGeometry {
	return bufferFromMesh(geom.Tetrahedron(radius, detail, generatorAttributes))
}

// OctahedronGeometry builds a regular octahedron on the sphere of the given
// radius. A detail above zero subdivides it toward a sphere.
func OctahedronGeometry(radius float64, detail int) BufferGeometry {
	return bufferFromMesh(geom.Octahedron(radius, detail, generatorAttributes))
}

// IcosahedronGeometry builds a regular icosahedron on the sphere of the given
// radius. A detail above zero subdivides it toward a sphere; this is the usual
// way to build a geodesic sphere.
func IcosahedronGeometry(radius float64, detail int) BufferGeometry {
	return bufferFromMesh(geom.Icosahedron(radius, detail, generatorAttributes))
}

// DodecahedronGeometry builds a regular dodecahedron on the sphere of the given
// radius. Each of the twelve pentagon faces ships as three triangles.
func DodecahedronGeometry(radius float64, detail int) BufferGeometry {
	return bufferFromMesh(geom.Dodecahedron(radius, detail, generatorAttributes))
}

// CircleGeometry builds a filled disc in the XZ plane at y=0 with a +Y normal,
// which matches PlaneGeometry and PolygonGeometry.
//
// segments is the number of rim steps; it falls back to 32 and stays inside
// [3, 512]. thetaStart is the angle of the first rim point, measured from +X
// toward +Z. thetaLength is the swept angle; a value at or below zero means a
// full turn, so CircleGeometry(1, 32, 0, 0) is a whole disc.
func CircleGeometry(radius float64, segments int, thetaStart, thetaLength float64) BufferGeometry {
	return bufferFromMesh(geom.Circle(radius, segments, thetaStart, thetaLength, generatorAttributes))
}

// RingGeometry builds an annulus in the XZ plane at y=0 with a +Y normal.
//
// thetaSegments is the number of steps around the ring and falls back to 32.
// phiSegments is the number of bands across it and falls back to 1. thetaStart
// and thetaLength name the sweep, as they do for CircleGeometry.
//
// An inner radius at or above the outer radius returns a zero-value
// BufferGeometry, because such a ring has no area.
func RingGeometry(innerRadius, outerRadius float64, thetaSegments, phiSegments int, thetaStart, thetaLength float64) BufferGeometry {
	return bufferFromMesh(geom.Ring(innerRadius, outerRadius, thetaSegments, phiSegments, thetaStart, thetaLength, generatorAttributes))
}

// Shape is a closed 2D outline with optional holes, in the XZ plane. Outline
// holds the outer ring as flat pairs (x0, z0, x1, z1, ...). Each ring in Holes
// is cut out of the outline and takes the same flat form. A ring with a single
// point acts as a Steiner point, which is what package scene/earcut does.
type Shape struct {
	Outline []float64
	Holes   [][]float64
}

func (s Shape) contour() geom.Contour {
	return geom.Contour{Outline: s.Outline, Holes: s.Holes}
}

// ShapeGeometry triangulates a shape into a flat cap lying in the XZ plane at
// the given elevation, with a +Y normal and UVs that span the shape's own
// bounding box.
//
// This is PolygonGeometry with texture coordinates. Use PolygonGeometry when the
// UVs are not wanted.
func ShapeGeometry(shape Shape, y float64) BufferGeometry {
	return bufferFromMesh(geom.Shape(shape.contour(), y, generatorAttributes))
}

// ExtrudeOptions names the solid ExtrudeGeometry builds. Depth is the height
// along +Y. Bevel turns on a rounded lip at both caps; BevelThickness is how far
// the lip reaches along Y, BevelSize is how far it pulls in from the outline,
// and BevelSegments is the number of steps in the lip.
//
// Every zero field takes a default: Depth 1, BevelThickness a tenth of the
// depth, BevelSize the bevel thickness, and BevelSegments 3.
type ExtrudeOptions struct {
	Depth          float64
	Bevel          bool
	BevelThickness float64
	BevelSize      float64
	BevelSegments  int
}

func (o ExtrudeOptions) options() geom.ExtrudeOptions {
	return geom.ExtrudeOptions{
		Depth:          o.Depth,
		Bevel:          o.Bevel,
		BevelThickness: o.BevelThickness,
		BevelSize:      o.BevelSize,
		BevelSegments:  o.BevelSegments,
	}
}

// ExtrudeGeometry sweeps a shape along +Y into a solid. The bottom cap sits at
// y=0 and the top cap at y=Depth. Both caps and every wall carry outward
// normals, so the solid lights and picks correctly from any side.
//
// A hole in the shape becomes a hole through the solid, with its own wall.
//
// A deep bevel on a thin shape can fold the inset ring onto itself, which shows
// as a pinched wall. Keep BevelSize below the shape's own thickness.
func ExtrudeGeometry(shape Shape, opts ExtrudeOptions) BufferGeometry {
	return bufferFromMesh(geom.Extrude(shape.contour(), opts.options(), generatorAttributes))
}
