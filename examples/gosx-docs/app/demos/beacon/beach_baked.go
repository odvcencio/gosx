package docs

import (
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// BlackglassBeachStaticAssets keeps the authored geometry in Go, while the
// page sends compact model references instead of large inline vertex arrays.
func BlackglassBeachStaticAssets() (map[string][]byte, error) {
	files := make(map[string][]byte)
	rim := blackglassBeachPools()[3].(scene.Mesh)
	arch := blackglassBeachGrotto(beachgen.PeriodGolden)[0].(scene.Mesh)
	trail := blackglassBeachTrail()[0].(scene.InstancedMesh)
	wreck := blackglassBeachWreck()
	for name, entry := range map[string]struct {
		nodes    []scene.Node
		material scene.Material
	}{
		"tide-pool-rims.glb":    {[]scene.Node{rim}, rim.Material},
		"sun-grotto-arch.glb":   {[]scene.Node{arch}, arch.Material},
		"glass-trail-soles.glb": {[]scene.Node{blackglassBeachBakedFootprints()}, trail.Material},
		"wreck-keel.glb":        {wreck[1:], wreck[0].(scene.InstancedMesh).Material},
	} {
		data, err := beachgen.StaticGLB(entry.material.(scene.StandardMaterial), entry.nodes...)
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	return files, nil
}

func blackglassBeachBakedMoments(period string) []scene.Node {
	var nodes []scene.Node
	for _, node := range blackglassBeachMoments(period) {
		switch n := node.(type) {
		case scene.Mesh:
			switch n.ID {
			case "wreck-prow":
				continue
			case "tide-pool-rims", "sun-grotto-arch", "wreck-keel":
				nodes = append(nodes, scene.Model{ID: n.ID, Src: blackglassBeachModelRoot + n.ID + ".glb", Static: scene.Bool(true),
					CastShadow: n.CastShadow, ReceiveShadow: n.ReceiveShadow})
				continue
			}
		case scene.InstancedMesh:
			if n.ID == "wreck-ribs" {
				// Millimetre-scale art keeps submillimetre transforms, rather
				// than serializing noise from trigonometry and terrain sampling.
				n.Positions = blackglassBeachCompactVectors(n.Positions)
				n.Scales = blackglassBeachCompactVectors(n.Scales)
				for i, r := range n.Rotations {
					n.Rotations[i] = scene.Euler{X: math.Round(r.X*1e4) / 1e4, Y: math.Round(r.Y*1e4) / 1e4, Z: math.Round(r.Z*1e4) / 1e4}
				}
				nodes = append(nodes, n)
				continue
			}
			if n.ID == "glass-trail-heels" {
				continue
			}
			if n.ID == "glass-trail-soles" {
				nodes = append(nodes, scene.Model{ID: n.ID, Src: blackglassBeachModelRoot + n.ID + ".glb", Static: scene.Bool(true),
					CastShadow: n.CastShadow, ReceiveShadow: n.ReceiveShadow})
				continue
			}
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// Only the upper discs of the wet prints are visible. Bake those surfaces
// together; the sides and bottoms buried in sand need no vertices or draws.
func blackglassBeachBakedFootprints() scene.Mesh {
	g := scene.BufferGeometry{Immutable: true, Revision: 1}
	for _, node := range blackglassBeachTrail() {
		batch := node.(scene.InstancedMesh)
		for i, p := range batch.Positions {
			scale, yaw := batch.Scales[i], batch.Rotations[i].Y
			y, base := p.Y+scale.Y/2, len(g.Positions)/3
			g.Positions = append(g.Positions, p.X, y, p.Z)
			g.Normals = append(g.Normals, 0, 1, 0)
			for j := 0; j < 8; j++ {
				a := 2 * math.Pi * float64(j) / 8
				x, z := scale.X*math.Sin(a), scale.Z*math.Cos(a)
				g.Positions = append(g.Positions, p.X+x*math.Cos(yaw)+z*math.Sin(yaw), y, p.Z-x*math.Sin(yaw)+z*math.Cos(yaw))
				g.Normals = append(g.Normals, 0, 1, 0)
				g.Indices = append(g.Indices, base, base+1+j, base+1+(j+1)%8)
			}
		}
	}
	return scene.Mesh{Geometry: g}
}

func blackglassBeachCompactVectors(values []scene.Vector3) []scene.Vector3 {
	for i, v := range values {
		values[i] = scene.Vec3(math.Round(v.X*1e4)/1e4, math.Round(v.Y*1e4)/1e4, math.Round(v.Z*1e4)/1e4)
	}
	return values
}
