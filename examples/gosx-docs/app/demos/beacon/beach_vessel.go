package docs

import (
	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

const blackglassWakeTextureSrc = blackglassBeachModelRoot + "wake-foam.png?v=9d1797f91df0"

func blackglassBeachVessel() *scene.Vessel {
	v := &scene.Vessel{
		ControlsAnchorID: "coast-controls", NodeID: "clipper", Position: scene.Vec3(beachgen.ShipX, 0, beachgen.ShipZ), Heading: beachgen.ShipHeading,
		Length: 22, Beam: 5, Draft: 1.4, DeckHeight: beachgen.JettyDeck, Helm: scene.Vec3(.65, 4.2, 8),
		WindDirection: 8, WindStrength: 8, MaxSpeed: 10, SailTrim: .55, QuickBoard: true,
		OpenWater:   true,
		Navigation:  true,
		Landings:    blackglassIslandLandings(),
		LODs:        []scene.VesselLOD{{NodeID: "clipper-mid", Distance: 65}, {NodeID: "clipper-low", Distance: 120}},
		WakeTexture: blackglassWakeTextureSrc,
	}
	v.CommandSignal = "$blackglass.vesselCommand"
	v.StateSignal = "$blackglass.navigation"
	v.DestinationSignal = "$blackglass.destination"
	for _, place := range blackglassBeachPlaces() {
		if !place.Island {
			continue
		}
		route := scene.VesselRoute{ID: place.View, Name: place.Name, Position: scene.Vec3(place.X, 0, place.Z), Departure: []scene.Vector3{scene.Vec3(80, 0, -80)}, DepartureRadius: 45, ApproachBeforeZ: -230}
		for _, p := range place.Route {
			route.Approach = append(route.Approach, scene.Vec3(p[0], 0, p[1]))
		}
		v.Routes = append(v.Routes, route)
	}
	return v
}
