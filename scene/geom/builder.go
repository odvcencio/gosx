package geom

import "math"

// vec3 is a point or a direction in local geometry space.
type vec3 struct{ X, Y, Z float64 }

// vec2 is a texture coordinate.
type vec2 struct{ U, V float64 }

// vertex carries one vertex of every stream. A builder drops the streams the
// caller did not ask for.
type vertex struct {
	position vec3
	normal   vec3
	uv       vec2
	color    vec3
}

// builder collects vertices into flat buffers. It fills only the streams named
// by want, so a raycaster pays for positions alone.
type builder struct {
	want Attribute
	mesh Mesh
}

func newBuilder(want Attribute, vertexCapacity int) *builder {
	if vertexCapacity < 0 {
		vertexCapacity = 0
	}
	b := &builder{want: want}
	b.mesh.Positions = make([]float64, 0, vertexCapacity*3)
	if want&AttrNormals != 0 {
		b.mesh.Normals = make([]float64, 0, vertexCapacity*3)
	}
	if want&AttrUVs != 0 {
		b.mesh.UVs = make([]float64, 0, vertexCapacity*2)
	}
	if want&AttrColors != 0 {
		b.mesh.Colors = make([]float64, 0, vertexCapacity*3)
	}
	return b
}

// emit appends one vertex and returns its vertex number. An indexed builder
// keeps the number; a non-indexed builder throws it away.
func (b *builder) emit(v vertex) int {
	index := len(b.mesh.Positions) / 3
	b.mesh.Positions = append(b.mesh.Positions, v.position.X, v.position.Y, v.position.Z)
	if b.want&AttrNormals != 0 {
		b.mesh.Normals = append(b.mesh.Normals, v.normal.X, v.normal.Y, v.normal.Z)
	}
	if b.want&AttrUVs != 0 {
		b.mesh.UVs = append(b.mesh.UVs, v.uv.U, v.uv.V)
	}
	if b.want&AttrColors != 0 {
		b.mesh.Colors = append(b.mesh.Colors, v.color.X, v.color.Y, v.color.Z)
	}
	return index
}

// tri appends three vertices as one non-indexed triangle.
func (b *builder) tri(a, c, d vertex) {
	b.emit(a)
	b.emit(c)
	b.emit(d)
}

// flatTri appends one triangle whose three vertices share the face normal. Use
// it where a hard edge must stay hard.
func (b *builder) flatTri(p0, p1, p2 vec3, color vec3, uv0, uv1, uv2 vec2) {
	n := triangleNormal(p0, p1, p2)
	b.tri(
		vertex{position: p0, normal: n, uv: uv0, color: color},
		vertex{position: p1, normal: n, uv: uv1, color: color},
		vertex{position: p2, normal: n, uv: uv2, color: color},
	)
}

// index appends three vertex numbers as one triangle.
func (b *builder) index(a, c, d int) {
	b.mesh.Indices = append(b.mesh.Indices, a, c, d)
}

func (b *builder) build() *Mesh {
	mesh := b.mesh
	return &mesh
}

// triangleNormal returns the unit normal of the triangle a, b, c, wound
// counter-clockwise as seen from the side the normal points to. A degenerate
// triangle returns +Y, which keeps the buffer finite.
func triangleNormal(a, b, c vec3) vec3 {
	ab := vec3{b.X - a.X, b.Y - a.Y, b.Z - a.Z}
	ac := vec3{c.X - a.X, c.Y - a.Y, c.Z - a.Z}
	return normalize(vec3{
		X: ab.Y*ac.Z - ab.Z*ac.Y,
		Y: ab.Z*ac.X - ab.X*ac.Z,
		Z: ab.X*ac.Y - ab.Y*ac.X,
	})
}

// normalize scales a vector to unit length. A zero or non-finite vector returns
// +Y, so no buffer ever carries a NaN normal.
func normalize(v vec3) vec3 {
	length := math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
	if length <= 0 || math.IsNaN(length) || math.IsInf(length, 0) {
		return vec3{Y: 1}
	}
	inv := 1 / length
	return vec3{X: v.X * inv, Y: v.Y * inv, Z: v.Z * inv}
}

func addVec(a, b vec3) vec3 { return vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

func subVec(a, b vec3) vec3 { return vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

func scaleVec(v vec3, s float64) vec3 { return vec3{v.X * s, v.Y * s, v.Z * s} }

func dotVec(a, b vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

func crossVec(a, b vec3) vec3 {
	return vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}

// radialUV maps a point on a cap circle onto the unit square.
func radialUV(cosTheta, sinTheta float64) vec2 {
	return vec2{U: 0.5 + cosTheta*0.5, V: 0.5 + sinTheta*0.5}
}
