package docs

import (
	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func blackglassBeachVessel() *scene.Vessel {
	return &scene.Vessel{
		NodeID: "clipper", Position: scene.Vec3(beachgen.ShipX, 0, beachgen.ShipZ), Heading: beachgen.ShipHeading,
		Length: 22, Beam: 5, Draft: 1.4, DeckHeight: beachgen.JettyDeck, Helm: scene.Vec3(.65, 4.2, 8),
		WindDirection: 8, WindStrength: 8, MaxSpeed: 10, SailTrim: .55,
		Bounds:      &scene.WalkBounds{MinX: -115, MinZ: -180, MaxX: 115, MaxZ: 4},
		LODs:        []scene.VesselLOD{{NodeID: "clipper-mid", Distance: 65}, {NodeID: "clipper-low", Distance: 120}},
		WakeTexture: blackglassBeachModelRoot + "wake-foam.png",
	}
}
