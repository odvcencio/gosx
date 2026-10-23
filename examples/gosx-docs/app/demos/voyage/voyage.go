package docs

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/vessel"
)

// The Voyage demo puts an expedition-size ship under way on an open spectral
// ocean. Everything is Go: the ship is built from scene primitives, the sea
// state is scene.OceanPreset, and the controls change Ocean.Spectrum and
// Ocean.Presentation through the same declarative presentation updates
// Blackglass Beach uses for its lighting periods. There is no page script.

// voyageStates are the four sea-state presets, in order.
var voyageStates = []string{"calm", "moderate", "rough", "storm"}

// voyageSpeeds are the game speeds a Bon Voyage passage runs at.
var voyageSpeeds = []int{1, 10, 60, 360, 1000}

// VoyageApparentWaterSpeed is the apparent speed of the water past the hull, in
// m/s, at a game speed. Wave animation never follows game time (the ocean keeps
// Presentation.TimeScale at 1), so a fast-forwarded passage cannot move the
// ship a true speed x game-speed per wall second: at 1,000x a 16 knot ship
// would cover 8 km per wall second and the surface would be a smear. The
// apparent speed rises by one ship speed per decade of game speed and stops at
// four ship speeds:
//
//	apparent = min(4 u, u (1 + log10 S)),   u = the hull's service speed
//
// For the expedition hull (u = 8.2 m/s, 16 knots) that is 8.2 m/s at 1x, 16.4
// at 10x, 22.8 at 60x, 29.2 at 360x and 32.8 m/s (64 knots) from 1,000x. The
// hull is in world coordinates and stays put; the water carries the motion.
func VoyageApparentWaterSpeed(speed int) float64 {
	u := vessel.Expedition().ServiceSpeed
	if speed < 1 {
		speed = 1
	}
	return math.Min(4*u, u*(1+math.Log10(float64(speed))))
}

// voyagePeriodID names a (sea state, game speed) pair: "storm-60x".
func voyagePeriodID(state string, speed int) string { return state + "-" + strconv.Itoa(speed) + "x" }

// voyageParsePeriod reads a period id, falling back to moderate at 1x.
func voyageParsePeriod(id string) (string, int) {
	state, rest, ok := strings.Cut(id, "-")
	speed, err := strconv.Atoi(strings.TrimSuffix(rest, "x"))
	if !ok || err != nil || !contains(voyageStates, state) || !containsInt(voyageSpeeds, speed) {
		return "moderate", 1
	}
	return state, speed
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func containsInt(list []int, v int) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// voyageSea is how each sea state is presented beyond its spectrum.
var voyageSea = map[string]struct {
	choppiness, clouds, turbidity float64
}{
	"calm":     {0.40, 0.25, 3},
	"moderate": {0.55, 0.45, 4},
	"rough":    {0.70, 0.70, 7},
	"storm":    {0.80, 0.95, 14},
}

// voyageOcean is the ocean for a sea state and game speed. The ship heads -Z, so
// the water drifts toward +Z, and the wind drives head seas.
func voyageOcean(state string, speed int) *scene.Ocean {
	spectrum := scene.OceanPreset(state)
	spectrum.Seed = 7
	sea := voyageSea[state]
	return &scene.Ocean{
		WindDirection: 15, Choppiness: sea.choppiness, Foam: 0.85, Extent: 8000,
		DeepColor: "#04202e", ShallowColor: "#1f6f78", ScatterColor: "#2fa58f",
		Reflections: &scene.OceanReflections{Mode: "ssr+planar", Resolution: 0.5, Strength: 0.8},
		Spectrum:    &spectrum,
		// The expedition ship leaves a wake. The ship stands still in the scene and the water drifts past it, so the runtime measures the
		// hull's speed through the water against the drift below.
		Wake: &scene.OceanWake{NodeID: "ship", Enabled: true, Length: vessel.Expedition().Length, Beam: vessel.Expedition().Beam},
		Presentation: &scene.OceanPresentation{
			// Waves always evolve at natural time, whatever the game speed.
			TimeScale:   1,
			WaterDriftZ: VoyageApparentWaterSpeed(speed),
			// Sea-state changes take 20 s of presentation time; the speed
			// buttons leave the spectrum alone and never retrigger it.
			TransitionSeconds: 20,
		},
	}
}

func voyageSky(state string) scene.Sky {
	sea := voyageSea[state]
	sky := scene.Sky{Mode: "physical", SunDirection: scene.SunDirectionFromAngles(38, 130), Turbidity: sea.turbidity, Rayleigh: 1.4, MieCoefficient: 0.004, MieDirectionalG: 0.8}
	sky.Clouds = &scene.SkyClouds{Coverage: sea.clouds, Altitude: 1500, Scale: 5200, Opacity: 0.75, Direction: 20}
	return sky
}

type voyageView struct {
	ID, Name         string
	Position, Target scene.Vector3
}

var voyageViews = []voyageView{
	{"quarter", "Quarter", scene.Vec3(78, 30, 96), scene.Vec3(0, 9, -6)},
	{"waterline", "Waterline", scene.Vec3(40, 5, 46), scene.Vec3(-6, 3, -30)},
}

func voyageViewFor(id string) voyageView {
	for _, v := range voyageViews {
		if v.ID == id {
			return v
		}
	}
	return voyageViews[0]
}

func voyageLook(position, target scene.Vector3) scene.Euler {
	fx, fy, fz := target.X-position.X, target.Y-position.Y, target.Z-position.Z
	return scene.Euler{X: math.Atan2(fy, math.Hypot(fx, fz)), Y: math.Atan2(-fx, -fz)}
}

func voyageCamera(v voyageView) scene.PerspectiveCamera {
	return scene.PerspectiveCamera{Position: v.Position, Rotation: voyageLook(v.Position, v.Target), FOV: 55, PortraitFOV: 75, Near: 0.5, Far: 20000}
}

// voyageSolid collects the faces of a ship part as one baked mesh. The vessel
// runtime resets every ship part's own position to the ship's origin, so each
// part carries its offset in its vertices, as an imported model does.
type voyageSolid struct {
	geometry scene.BufferGeometry
	centre   scene.Vector3
}

func newVoyageSolid(centre scene.Vector3) *voyageSolid {
	return &voyageSolid{geometry: scene.BufferGeometry{Immutable: true, Revision: 1}, centre: centre}
}

// quad adds a flat quad whose normal faces away from the solid's centre.
func (s *voyageSolid) quad(a, b, c, d scene.Vector3) {
	ux, uy, uz := b.X-a.X, b.Y-a.Y, b.Z-a.Z
	vx, vy, vz := d.X-a.X, d.Y-a.Y, d.Z-a.Z
	nx, ny, nz := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx
	if length := math.Sqrt(nx*nx + ny*ny + nz*nz); length > 0 {
		nx, ny, nz = nx/length, ny/length, nz/length
	}
	mx, my, mz := (a.X+b.X+c.X+d.X)/4-s.centre.X, (a.Y+b.Y+c.Y+d.Y)/4-s.centre.Y, (a.Z+b.Z+c.Z+d.Z)/4-s.centre.Z
	if nx*mx+ny*my+nz*mz < 0 {
		b, d = d, b
		nx, ny, nz = -nx, -ny, -nz
	}
	g := &s.geometry
	base := len(g.Positions) / 3
	for i, p := range []scene.Vector3{a, b, c, d} {
		g.Positions = append(g.Positions, p.X, p.Y, p.Z)
		g.Normals = append(g.Normals, nx, ny, nz)
		g.UVs = append(g.UVs, float64(i%2), float64(i/2))
	}
	g.Indices = append(g.Indices, base, base+1, base+2, base, base+2, base+3)
}

// voyageFrustum is a hexahedron from z0 (width w0) to z1 (width w1) between
// y0 and y1, centred on x = 0: a box when w0 == w1, a tapered bow otherwise.
func voyageFrustum(z0, z1, w0, w1, y0, y1 float64) scene.BufferGeometry {
	c := func(x, y, z float64) scene.Vector3 { return scene.Vec3(x, y, z) }
	s := newVoyageSolid(c(0, (y0+y1)/2, (z0+z1)/2))
	a0, b0 := c(-w0/2, y0, z0), c(w0/2, y0, z0)
	c0, d0 := c(w0/2, y1, z0), c(-w0/2, y1, z0)
	a1, b1 := c(-w1/2, y0, z1), c(w1/2, y0, z1)
	c1, d1 := c(w1/2, y1, z1), c(-w1/2, y1, z1)
	s.quad(a0, b0, c0, d0)
	s.quad(a1, b1, c1, d1)
	s.quad(a0, a1, d1, d0)
	s.quad(b0, b1, c1, c0)
	s.quad(d0, c0, c1, d1)
	s.quad(a0, b0, b1, a1)
	return s.geometry
}

// voyageStack is an upright round stack from y0 (radius r0) to y1 (radius r1) at (cx, cz).
func voyageStack(cx, cz, y0, y1, r0, r1 float64, segments int) scene.BufferGeometry {
	s := newVoyageSolid(scene.Vec3(cx, (y0+y1)/2, cz))
	ring := func(r, y float64, i int) scene.Vector3 {
		a := 2 * math.Pi * float64(i) / float64(segments)
		return scene.Vec3(cx+r*math.Cos(a), y, cz+r*math.Sin(a))
	}
	for i := 0; i < segments; i++ {
		s.quad(ring(r0, y0, i), ring(r0, y0, i+1), ring(r1, y1, i+1), ring(r1, y1, i))
		s.quad(scene.Vec3(cx, y1, cz), ring(r1, y1, i), ring(r1, y1, i+1), ring(r1, y1, i+1))
	}
	return s.geometry
}

// voyageShip builds an expedition-size hull (125 m by 21 m, from the vessel
// package's Expedition class) out of baked parts: a tapered bow, a hull block,
// two decks, a bridge with a glass band and an orange stack. The origin is the
// waterline at the ship's centre; the bow points to -Z. Part IDs start with
// "ship/", which is how the vessel runtime finds them.
func voyageShip() []scene.Node {
	hull := vessel.Expedition()
	half := hull.Length / 2
	steel := scene.StandardMaterial{Color: "#1d2a36", Roughness: 0.55, Metalness: 0.2}
	paint := scene.StandardMaterial{Color: "#e8ecee", Roughness: 0.6}
	funnel := scene.StandardMaterial{Color: "#d9792f", Roughness: 0.5}
	glass := scene.StandardMaterial{Color: "#16222c", Roughness: 0.15, Metalness: 0.4}
	top, keel := 6.8, -hull.Draft // hull top above the waterline, and the keel below it
	part := func(id string, g scene.BufferGeometry, m scene.Material) scene.Node {
		return scene.Mesh{ID: "ship/" + id, Geometry: g, Material: m, CastShadow: true, ReceiveShadow: true}
	}
	bowStart := -half + 34
	return []scene.Node{
		part("hull", voyageFrustum(half, bowStart, hull.Beam, hull.Beam, keel, top), steel),
		part("bow", voyageFrustum(bowStart, -half, hull.Beam, hull.Beam*0.12, keel, top+1.5), steel),
		part("deck", voyageFrustum(half-14, -half+52, hull.Beam*0.82, hull.Beam*0.82, top, top+12), paint),
		part("deck-upper", voyageFrustum(half-26, -half+50, hull.Beam*0.66, hull.Beam*0.66, top+12, top+20), paint),
		part("bridge", voyageFrustum(-half+60, -half+48, hull.Beam*0.7, hull.Beam*0.7, top+20, top+24.5), paint),
		part("bridge-glass", voyageFrustum(-half+60.1, -half+47.9, hull.Beam*0.72, hull.Beam*0.72, top+21.2, top+22.8), glass),
		part("funnel", voyageStack(0, 18, top+20, top+30, 3.6, 3.1, 24), funnel),
	}
}

func voyageProps(viewID, periodID string) scene.Props {
	state, speed := voyageParsePeriod(periodID)
	view := voyageViewFor(viewID)
	sky := voyageSky(state)
	sun := sky.SunDirection
	hull := vessel.Expedition()
	nodes := []scene.Node{
		scene.DirectionalLight{ID: "sun", Color: "#fff1dd", Intensity: 3, Direction: scene.Vec3(-sun.X, -sun.Y, -sun.Z), CastShadow: true, ShadowBias: -0.0018, ShadowSize: 2048, ShadowCascades: 3, ShadowSoftness: 1.5},
	}
	nodes = append(nodes, voyageShip()...)
	return scene.Props{
		Width: 1280, Height: 720,
		Label:      "Voyage — " + view.Name + ", " + state + " sea at " + strconv.Itoa(speed) + "x",
		AriaLabel:  "An expedition ship under way on an open sea. The sea state and the game speed change the water; the ship stays level in the frame while the water passes it.",
		Background: "#9fb7c9", Controls: scene.ControlOrbit, ControlTarget: view.Target, ControlZoom: scene.Bool(true), ControlMinDistance: 20, ControlMaxDistance: 400,
		AutoRotate: scene.Bool(false), Responsive: scene.Bool(true), FillHeight: scene.Bool(true),
		PreferWebGPU: scene.Bool(true), CanvasAlpha: scene.Bool(false), Stats: scene.Bool(false),
		UnsupportedMessage: "Interactive 3D is unavailable in this browser.",
		MaxFPS:             120, MaxDevicePixelRatio: 2, MaxPixels: scene.PostFXMaxPixels1440p,
		AdaptiveQuality: scene.Bool(true), AdaptiveTargetFrameMS: 16.7, AdaptiveWarmupFrames: 24, AdaptivePostFX: scene.Bool(true),
		Camera: voyageCamera(view),
		Vessel: &scene.Vessel{
			NodeID: "ship", Position: scene.Vec3(0, 0, 0), Length: hull.Length, Beam: hull.Beam, Draft: hull.Draft, DeckHeight: 8,
			Helm: scene.Vec3(0, 30, -28), OpenWater: true, Wake: scene.Bool(false),
		},
		Environment: scene.Environment{
			Sky: &sky, FogColor: "#b9c7d3", FogDensity: 0.0003,
			Ocean: voyageOcean(state, speed),
		},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels1440p, Effects: []scene.PostEffect{
			scene.Bloom{Mode: "mip", Threshold: 1.6, Strength: 0.08, Radius: 5, Scale: 0.5},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 0.5},
			scene.FXAA{},
		}},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels2048},
		Graph:   scene.NewGraph(nodes...),
	}
}

// voyagePresentation declares the controls' contract: every (sea state, game
// speed) pair is a period whose command replaces the environment, so any pair
// can follow any other and a transition never depends on where it started.
func voyagePresentation(viewID, periodID string) *scene.Presentation {
	config := &scene.Presentation{InputSignal: "$voyage.presentation", OutputSignal: "$voyage.presentationState", InitialView: viewID, InitialPeriod: periodID, Query: true}
	for _, v := range voyageViews {
		config.Views = append(config.Views, scene.PresentationView{ID: v.ID, Label: v.Name, Camera: scene.CameraIR(voyageCamera(v))})
	}
	for _, state := range voyageStates {
		for _, speed := range voyageSpeeds {
			props := voyageProps(viewID, voyagePeriodID(state, speed))
			config.Periods = append(config.Periods, scene.PresentationPeriod{
				ID: voyagePeriodID(state, speed), Hour: 12,
				Commands: []scene.Command{scene.SetSceneEnvironmentCommand(props.SceneIR().Environment)},
			})
		}
	}
	return config
}

// VoyageProgram is the full scene for a view and a (sea state, speed) period.
func VoyageProgram(viewID, periodID string) scene.Props {
	view := voyageViewFor(viewID)
	state, speed := voyageParsePeriod(periodID)
	props := voyageProps(view.ID, voyagePeriodID(state, speed))
	props.Presentation = voyagePresentation(view.ID, voyagePeriodID(state, speed))
	return props
}

func voyageLabel(state string, speed int) string {
	return fmt.Sprintf("%s sea, %dx", state, speed)
}
