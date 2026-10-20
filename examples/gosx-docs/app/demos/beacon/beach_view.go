package docs

import (
	"sync"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// BlackglassViewUpdate changes presentation without replacing the live world,
// its vessel transforms, collected objects, or the pocket emulator.
type BlackglassViewUpdate struct {
	View     string          `json:"view"`
	Period   string          `json:"period"`
	Label    string          `json:"label"`
	Camera   scene.IRCamera  `json:"camera"`
	Commands []scene.Command `json:"commands"`
}

var blackglassViewOnce sync.Once
var blackglassViewPeriods map[string]scene.SceneIR

func BlackglassBeachViewUpdate(viewID, fromPeriod, periodID string) BlackglassViewUpdate {
	view, period := blackglassBeachViewFor(viewID), blackglassBeachPeriodFor(periodID)
	blackglassViewOnce.Do(func() {
		blackglassViewPeriods = make(map[string]scene.SceneIR, len(beachgen.Periods))
		for _, id := range beachgen.Periods {
			blackglassViewPeriods[id] = blackglassBeachProgram("shore", id, false).SceneIR()
		}
	})
	before := blackglassViewPeriods[blackglassBeachPeriodFor(fromPeriod).ID]
	after := blackglassViewPeriods[period.ID]
	// The ship and island models are identical in every lighting preset. Diff
	// only the authored light, atmosphere, beacon, and grotto presentation.
	commands := scene.DiffCommands(before, after)
	if commands == nil {
		commands = []scene.Command{}
	}
	camera := scene.PerspectiveCamera{Position: view.Position, Rotation: blackglassBeachLookRotation(view.Position, view.Target), FOV: 50, PortraitFOV: 70, Near: .1, Far: 900}
	if view.ID == "pools" {
		camera.PortraitFOV = 95
	}
	return BlackglassViewUpdate{View: view.ID, Period: period.ID, Label: "Blackglass Beach — " + view.Name + " at " + period.Name, Camera: scene.CameraIR(camera), Commands: commands}
}

func blackglassBeachPresentation(view, period string) *scene.Presentation {
	config := &scene.Presentation{Endpoint: "/api/blackglass-view", InputSignal: "$blackglass.presentation", OutputSignal: "$blackglass.presentationState", InitialView: view, InitialPeriod: period, Query: true}
	for _, id := range []string{"shore", "glass", "cliff", "pools", "ship", "beacon", "hollow"} {
		v := blackglassBeachViewFor(id)
		camera := scene.PerspectiveCamera{Position: v.Position, Rotation: blackglassBeachLookRotation(v.Position, v.Target), FOV: 50, PortraitFOV: 70, Near: .1, Far: 900}
		if id == "pools" {
			camera.PortraitFOV = 95
		}
		config.Views = append(config.Views, scene.PresentationView{ID: id, Label: v.Name, Camera: scene.CameraIR(camera), Vessel: id == "ship"})
	}
	for _, id := range beachgen.Periods {
		hour := 17.5
		if id == beachgen.PeriodNoon {
			hour = 12
		}
		if id == beachgen.PeriodBlue {
			hour = 18.3
		}
		config.Periods = append(config.Periods, scene.PresentationPeriod{ID: id, Hour: hour})
	}
	return config
}
