package beachgen

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"m31labs.dev/gosx/scene"
)

// StaticGLB bakes authored geometry and its PBR material into one quantized
// draw, without requiring a runtime material override while models load.
func StaticGLB(material scene.StandardMaterial, nodes ...scene.Node) ([]byte, error) {
	rgb, err := hex.DecodeString(strings.TrimPrefix(material.Color, "#"))
	if err != nil || len(rgb) != 3 {
		return nil, fmt.Errorf("static material requires an RGB hex color")
	}
	color := [4]float64{srgbLinear(float64(rgb[0]) / 255), srgbLinear(float64(rgb[1]) / 255), srgbLinear(float64(rgb[2]) / 255), 1}
	out := &geometry{}
	for _, node := range nodes {
		switch n := node.(type) {
		case scene.Mesh:
			g, err := staticGeometry(n.Geometry)
			if err != nil {
				return nil, err
			}
			appendStatic(out, g, n.Position, n.Rotation, n.Scale)
		case scene.InstancedMesh:
			if len(n.Positions) != n.Count || len(n.Rotations) != n.Count || len(n.Scales) != n.Count {
				return nil, fmt.Errorf("static instances require explicit transforms")
			}
			g, err := staticGeometry(n.Geometry)
			if err != nil {
				return nil, err
			}
			for i := 0; i < n.Count; i++ {
				appendStatic(out, g, n.Positions[i], n.Rotations[i], n.Scales[i])
			}
		default:
			return nil, fmt.Errorf("unsupported static node %T", node)
		}
	}
	return writeGLB(out, solidMaterial(color, material.Roughness, material.Metalness), nil)
}

func staticGeometry(authored scene.Geometry) (*geometry, error) {
	switch g := authored.(type) {
	case scene.BufferGeometry:
		out := &geometry{positions: append([]float64(nil), g.Positions...), normals: append([]float64(nil), g.Normals...)}
		out.uvs = make([]float64, len(g.Positions)/3*2)
		for _, i := range g.Indices {
			out.indices = append(out.indices, uint16(i))
		}
		return out, nil
	case scene.CylinderGeometry:
		return staticCylinder(g), nil
	case scene.BoxGeometry:
		return staticBox(g), nil
	default:
		return nil, fmt.Errorf("unsupported static geometry %T", authored)
	}
}

func staticCylinder(c scene.CylinderGeometry) *geometry {
	g := &geometry{}
	segments := c.Segments
	if segments < 3 {
		segments = 8
	}
	for i := 0; i <= segments; i++ {
		a := 2 * math.Pi * float64(i) / float64(segments)
		x, z := math.Sin(a), math.Cos(a)
		ny := (c.RadiusBottom - c.RadiusTop) / c.Height
		norm := math.Sqrt(1 + ny*ny)
		for _, p := range []vec3{{x * c.RadiusBottom, -c.Height / 2, z * c.RadiusBottom}, {x * c.RadiusTop, c.Height / 2, z * c.RadiusTop}} {
			g.positions = append(g.positions, p.x, p.y, p.z)
			g.normals = append(g.normals, x/norm, ny/norm, z/norm)
			g.uvs = append(g.uvs, 0, 0)
		}
		if i < segments {
			v := uint16(i * 2)
			g.indices = append(g.indices, v, v+3, v+1, v, v+2, v+3)
		}
	}
	for _, side := range []float64{-1, 1} {
		radius := c.RadiusBottom
		if side > 0 {
			radius = c.RadiusTop
		}
		base := uint16(len(g.positions) / 3)
		g.positions = append(g.positions, 0, side*c.Height/2, 0)
		g.normals = append(g.normals, 0, side, 0)
		g.uvs = append(g.uvs, 0, 0)
		for i := 0; i < segments; i++ {
			a := 2 * math.Pi * float64(i) / float64(segments)
			g.positions = append(g.positions, radius*math.Sin(a), side*c.Height/2, radius*math.Cos(a))
			g.normals = append(g.normals, 0, side, 0)
			g.uvs = append(g.uvs, 0, 0)
			b, next := base+1+uint16(i), base+1+uint16((i+1)%segments)
			if side > 0 {
				g.indices = append(g.indices, base, b, next)
			} else {
				g.indices = append(g.indices, base, next, b)
			}
		}
	}
	return g
}

func staticBox(b scene.BoxGeometry) *geometry {
	g := &geometry{}
	x, y, z := b.Width/2, b.Height/2, b.Depth/2
	for _, face := range [][4]vec3{
		{{x, -y, z}, {x, -y, -z}, {x, y, -z}, {x, y, z}}, {{-x, -y, -z}, {-x, -y, z}, {-x, y, z}, {-x, y, -z}},
		{{-x, y, z}, {x, y, z}, {x, y, -z}, {-x, y, -z}}, {{-x, -y, -z}, {x, -y, -z}, {x, -y, z}, {-x, -y, z}},
		{{-x, -y, z}, {x, -y, z}, {x, y, z}, {-x, y, z}}, {{x, -y, -z}, {-x, -y, -z}, {-x, y, -z}, {x, y, -z}},
	} {
		scatterQuad(g, face[0], face[1], face[2], face[3])
	}
	averageVertexNormals(g)
	return g
}

func appendStatic(out, g *geometry, position scene.Vector3, rotation scene.Euler, scale scene.Vector3) {
	if scale == (scene.Vector3{}) {
		scale = scene.Vec3(1, 1, 1)
	}
	rotate := func(v vec3) vec3 {
		sx, cx := math.Sincos(rotation.X)
		sy, cy := math.Sincos(rotation.Y)
		sz, cz := math.Sincos(rotation.Z)
		v.y, v.z = cx*v.y-sx*v.z, sx*v.y+cx*v.z
		v.x, v.z = cy*v.x+sy*v.z, -sy*v.x+cy*v.z
		v.x, v.y = cz*v.x-sz*v.y, sz*v.x+cz*v.y
		return v
	}
	base := uint16(len(out.positions) / 3)
	for i := 0; i < len(g.positions); i += 3 {
		p := rotate(vec3{g.positions[i] * scale.X, g.positions[i+1] * scale.Y, g.positions[i+2] * scale.Z})
		n := rotate(vec3{g.normals[i] / scale.X, g.normals[i+1] / scale.Y, g.normals[i+2] / scale.Z})
		length := math.Sqrt(n.x*n.x + n.y*n.y + n.z*n.z)
		out.positions = append(out.positions, p.x+position.X, p.y+position.Y, p.z+position.Z)
		out.normals = append(out.normals, n.x/length, n.y/length, n.z/length)
		out.uvs = append(out.uvs, 0, 0)
	}
	for _, i := range g.indices {
		out.indices = append(out.indices, base+i)
	}
}
