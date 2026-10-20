package docs

import (
	"fmt"
	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func blackglassBeachExploration() *scene.Exploration {
	e := &scene.Exploration{RevealSignal: "$blackglass.journalOpen", DismissSignals: []string{"$blackglass.chartOpen"}, InputSignal: "$blackglass.inspect", OutputSignal: "$blackglass.journal", StorageKey: "gosx.blackglass.field-journal.v1", Pocket: &scene.Pocket{PauseBackground: true, DialogID: "coast-handheld", Source: "/demos/blackglass-handheld/pocket.html", Title: "Playable pocket Game Boy Color", Protocol: "gosx-handheld", OpenSignal: "$blackglass.pocket"}}
	labels := []string{"Ochre stars", "Anemone garden", "The limpet ledge"}
	for i, site := range blackglassBeachPoolSites() {
		e.Items = append(e.Items, scene.Inspection{ID: fmt.Sprintf("tide-pool-%d", i), Label: labels[i], Pulse: &scene.InspectionPulse{NodeID: "pool-ripple", Position: scene.Vec3(site[0], blackglassBeachPoolLevel(site[0], site[1], site[2])+.018, site[1]), Radius: site[2], DurationSeconds: 1.1}})
	}
	// The typed collection patch is replayed after model hydration on return.
	e.Items = append(e.Items, scene.Inspection{ID: "shore-handheld", Label: "A pocket-sized time capsule", Collect: true, Commands: blackglassHandheldPickup()})
	for _, island := range beachgen.OffshoreIslands() {
		p := blackglassIslandFindPosition(island)
		item := scene.Inspection{ID: island.Find, Label: map[string]string{"drift-letter": "The drift letter", "tide-bell": "The tide bell", "star-dial": "The star dial"}[island.Find], Position: &p, Radius: 7}
		if island.Find == "tide-bell" {
			item.Sound = &scene.SynthPatch{Name: "Tide bell", Tones: []scene.ToneLayer{{Frequency: 440, Duration: 2.7, Waveform: "sine", Gain: scene.Float(.13)}, {Frequency: 880, Duration: 2.7, Waveform: "sine", Gain: scene.Float(.04)}, {Frequency: 1188, Duration: 2.7, Waveform: "sine", Gain: scene.Float(.025)}}}
		}
		e.Items = append(e.Items, item)
	}
	return e
}
