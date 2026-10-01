package docs

import (
	"math"
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func TestBeachHeroFramesGlassAndGlintPath(t *testing.T) {
	view := blackglassBeachViewFor("shore")
	yaw := math.Atan2(view.Target.X-view.Position.X, view.Position.Z-view.Target.Z)
	horizontal := math.Tan(42*math.Pi/360) * (1440.0 / 900)
	project := func(angle float64) float64 { return .5 + math.Tan(angle-yaw)/(2*horizontal) }
	glass := project(math.Atan2(beachgen.MonolithX-view.Position.X, view.Position.Z-beachgen.MonolithZ))
	sun := beachgen.PeriodSky(beachgen.PeriodGolden).SunDirection
	glint := project(math.Atan2(sun.X, -sun.Z))
	if glass < .30 || glass > .38 || glint < .4 || glint > .65 {
		t.Fatalf("hero glass %g, glint %g", glass, glint)
	}
	eye := view.Position.Y - beachgen.TerrainHeight(view.Position.X, view.Position.Z, beachgen.Seed)
	if eye < .8 || eye > 1.3 {
		t.Fatalf("hero eye height %g", eye)
	}
}

func TestBeachContentPreservesWetnessAndEnablesGlass(t *testing.T) {
	detail := blackglassBeachDetail()
	if detail.Ground.Roughness != "" || detail.Ground.Albedo != "" {
		t.Fatal("dry detail must not overwrite wet sand")
	}
	if detail.Steep.Normal == "" || detail.Steep.Roughness == "" {
		t.Fatal("basalt needs normal and roughness detail")
	}
	for _, period := range beachgen.Periods {
		p := BlackglassBeachProgram("glass", period)
		if p.Camera.Position != blackglassBeachViewFor("glass").Position {
			t.Fatal("close-up lost its camera")
		}
		for _, node := range p.Graph.Nodes {
			m, ok := node.(scene.Model)
			if !ok || m.ID != "monolith" {
				continue
			}
			material := m.Material.(scene.StandardMaterial)
			if material.Transmission < .8 || material.IOR == nil || *material.IOR <= 1 {
				t.Fatal("monolith must request real glass rendering")
			}
		}
	}
}

func TestBeachPeriodsHaveDistinctHorizonLight(t *testing.T) {
	direction := scene.Vec3(0, .03, -1)
	r, _, b := beachgen.PeriodSky(beachgen.PeriodBlue).PhysicalRadiance(direction, false)
	if b < 1.5*r {
		t.Fatal("blue hour must lose the warm sunset at the horizon")
	}
	r, _, b = beachgen.PeriodSky(beachgen.PeriodGolden).PhysicalRadiance(direction, false)
	if r <= b {
		t.Fatal("golden hour must retain its warm sun path")
	}
}
