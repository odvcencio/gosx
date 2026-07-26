package scene

import "m31labs.dev/gosx/scene/geom"

// TriangleMesh is the tessellated surface of one geometry, in the geometry's
// own local space. Positions and Normals hold three numbers per vertex; UVs
// hold two. Indices holds three vertex numbers per triangle; an empty Indices
// means the positions already run as a triangle list.
//
// Normals and UVs are empty when the source geometry carries none.
type TriangleMesh struct {
	Positions []float64
	Normals   []float64
	UVs       []float64
	Indices   []int
}

// VertexCount returns the number of vertices the mesh holds.
func (m TriangleMesh) VertexCount() int { return len(m.Positions) / 3 }

// TriangleCount returns the number of triangles the mesh draws.
func (m TriangleMesh) TriangleCount() int {
	if len(m.Indices) > 0 {
		return len(m.Indices) / 3
	}
	return len(m.Positions) / 9
}

// Tessellate turns one geometry into the triangles a renderer draws and a ray
// tests. It answers for every geometry that owns a surface. It reports false
// for a geometry with no surface, such as LinesGeometry, and for a nil or
// unknown geometry.
//
// The triangles come from package scene/geom, the single generator the browser
// wire path, the native renderer and the exact raycaster all share. Ask this
// function instead of writing a second generator. A second copy makes the three
// consumers disagree, and no test can see the difference.
//
// A BufferGeometry is already triangles, so the returned mesh borrows the
// caller's slices and copies nothing. Do not write through the result.
func Tessellate(geometry Geometry) (TriangleMesh, bool) {
	return tessellate(geometry, geom.AllAttributes)
}

// tessellate is the shared body. want selects the vertex streams to fill; the
// raycaster asks for positions alone, because a triangle test reads no other
// stream.
func tessellate(geometry Geometry, want geom.Attribute) (TriangleMesh, bool) {
	switch g := geometry.(type) {
	case nil:
		return TriangleMesh{}, false
	case BufferGeometry:
		return bufferTriangleMesh(g)
	case *BufferGeometry:
		if g == nil {
			return TriangleMesh{}, false
		}
		return bufferTriangleMesh(*g)
	case LinesGeometry:
		// A polyline owns no surface. Callers pick it with a threshold instead.
		return TriangleMesh{}, false
	}
	params, ok := geometryParams(geometry)
	if !ok {
		return TriangleMesh{}, false
	}
	mesh := geom.Build(params, want)
	if mesh == nil || mesh.VertexCount() == 0 {
		return TriangleMesh{}, false
	}
	return TriangleMesh{
		Positions: mesh.Positions,
		Normals:   mesh.Normals,
		UVs:       mesh.UVs,
		Indices:   mesh.Indices,
	}, true
}

func bufferTriangleMesh(g BufferGeometry) (TriangleMesh, bool) {
	if len(g.Positions) < 9 && len(g.Indices) < 3 {
		return TriangleMesh{}, false
	}
	return TriangleMesh{
		Positions: g.Positions,
		Normals:   g.Normals,
		UVs:       g.UVs,
		Indices:   g.Indices,
	}, true
}

// geometryParams maps one authored parametric geometry onto the generator
// parameters. It reports false for a geometry the generator does not name.
//
// Add a case here whenever a new parametric Geometry type lands, and add the
// generator to package scene/geom. Forgetting the generator makes Tessellate
// report false, which the caller must then surface. Forgetting this case does
// the same. Neither failure can drop a draw in silence.
func geometryParams(geometry Geometry) (geom.Params, bool) {
	switch g := geometry.(type) {
	case CubeGeometry:
		return geom.Params{Kind: geom.KindCube, Size: g.Size}, true
	case BoxGeometry:
		return geom.Params{Kind: geom.KindBox, Width: g.Width, Height: g.Height, Depth: g.Depth}, true
	case PlaneGeometry:
		return geom.Params{Kind: geom.KindPlane, Width: g.Width, Height: g.Height}, true
	case PyramidGeometry:
		return geom.Params{Kind: geom.KindPyramid, Width: g.Width, Height: g.Height, Depth: g.Depth}, true
	case SphereGeometry:
		return geom.Params{Kind: geom.KindSphere, Radius: g.Radius, Segments: g.Segments}, true
	case CylinderGeometry:
		if g.RadiusTop <= 0 {
			return geom.Params{
				Kind: geom.KindCone, RadiusBottom: g.RadiusBottom, Height: g.Height, Segments: g.Segments,
			}, true
		}
		return geom.Params{
			Kind: geom.KindCylinder, RadiusTop: g.RadiusTop, RadiusBottom: g.RadiusBottom,
			Height: g.Height, Segments: g.Segments,
		}, true
	case TorusGeometry:
		return geom.Params{
			Kind: geom.KindTorus, Radius: g.Radius, Tube: g.Tube,
			RadialSegments: g.RadialSegments, TubularSegments: g.TubularSegments,
		}, true
	case TorusKnotGeometry:
		return geom.Params{
			Kind: geom.KindTorusKnot, Radius: g.Radius, Tube: g.Tube,
			RadialSegments: g.RadialSegments, TubularSegments: g.TubularSegments,
		}, true
	}
	return geom.Params{}, false
}
