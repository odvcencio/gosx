package docs

import (
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// The beacon: a lighthouse on the eastern headland whose beam sweeps the haze.
// A small analytic scattering shader gives its beam soft edges. The beam
// and its water glint sweep together, strongest at blue hour.
const (
	beaconX, beaconZ = 45.0, -14.0
	beaconTower      = 9.0
)

func blackglassBeachMoments(periodID string) []scene.Node {
	nodes := blackglassBeachBeacon(periodID)
	nodes = append(nodes, blackglassBeachPools()...)
	nodes = append(nodes, blackglassBeachWreck()...)
	nodes = append(nodes, blackglassBeachTrail()...)
	return append(nodes, blackglassBeachGrotto(periodID)...)
}

func blackglassBeachBeacon(periodID string) []scene.Node {
	ground := beachgen.TerrainHeight(beaconX, beaconZ, beachgen.Seed)
	lantern := ground + beaconTower + 0.8
	glow, beamCore := 1.5, 0.0
	if periodID == beachgen.PeriodBlue {
		glow, beamCore = 9, 0.4
	}
	white := scene.StandardMaterial{Color: "#e9e4dc", Roughness: 0.7}
	red := scene.StandardMaterial{Color: "#8e2a22", Roughness: 0.6}
	dark := scene.StandardMaterial{Color: "#1c1d1f", Roughness: 0.5, Metalness: 0.6}
	warm := [3]float64{1, 0.82, 0.55}
	light := scene.StandardMaterial{Color: "#fff1d6", EmissiveColor: &warm, Emissive: glow, Roughness: 0.2}
	beam := func(id string, length, radius, opacity float64) scene.Mesh {
		return scene.Mesh{ID: id, Geometry: blackglassBeamGeometry(length, radius),
			Material: blackglassBeamMaterial(opacity*.5, scene.Vec3(beaconX, lantern, beaconZ)),
			Visible:  scene.Bool(opacity > 0),
			Position: scene.Vec3(beaconX, lantern, beaconZ), Spin: scene.Euler{Y: 0.45},
			DepthWrite: scene.Bool(false), CastShadow: false, ReceiveShadow: false}
	}
	y := func(h float64) scene.Vector3 { return scene.Vec3(beaconX, ground+h, beaconZ) }
	return []scene.Node{
		scene.Mesh{ID: "beacon-tower", Geometry: scene.CylinderGeometry{RadiusTop: 1.1, RadiusBottom: 1.6, Height: beaconTower, Segments: 24},
			Material: white, Position: y(beaconTower / 2), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-band", Geometry: scene.CylinderGeometry{RadiusTop: 1.33, RadiusBottom: 1.44, Height: 1.6, Segments: 24},
			Material: red, Position: y(beaconTower * 0.55), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-gallery", Geometry: scene.CylinderGeometry{RadiusTop: 1.5, RadiusBottom: 1.5, Height: 0.25, Segments: 24},
			Material: dark, Position: y(beaconTower + 0.1), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-lantern", Geometry: scene.CylinderGeometry{RadiusTop: 0.8, RadiusBottom: 0.8, Height: 1.1, Segments: 16},
			Material: light, Position: y(beaconTower + 0.8)},
		scene.Mesh{ID: "beacon-roof", Geometry: scene.CylinderGeometry{RadiusTop: 0.05, RadiusBottom: 1.1, Height: 0.9, Segments: 16},
			Material: red, Position: y(beaconTower + 1.8), CastShadow: true},
		beam("beacon-beam", 70, 4, beamCore),
		blackglassBeachBeamReflection(beamCore),
	}
}

// Three axial planes approximate the cone's depth in one additive draw.
// Smooth radial and exponential distance falloff soften its visible edges.
func blackglassBeamGeometry(length, radius float64) scene.BufferGeometry {
	g := scene.BufferGeometry{Revision: 1}
	for _, dir := range []float64{1, -1} {
		for plane := 0; plane < 3; plane++ {
			angle := float64(plane) * math.Pi / 3
			base := len(g.Positions) / 3
			for _, point := range [][2]float64{{0, -.03}, {dir * length, -radius}, {dir * length, radius}, {0, .03}} {
				g.Positions = append(g.Positions, point[0], point[1]*math.Cos(angle), point[1]*math.Sin(angle))
				g.Normals = append(g.Normals, 0, -math.Sin(angle), math.Cos(angle))
			}
			g.UVs = append(g.UVs, 0, 0, 1, 0, 1, 1, 0, 1)
			g.Indices = append(g.Indices, base, base+1, base+2, base, base+2, base+3,
				base, base+2, base+1, base, base+3, base+2)
		}
	}
	return blackglassShaderGeometry(g)
}

// Millimetre precision keeps procedural geometry compact on the wire.
func momentRound(v float64) float64 { return math.Round(v*1000) / 1000 }

// Add a flat-shaded quad with shared triangle vertices and compact normals.
func momentQuad(g *scene.BufferGeometry, a, b, c, d scene.Vector3) {
	u, v := scene.Vec3(b.X-a.X, b.Y-a.Y, b.Z-a.Z), scene.Vec3(c.X-a.X, c.Y-a.Y, c.Z-a.Z)
	n := scene.Vec3(u.Y*v.Z-u.Z*v.Y, u.Z*v.X-u.X*v.Z, u.X*v.Y-u.Y*v.X)
	length := math.Sqrt(n.X*n.X + n.Y*n.Y + n.Z*n.Z)
	base := len(g.Positions) / 3
	for _, p := range []scene.Vector3{a, b, c, d} {
		g.Positions = append(g.Positions, momentRound(p.X), momentRound(p.Y), momentRound(p.Z))
		g.Normals = append(g.Normals, momentRound(n.X/length), momentRound(n.Y/length), momentRound(n.Z/length))
	}
	g.Indices = append(g.Indices, base, base+1, base+2, base, base+2, base+3)
}

func blackglassBeachBeaconCollider() scene.WalkCollider {
	return scene.WalkCollider{Kind: "cylinder", X: beaconX, Z: beaconZ,
		Y: beachgen.TerrainHeight(beaconX, beaconZ, beachgen.Seed), Radius: 1.6, Height: beaconTower + 2.25}
}
