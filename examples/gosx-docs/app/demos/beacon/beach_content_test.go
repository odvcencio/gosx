package docs

import (
	"crypto/sha256"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func TestBeachCachedAssetURLsMatchContent(t *testing.T) {
	depth := BlackglassBeachProgram("shore", "").Environment.Ocean.Bathymetry.Src
	for _, src := range []string{blackglassBeachPosterSrc, depth, blackglassDiscoveriesScriptSrc, blackglassChartScriptSrc, blackglassDiscoveriesStylesSrc} {
		u, err := url.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join("../../../public", strings.TrimPrefix(u.Path, "/")))
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("%x", sha256.Sum256(data))[:12]
		if u.Query().Get("v") != want {
			t.Errorf("%s needs content fingerprint %s", src, want)
		}
	}
}

func TestBeachHeroFramesCoveAndGlass(t *testing.T) {
	view := blackglassBeachViewFor("shore")
	yaw := math.Atan2(view.Target.X-view.Position.X, view.Position.Z-view.Target.Z)
	pitch := math.Atan2(view.Target.Y-view.Position.Y, math.Hypot(view.Target.X-view.Position.X, view.Target.Z-view.Position.Z))
	camera := BlackglassBeachProgram("shore", "").Camera
	project := func(point scene.Vector3, fov, aspect float64) (float64, float64) {
		vertical := math.Tan(fov * math.Pi / 360)
		dx, dy, dz := point.X-view.Position.X, point.Y-view.Position.Y, point.Z-view.Position.Z
		right := dx*math.Cos(yaw) + dz*math.Sin(yaw)
		forward := dx*math.Sin(yaw) - dz*math.Cos(yaw)
		depth := forward*math.Cos(pitch) + dy*math.Sin(pitch)
		up := dy*math.Cos(pitch) - forward*math.Sin(pitch)
		return .5 + right/(2*vertical*aspect*depth), .5 - up/(2*vertical*depth)
	}
	for _, frame := range []struct {
		name        string
		fov, aspect float64
	}{{"desktop", camera.FOV, 1440.0 / 900}, {"portrait", camera.PortraitFOV, 390.0 / 844}} {
		for name, point := range map[string]scene.Vector3{
			"glass":       scene.Vec3(beachgen.MonolithX, 1.8, beachgen.MonolithZ),
			"stack crown": scene.Vec3(-16, 18, -32),
		} {
			x, y := project(point, frame.fov, frame.aspect)
			if x < .12 || x > .88 || y < .08 || y > .8 {
				t.Errorf("%s hero crops %s at (%g, %g)", frame.name, name, x, y)
			}
		}
	}
	_, shore := project(scene.Vec3(view.Position.X, beachgen.TerrainHeight(view.Position.X, 5, beachgen.Seed), 5), camera.FOV, 1440.0/900)
	if shore < .6 || shore > .95 {
		t.Fatalf("hero must show foreshore in the foreground: %g", shore)
	}
	eye := view.Position.Y - beachgen.TerrainHeight(view.Position.X, view.Position.Z, beachgen.Seed)
	if eye < 1.65 || eye > 1.75 {
		t.Fatalf("hero eye height %g", eye)
	}
	if sun := beachgen.PeriodSky(beachgen.PeriodGolden).SunDirection; sun.Z < .2 || sun.Y < .1 {
		t.Fatal("golden key must light the visible rock faces")
	}
}

func TestBeachContentPreservesWetnessAndEnablesGlass(t *testing.T) {
	detail := blackglassBeachDetail()
	if detail.Ground.Roughness != "" {
		t.Fatal("dry detail must not overwrite wet sand")
	}
	if detail.Ground.Albedo == "" || detail.Ground.Normal == "" || detail.Steep.Albedo == "" || detail.Steep.Normal == "" || detail.Steep.Roughness == "" {
		t.Fatal("sand and basalt need scanned surface detail")
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

func TestBeachOverlookClearsHeadland(t *testing.T) {
	view := blackglassBeachViewFor("cliff")
	for step := 1; step < 100; step++ {
		progress := float64(step) / 100
		x := view.Position.X + progress*(view.Target.X-view.Position.X)
		y := view.Position.Y + progress*(view.Target.Y-view.Position.Y)
		z := view.Position.Z + progress*(view.Target.Z-view.Position.Z)
		if ground := beachgen.TerrainHeight(x, z, beachgen.Seed); ground >= y {
			t.Fatalf("overlook looks through the headland at (%g, %g): ground %g, sightline %g", x, z, ground, y)
		}
	}
}

func TestBeachPeriodsHaveDistinctHorizonLight(t *testing.T) {
	direction := scene.Vec3(0, .03, -1)
	r, _, b := beachgen.PeriodSky(beachgen.PeriodBlue).PhysicalRadiance(direction, false)
	if b < 1.5*r {
		t.Fatal("blue hour must lose the warm sunset at the horizon")
	}
	sun := beachgen.PeriodSky(beachgen.PeriodGolden).SunDirection
	direction = scene.Vec3(sun.X, .03, sun.Z)
	r, _, b = beachgen.PeriodSky(beachgen.PeriodGolden).PhysicalRadiance(direction, false)
	if r <= b {
		t.Fatal("golden hour must retain its warm sun path")
	}
}
