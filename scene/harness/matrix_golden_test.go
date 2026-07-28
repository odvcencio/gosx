package harness_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/imagediff"
	"m31labs.dev/gosx/scene/preview"
)

// This file is the golden-frame matrix for the browser-free renderer.
//
// It walks what the public scene API can express: every geometry the CPU
// rasterizer builds, every material style that reaches a pixel, every light
// term, shadows on against off, instancing, and the post-effect chain. Each case
// renders one small deterministic frame, gates it on real content, and pins its
// raw-pixel hash.
//
// Two traps shaped the design, and both have already caught this repository.
//
// First, a golden frame of a blank image passes forever. So every case carries
// floors on coverage, unique colours and luminance variance, and those floors
// live in the case table, not in the golden file. Regenerating the goldens
// cannot turn a blank frame into a pass.
//
// Second, a hash says a frame changed and nothing more. So each case also stores
// a reference PNG, and the run compares against it through
// Session.RequireGoldenFile, which reports the changed-pixel count, the largest
// channel delta and the bounding box of the change.
//
// Regenerate the fixtures with:
//
//	go test ./scene/harness/ -run TestGoldenMatrix -update
//
// Say in the commit message which rendering change made the update necessary.

var updateGoldens = flag.Bool("update", false,
	"rewrite the golden matrix fixtures from the current renderer output")

const (
	matrixGoldenDir  = "testdata/matrix"
	matrixMetricPath = "testdata/matrix_metrics.json"
	// matrixWidth and matrixHeight keep every fixture small. A 96x64 frame
	// catches a shading, winding or transform regression, and a PNG of one costs
	// about a kilobyte.
	matrixWidth  = 96
	matrixHeight = 64
)

// matrixFloors are the minimum metrics a case must clear. All three are reported
// on failure, because each one catches a different loss:
//
//   - coverage catches a dropped draw or a broken transform.
//   - colors catches lost shading. A faceted solid shows one colour per visible
//     face; a curved body shades per pixel and shows hundreds.
//   - variance catches a flat fill that keeps the silhouette and loses the light.
type matrixFloors struct {
	coverage float64
	colors   int
	variance float64
}

// matrixCase is one pinned frame.
//
// A case names either typed props or a SceneIR document. Both reach the same CPU
// rasterizer. A document is the only way to reach a geometry that the typed Go
// API cannot express, and the cone is one: package scene has no ConeGeometry, and
// CylinderGeometry{RadiusTop: 0} lowers to kind "cylinder" with radiusTop zero,
// which geom.Normalize promotes back to one.
type matrixCase struct {
	name     string
	props    scene.Props
	document string
	opts     preview.Options
	floors   matrixFloors
	// time is the animation time the frame renders at.
	time float64
}

// session builds the harness session for one case.
func (tc matrixCase) session(t *testing.T) *harness.Session {
	t.Helper()
	if tc.document == "" {
		return harness.New(tc.props, tc.opts)
	}
	session, err := harness.NewFromJSON([]byte(tc.document), tc.opts)
	if err != nil {
		t.Fatalf("%s: %v", tc.name, err)
	}
	return session
}

// matrixMetrics is the recorded telemetry for one case. Storing the metrics next
// to the hash makes a regression readable: the hash says "changed" and the
// metrics say "the shading collapsed" or "the silhouette moved".
type matrixMetrics struct {
	PixelSHA256       string  `json:"pixelSHA256"`
	Coverage          float64 `json:"coverage"`
	UniqueColors      int     `json:"uniqueColors"`
	LuminanceVariance float64 `json:"luminanceVariance"`
	EdgeEnergy        float64 `json:"edgeEnergy"`
	Batches           int     `json:"batches"`
	Materials         int     `json:"materials"`
}

func matrixCamera(position scene.Vector3, rotation scene.Euler) *scene.PerspectiveCamera {
	return &scene.PerspectiveCamera{Position: position, Rotation: rotation, FOV: 55, Near: 0.1, Far: 60}
}

func matrixOptions() preview.Options {
	return preview.Options{
		Width: matrixWidth, Height: matrixHeight, Background: "#000000",
		Camera: matrixCamera(scene.Vec3(0, 1.2, 5.5), scene.Euler{}),
		// Shadows and post effects stay off unless a case asks for them, so an
		// unrelated case never pays for a shadow-map rasterization.
		DisableShadows: true, DisablePostFX: true,
	}
}

// keyLight is the one light the CPU rasterizer reads. Every geometry and material
// case uses it, so a lighting change shows up in the whole matrix at once.
func keyLight() scene.DirectionalLight {
	return scene.DirectionalLight{ID: "key", Intensity: 1.2, Direction: scene.Vec3(-0.4, -1, -0.3)}
}

func matrixMesh(geometry scene.Geometry, material scene.Material) scene.Mesh {
	return scene.Mesh{ID: "probe", Geometry: geometry, Material: material}
}

// matrixEnvironment authors every environment term explicitly, including the two
// hemisphere intensities.
//
// Do not let a fixture fall back to a default here. resolveHemisphereAmbient in
// render/bundle substitutes a sky and ground intensity of one when either is
// unset, while both browser renderers default to zero, so an unauthored scene
// gets a bright dome natively and none in the browser. That default is under
// dispute. A golden of an unauthored scene would pin the disputed value and make
// it permanent.
func matrixEnvironment() scene.Environment {
	return scene.Environment{
		AmbientColor: "#404858", AmbientIntensity: 0.35,
		SkyColor: "#ccddff", SkyIntensity: 0.6,
		GroundColor: "#483c38", GroundIntensity: 0.4,
	}
}

func matrixProps(nodes ...scene.Node) scene.Props {
	return scene.Props{
		Background:  "#000000",
		Environment: matrixEnvironment(),
		Graph:       scene.Graph{Nodes: nodes},
	}
}

// geometryCases pins one frame per primitive the CPU rasterizer builds. The
// floors differ because a faceted solid and a curved body shade differently, and
// one shared floor would either accept a flat sphere or reject a real plane.
func geometryCases() []matrixCase {
	standard := scene.StandardMaterial{Color: "#ff8060", Roughness: 0.5}
	cases := []struct {
		name     string
		geometry scene.Geometry
		floors   matrixFloors
	}{
		{"geom-box", scene.BoxGeometry{Width: 1.8, Height: 1.8, Depth: 1.8}, matrixFloors{0.06, 3, 0.004}},
		{"geom-cube", scene.CubeGeometry{Size: 1.6}, matrixFloors{0.04, 3, 0.003}},
		{"geom-plane", scene.PlaneGeometry{Width: 1.8, Height: 1.8}, matrixFloors{0.01, 2, 0.004}},
		{"geom-pyramid", scene.PyramidGeometry{Width: 1.8, Height: 1.8, Depth: 1.8}, matrixFloors{0.03, 3, 0.007}},
		{"geom-sphere", scene.SphereGeometry{Radius: 1.1, Segments: 24}, matrixFloors{0.05, 80, 0.007}},
		{"geom-cylinder", scene.CylinderGeometry{RadiusTop: 0.5, RadiusBottom: 1, Height: 1.8, Segments: 24}, matrixFloors{0.04, 60, 0.006}},
		{"geom-torus", scene.TorusGeometry{Radius: 1, Tube: 0.34, RadialSegments: 24, TubularSegments: 16}, matrixFloors{0.03, 80, 0.008}},
		{"geom-torusknot", scene.TorusKnotGeometry{Radius: 1, Tube: 0.34, RadialSegments: 12, TubularSegments: 64}, matrixFloors{0.10, 150, 0.014}},
	}
	out := make([]matrixCase, 0, len(cases)+1)
	for _, tc := range cases {
		out = append(out, matrixCase{
			name:   tc.name,
			props:  matrixProps(matrixMesh(tc.geometry, standard), keyLight()),
			opts:   matrixOptions(),
			floors: tc.floors,
		})
	}
	// The cone reaches the matrix only through a document. Package scene names no
	// ConeGeometry, so the typed API cannot ask for the primitive that
	// render/bundle already builds. Losing the cone from the matrix would leave a
	// working generator with no pinned frame.
	out = append(out, matrixCase{
		name: "geom-cone",
		document: `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"probe","kind":"cone",` +
			`"radiusBottom":1,"height":1.8,"segments":24,"color":"#ff8060","roughness":0.5}],` +
			`"lights":[{"id":"key","kind":"directional","directionX":-0.4,"directionY":-1,` +
			`"directionZ":-0.3,"intensity":1.2}],` +
			`"environment":{"ambientColor":"#404858","ambientIntensity":0.35,` +
			`"skyColor":"#ccddff","skyIntensity":0.6,"groundColor":"#483c38","groundIntensity":0.4}}`,
		opts:   matrixOptions(),
		floors: matrixFloors{0.03, 20, 0.005},
	})
	return out
}

// materialCases pin only the material axes the CPU shading model implements.
//
// The model is one Lambert expression in sceneLighting.shade at device.go:1915:
// base * (ambient*hemi + lightColor*NdotL*shadow). It reads base colour, opacity,
// the emissive scale and one base colour texture. It reads no roughness, no
// metalness, no clear coat, no sheen, no transmission, no iridescence, no
// anisotropy and no map other than base colour.
//
// So a golden frame per physically-based material would be an inert oracle: the
// hash would never move when the material feature moved. Those fields are pinned
// as identities instead, by TestPhysicallyBasedMaterialFieldsAreInvisible in
// package render/gpu/headless. Only the axes below are pinned as pixels.
//
// mat-emissive is included with a warning attached. The rasterizer's emissive is
// not an emitter: it multiplies the base colour into the albedo, which the light
// then modulates, so an unlit face stays black at any strength.
// TestEmissiveIsAnAlbedoMultiplierNotAnEmitter measures that relation exactly.
func materialCases() []matrixCase {
	opacity := func(value float64) *float64 { return &value }
	sphere := scene.SphereGeometry{Radius: 1.3, Segments: 24}
	cases := []struct {
		name     string
		material scene.Material
		floors   matrixFloors
	}{
		{"mat-color", scene.StandardMaterial{Color: "#4488ff", Roughness: 0.5}, matrixFloors{0.05, 80, 0.005}},
		{"mat-opacity-alpha", scene.StandardMaterial{Color: "#4488ff", Opacity: opacity(0.35), BlendMode: scene.BlendAlpha}, matrixFloors{0.05, 40, 0.001}},
		{"mat-opacity-additive", scene.StandardMaterial{Color: "#4488ff", Opacity: opacity(0.5), BlendMode: scene.BlendAdditive}, matrixFloors{0.05, 40, 0.002}},
		{"mat-emissive", scene.StandardMaterial{Color: "#4488ff", Emissive: 1}, matrixFloors{0.05, 80, 0.006}},
	}
	out := make([]matrixCase, 0, len(cases))
	for _, tc := range cases {
		out = append(out, matrixCase{
			name:   tc.name,
			props:  matrixProps(matrixMesh(sphere, tc.material), keyLight()),
			opts:   matrixOptions(),
			floors: tc.floors,
		})
	}
	return out
}

// lightingCases pin the light terms the rasterizer reads. The absent-light case
// matters: the renderer falls back to a built-in key light, so a scene with no
// light must still show a shaded body.
func lightingCases() []matrixCase {
	sphere := matrixMesh(scene.SphereGeometry{Radius: 1.3, Segments: 24},
		scene.StandardMaterial{Color: "#cccccc", Roughness: 0.6})
	withEnvironment := func(env scene.Environment, nodes ...scene.Node) scene.Props {
		props := matrixProps(nodes...)
		props.Environment = env
		return props
	}
	return []matrixCase{
		{
			name:   "light-directional-down",
			props:  matrixProps(sphere, scene.DirectionalLight{ID: "key", Intensity: 1.2, Direction: scene.Vec3(0, -1, 0)}),
			opts:   matrixOptions(),
			floors: matrixFloors{0.05, 60, 0.004},
		},
		{
			name:  "light-directional-side",
			props: matrixProps(sphere, scene.DirectionalLight{ID: "key", Intensity: 1.2, Direction: scene.Vec3(1, -0.2, 0.5)}),
			opts:  matrixOptions(),
			// A light that grazes the visible hemisphere leaves most of it on the
			// ambient term alone, so the variance floor sits lower than the
			// overhead case. The colour floor is the real gate here.
			floors: matrixFloors{0.05, 60, 0.003},
		},
		{
			// No authored light. resolveDirectionalLight falls back to a built-in
			// key light, so a frame here proves the fallback still shades.
			name:   "light-absent",
			props:  matrixProps(sphere),
			opts:   matrixOptions(),
			floors: matrixFloors{0.05, 60, 0.004},
		},
		{
			// A strong red ambient term. Every other term keeps its authored
			// matrix value, so the frame isolates the ambient contribution.
			name: "env-ambient",
			props: withEnvironment(scene.Environment{
				AmbientColor: "#ff2200", AmbientIntensity: 0.9,
				SkyColor: "#ccddff", SkyIntensity: 0.6,
				GroundColor: "#483c38", GroundIntensity: 0.4},
				sphere, keyLight()),
			opts:   matrixOptions(),
			floors: matrixFloors{0.05, 60, 0.004},
		},
		{
			// A red sky over a blue ground. The dome now dominates, so this frame
			// is the one that moves most if the three ambient terms stop being
			// summed independently.
			name: "env-hemisphere",
			props: withEnvironment(scene.Environment{
				AmbientColor: "#404858", AmbientIntensity: 0.35,
				SkyColor: "#ff0000", SkyIntensity: 1,
				GroundColor: "#0000ff", GroundIntensity: 1},
				sphere, keyLight()),
			opts:   matrixOptions(),
			floors: matrixFloors{0.05, 60, 0.004},
		},
	}
}

// shadowScene builds a wide ground plane with three cubes above it, lit straight
// down, and turns the whole scene by a heading. This is the shape the shadow path
// needs, and it reproduces two historical defects at once.
//
// A plane wide enough to receive a shadow always has a corner behind the camera.
// The rasterizer used to drop any triangle with a vertex behind the camera, so no
// ground plane ever appeared in a headless frame and no test could witness a
// shadow landing on one.
//
// The scene turns with the camera, so every heading must produce the same picture
// up to rotation. The old cascade fit ignored the camera rotation and produced
// shadows only near heading zero.
func shadowScene(headingRadians float64) scene.Props {
	sin, cos := math.Sin(headingRadians), math.Cos(headingRadians)
	// forward is the ground direction the camera looks along. A heading of h
	// turns the view axis from -Z toward +X.
	forwardX, forwardZ := sin, -cos
	rightX, rightZ := cos, sin
	groundX, groundZ := forwardX*8, forwardZ*8

	nodes := []scene.Node{
		scene.Mesh{ID: "ground", Position: scene.Vec3(groundX, 0, groundZ),
			Geometry:      scene.PlaneGeometry{Width: 40, Height: 40},
			Material:      scene.StandardMaterial{Color: "#8a8a8a", Roughness: 0.9},
			ReceiveShadow: true},
	}
	for index, offset := range []float64{-3, 0, 3} {
		nodes = append(nodes, scene.Mesh{
			ID:         fmt.Sprintf("caster%d", index),
			Position:   scene.Vec3(groundX+rightX*offset, 2, groundZ+rightZ*offset),
			Geometry:   scene.CubeGeometry{Size: 1.6},
			Material:   scene.StandardMaterial{Color: "#8a8a8a", Roughness: 0.9},
			CastShadow: true,
		})
	}
	nodes = append(nodes, scene.DirectionalLight{
		ID: "sun", Intensity: 1, Direction: scene.Vec3(0, -1, 0), CastShadow: true,
	})
	return matrixProps(nodes...)
}

// shadowOptions places the camera five units up and tilts it down so the ground
// fills the frame. atan(5/8) centres the view on the ground eight units ahead.
func shadowOptions(headingRadians float64, disableShadows bool) preview.Options {
	opts := matrixOptions()
	opts.Camera = matrixCamera(scene.Vec3(0, 5, 0), scene.Euler{X: 0.5586, Y: headingRadians})
	opts.DisableShadows = disableShadows
	return opts
}

// rasterPathCases pin the geometry paths where the recent defects lived: the near
// plane, the depth test, and instanced transforms.
func rasterPathCases() []matrixCase {
	grey := scene.StandardMaterial{Color: "#8a8a8a", Roughness: 0.9}
	// A plane wide enough to reach behind the camera. Without near-plane clipping
	// the whole plane disappears, so the coverage floor alone rejects that defect.
	nearClip := matrixProps(
		scene.Mesh{ID: "floor", Position: scene.Vec3(0, 0, 0), Geometry: scene.PlaneGeometry{Width: 80, Height: 80},
			Material: grey},
		scene.DirectionalLight{ID: "sun", Intensity: 1, Direction: scene.Vec3(0, -1, 0)})
	nearClipOptions := matrixOptions()
	nearClipOptions.Camera = matrixCamera(scene.Vec3(0, 2, 0), scene.Euler{X: 0.35})

	// Two overlapping cubes at different depths. The near one must win, so a lost
	// depth test moves the pixels and the hash.
	// Both cubes sit at the camera's eye height, so the middle pixel of the frame
	// lands inside the overlap and TestDepthOrderPutsTheNearSurfaceInFront can
	// read the winner from one sample.
	// The near cube is declared first, so draw order alone would let the far cube
	// paint over it. Only a working depth test keeps the near surface in front.
	depth := matrixProps(
		scene.Mesh{ID: "near", Position: scene.Vec3(-0.6, 1.2, 1),
			Geometry: scene.CubeGeometry{Size: 2}, Material: scene.StandardMaterial{Color: "#c02040"}},
		scene.Mesh{ID: "far", Position: scene.Vec3(0.6, 1.2, -1),
			Geometry: scene.CubeGeometry{Size: 2}, Material: scene.StandardMaterial{Color: "#20c040"}},
		keyLight())

	return []matrixCase{
		{name: "raster-near-plane-ground", props: nearClip, opts: nearClipOptions,
			floors: matrixFloors{0.30, 2, 0.02}},
		// Two flat-shaded cubes hold little luminance spread, so the colour count
		// and the coverage are the gates. TestDepthOrderPutsTheNearSurfaceInFront
		// checks the ordering itself at the overlap.
		{name: "raster-depth-order", props: depth, opts: matrixOptions(),
			floors: matrixFloors{0.15, 3, 0.002}},
	}
}

func sceneCases() []matrixCase {
	instanced := scene.InstancedMesh{
		ID:       "grid",
		Count:    4,
		Geometry: scene.CubeGeometry{Size: 0.6},
		Material: scene.StandardMaterial{Color: "#ffffff"},
		Positions: []scene.Vector3{
			scene.Vec3(-1, 0.6, 0), scene.Vec3(1, 0.6, 0),
			scene.Vec3(-1, -0.6, 0), scene.Vec3(1, -0.6, 0),
		},
		Colors: []string{"#ff0000", "#00ff00", "#0000ff", "#ffff00"},
	}
	postFXProps := matrixProps(
		matrixMesh(scene.SphereGeometry{Radius: 1.3, Segments: 24},
			scene.StandardMaterial{Color: "#ffeeaa", Emissive: 1}),
		keyLight())
	postFXProps.PostFX = scene.PostFX{Effects: []scene.PostEffect{
		scene.Bloom{Threshold: 0.4, Strength: 1.2, Radius: 6},
		scene.SSAO{Radius: 4, Intensity: 1.2},
		scene.DOF{FocusDistance: 6, Aperture: 0.1, MaxBlur: 6},
		scene.Vignette{Intensity: 1},
		scene.ColorGrade{Exposure: 1.3, Contrast: 1.4, Saturation: 1.6},
		scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.2},
	}}
	postFXOptions := matrixOptions()
	postFXOptions.DisablePostFX = false

	return []matrixCase{
		{
			name: "instanced-colors", props: matrixProps(instanced, keyLight()),
			opts: matrixOptions(), floors: matrixFloors{0.04, 5, 0.006},
		},
		{
			name: "shadow-on", props: shadowScene(0),
			opts: shadowOptions(0, false), floors: matrixFloors{0.5, 4, 0.025},
		},
		{
			name: "shadow-off", props: shadowScene(0),
			opts: shadowOptions(0, true), floors: matrixFloors{0.5, 3, 0.025},
		},
		{
			// A quarter turn. The scene turns with the camera, so the picture must
			// hold the same amount of shadow. The rotation-blind cascade fit lost
			// most of it away from heading zero.
			name: "shadow-on-heading-quarter", props: shadowScene(math.Pi / 2),
			opts: shadowOptions(math.Pi/2, false), floors: matrixFloors{0.5, 4, 0.025},
		},
		{
			// An off-axis heading, where a fit that snaps to an axis goes wrong in
			// a different way from a quarter turn.
			name: "shadow-on-heading-oblique", props: shadowScene(2.3),
			opts: shadowOptions(2.3, false), floors: matrixFloors{0.5, 4, 0.025},
		},
		{
			// The whole native post-effect chain. On the CPU path each pass is an
			// identity copy, so this frame must match the frame with no chain at
			// all. It is the guard that caught a vignette erasing the whole image.
			name: "postfx-chain", props: postFXProps,
			opts: postFXOptions, floors: matrixFloors{0.05, 32, 0.006},
		},
	}
}

func matrixCases() []matrixCase {
	var out []matrixCase
	out = append(out, geometryCases()...)
	out = append(out, materialCases()...)
	out = append(out, lightingCases()...)
	out = append(out, rasterPathCases()...)
	out = append(out, sceneCases()...)
	return out
}

// TestGoldenMatrix renders every case, gates it on real content, and pins both
// the raw-pixel hash and the reference PNG.
func TestGoldenMatrix(t *testing.T) {
	cases := matrixCases()
	stored := readMatrixMetrics(t)
	recorded := map[string]matrixMetrics{}

	for _, tc := range cases {
		if _, duplicate := recorded[tc.name]; duplicate {
			t.Fatalf("two cases share the name %q, so one golden would overwrite the other", tc.name)
		}
		recorded[tc.name] = matrixMetrics{}
		t.Run(tc.name, func(t *testing.T) {
			session := tc.session(t)
			result, err := session.Render(tc.time)
			if err != nil {
				t.Fatal(err)
			}
			frame := lastFrameTelemetry(t, session)
			metrics := matrixMetrics{
				PixelSHA256: frame.PixelHash, Coverage: frame.Coverage,
				UniqueColors: frame.UniqueColors, LuminanceVariance: frame.LuminanceVariance,
				EdgeEnergy: frame.EdgeEnergy, Batches: frame.Batches, Materials: frame.Materials,
			}
			recorded[tc.name] = metrics

			// Report every metric on failure. A gate that checks one and stops
			// hides the others, and a hash alone never says what was lost.
			t.Logf("%s: coverage %.6f, unique colours %d, luminance variance %.6f, edge energy %.6f, batches %d",
				tc.name, metrics.Coverage, metrics.UniqueColors, metrics.LuminanceVariance,
				metrics.EdgeEnergy, metrics.Batches)
			if metrics.Coverage < tc.floors.coverage ||
				metrics.UniqueColors < tc.floors.colors ||
				metrics.LuminanceVariance < tc.floors.variance {
				t.Fatalf("%s lost content: coverage %.6f (floor %.6f), unique colours %d (floor %d), "+
					"luminance variance %.6f (floor %.6f)",
					tc.name, metrics.Coverage, tc.floors.coverage, metrics.UniqueColors, tc.floors.colors,
					metrics.LuminanceVariance, tc.floors.variance)
			}
			if frame.DeviceLost {
				t.Fatal("the headless device reported loss")
			}

			path := filepath.Join(matrixGoldenDir, tc.name+".png")
			if *updateGoldens {
				writeGoldenPNG(t, path, result)
				return
			}
			// RequireGoldenFile localizes the change: it reports the changed-pixel
			// count, the largest channel delta, and the bounding box.
			if _, err := session.RequireGoldenFile(tc.name, path, imagediff.Options{}); err != nil {
				t.Fatalf("golden %s: %v", path, err)
			}
			if err := session.Validate(); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			want, ok := stored[tc.name]
			if !ok {
				t.Fatalf("%s has no pinned metrics; run the test with -update and review the diff", tc.name)
			}
			if metrics != want {
				t.Fatalf("%s metrics moved:\n got %+v\nwant %+v", tc.name, metrics, want)
			}
		})
	}

	if *updateGoldens {
		writeMatrixMetrics(t, recorded)
		t.Log("golden matrix fixtures rewritten; review the diff before committing")
		return
	}
	// A stale entry means a case was renamed or removed and its fixture stayed.
	// Silence here would leave a dead PNG that nobody ever checks again.
	for name := range stored {
		if _, live := recorded[name]; !live {
			t.Errorf("the golden matrix pins %q, but no case renders it; run with -update to drop it", name)
		}
	}
}

// TestPostFXChainStatesThePostEffectGap states the post-effect gap exactly.
//
// The CPU path used to implement no screen-space effect at all, so this test
// used to require the chained frame to match the unchained one. It now runs
// bloom, tone mapping, vignette and colour grading, so the two frames must
// differ. Ambient occlusion, depth of field and every Selena custom pass still
// copy, so the difference comes from four passes and not six.
//
// The test fails in both directions that matter: a pass that erases the frame,
// and a chain that stops reaching the pixels at all.
func TestPostFXChainStatesThePostEffectGap(t *testing.T) {
	var chain matrixCase
	for _, tc := range sceneCases() {
		if tc.name == "postfx-chain" {
			chain = tc
		}
	}
	if chain.name == "" {
		t.Fatal("the postfx-chain case disappeared from the matrix")
	}

	withChain := chain.session(t)
	if _, err := withChain.Render(0); err != nil {
		t.Fatal(err)
	}
	bare := chain
	bare.props.PostFX = scene.PostFX{}
	withoutChain := bare.session(t)
	if _, err := withoutChain.Render(0); err != nil {
		t.Fatal(err)
	}

	withFrame := lastFrameTelemetry(t, withChain)
	withoutFrame := lastFrameTelemetry(t, withoutChain)
	// 32 -> 120: the chain gained four real passes, and a vignette alone puts a
	// smooth ramp across the frame. Measured 208 colours.
	if withFrame.UniqueColors < 120 || withFrame.LuminanceVariance < 0.006 {
		t.Fatalf("the chained frame holds no content: %d colours, variance %.6f",
			withFrame.UniqueColors, withFrame.LuminanceVariance)
	}
	if withFrame.PixelHash == withoutFrame.PixelHash {
		t.Fatalf("the post-effect chain left every pixel untouched (hash %s).\n"+
			"Four of its passes run on this backend, so the chain must move the frame. "+
			"Either the label routing stopped reaching the fragment stages, or the chain was dropped.",
			withFrame.PixelHash)
	}
	// The chain must not swallow the frame. A pass that clears its target and
	// draws nothing was the original defect here, and it presents flat black.
	if withFrame.Coverage < withoutFrame.Coverage*0.5 {
		t.Fatalf("the chain cut coverage from %.6f to %.6f; a pass is erasing the frame",
			withoutFrame.Coverage, withFrame.Coverage)
	}
}

// TestShadowsChangeTheFrame proves the shadow axis of the matrix is real. Two
// pinned hashes that happened to be equal would pass the golden test and say
// nothing about shadows.
func TestShadowsChangeTheFrame(t *testing.T) {
	var on, off matrixCase
	for _, tc := range sceneCases() {
		switch tc.name {
		case "shadow-on":
			on = tc
		case "shadow-off":
			off = tc
		}
	}
	if on.name == "" || off.name == "" {
		t.Fatal("the shadow cases disappeared from the matrix")
	}

	shadedImage, shaded := renderMatrixImage(t, on)
	flatImage, flat := renderMatrixImage(t, off)
	if shaded.PixelHash == flat.PixelHash {
		t.Fatalf("shadows on and shadows off produced the same frame (%s); "+
			"the shadow pass contributes nothing", shaded.PixelHash)
	}
	// A shadow removes light, so the shaded frame must be darker on average.
	//
	// This check used to compare luminance variance instead, which the comment
	// beside it never claimed. Variance is the wrong proxy here: the shadow
	// patch covers a small part of a frame whose spread is dominated by the
	// ground against the background, and moving that patch toward the mean can
	// lower the variance. Once the present pass started running the ACES curve,
	// the shadowed frame measured 0.040240 against 0.040401 flat, and a check
	// that never matched its own claim failed. Mean luminance is the direct
	// consequence of removing light and cannot go the other way.
	shadedMean := meanLuminance(shadedImage)
	flatMean := meanLuminance(flatImage)
	if shadedMean >= flatMean {
		t.Fatalf("the shadowed frame is not darker than the flat one: mean luminance %.6f against %.6f",
			shadedMean, flatMean)
	}
	// A shadow adds a light level, so it adds colours. This half rejects a
	// change that darkened the whole frame instead of painting a patch.
	if shaded.UniqueColors <= flat.UniqueColors {
		t.Fatalf("the shadowed frame holds %d colours and the flat one holds %d; "+
			"a shadow must add a level, not dim the whole frame",
			shaded.UniqueColors, flat.UniqueColors)
	}
}

// renderMatrixImage renders one case and returns both the image and the frame
// telemetry. renderMatrixFrame returns the telemetry alone, and a mean over the
// pixels needs the image.
func renderMatrixImage(t *testing.T, tc matrixCase) (*image.RGBA, harness.FrameTelemetry) {
	t.Helper()
	session := tc.session(t)
	result, err := session.Render(tc.time)
	if err != nil {
		t.Fatal(err)
	}
	return result.Image, lastFrameTelemetry(t, session)
}

// meanLuminance returns the average Rec.709 luminance of an image, on a zero to
// one scale.
func meanLuminance(img *image.RGBA) float64 {
	bounds := img.Bounds()
	count := float64(bounds.Dx() * bounds.Dy())
	if count == 0 {
		return 0
	}
	sum := 0.0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			sum += 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
		}
	}
	return sum / count / 255
}

// TestShadowAreaHoldsAcrossHeadings proves the shadow cases measure the cascade
// fit and not a lucky camera angle.
//
// The scene turns with the camera, so a correct fit paints the same amount of
// shadow at every heading. The rotation-blind fit produced shadows only near
// heading zero, and every test in the repository passed.
func TestShadowAreaHoldsAcrossHeadings(t *testing.T) {
	reference := shadowedPixelCount(t, 0)
	if reference == 0 {
		t.Fatal("heading zero painted no shadow, so this test cannot measure the fit")
	}
	for _, heading := range []float64{math.Pi / 2, math.Pi, -math.Pi / 2, 2.3} {
		got := shadowedPixelCount(t, heading)
		if got == 0 {
			t.Errorf("heading %.2f painted no shadow; the cascade fit ignores the camera heading", heading)
			continue
		}
		// The slack covers rasterization only. An off-axis heading moves the
		// shadow edge by a few pixels. The rotation-blind fit lost a quarter of
		// the shadow at heading pi, so this bound rejects it with room to spare.
		if limit := float64(reference)*0.05 + 12; math.Abs(float64(got-reference)) > limit {
			t.Errorf("heading %.2f shadowed %d pixels, heading zero shadowed %d; the fits disagree",
				heading, got, reference)
		}
	}
}

// shadowedPixelCount renders the shadow scene at one heading and counts pixels
// that are lit but clearly darker than the fully lit ground. The ground is mid
// grey and rough, so a shadowed sample keeps only its ambient term.
func shadowedPixelCount(t *testing.T, heading float64) int {
	t.Helper()
	result, err := preview.Render(shadowScene(heading), shadowOptions(heading, false))
	if err != nil {
		t.Fatal(err)
	}
	shadowed := 0
	bounds := result.Image.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := result.Image.RGBAAt(x, y)
			if sum := int(c.R) + int(c.G) + int(c.B); sum >= 24 && sum < 300 {
				shadowed++
			}
		}
	}
	return shadowed
}

// TestDepthOrderPutsTheNearSurfaceInFront reads the overlap directly. The golden
// hash for raster-depth-order would move if the ordering flipped, but it would
// not say which surface won, and the depth test is one of the paths where a
// defect made a whole class of output invisible.
func TestDepthOrderPutsTheNearSurfaceInFront(t *testing.T) {
	var depth matrixCase
	for _, tc := range rasterPathCases() {
		if tc.name == "raster-depth-order" {
			depth = tc
		}
	}
	if depth.name == "" {
		t.Fatal("the raster-depth-order case disappeared from the matrix")
	}
	result, err := preview.Render(depth.props, depth.opts)
	if err != nil {
		t.Fatal(err)
	}
	// The near cube is red and sits left of centre; the far cube is green and sits
	// right of centre. Their overlap covers the middle column, so the middle pixel
	// must be red.
	bounds := result.Image.Bounds()
	centre := result.Image.RGBAAt(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2)
	if centre.R <= centre.G {
		t.Fatalf("the far green cube won the overlap: %+v; the near red cube must win the depth test", centre)
	}
}

func renderMatrixFrame(t *testing.T, tc matrixCase) harness.FrameTelemetry {
	t.Helper()
	session := tc.session(t)
	if _, err := session.Render(tc.time); err != nil {
		t.Fatal(err)
	}
	return lastFrameTelemetry(t, session)
}

func lastFrameTelemetry(t *testing.T, session *harness.Session) harness.FrameTelemetry {
	t.Helper()
	report := session.Report()
	for index := len(report.Events) - 1; index >= 0; index-- {
		if report.Events[index].Frame != nil {
			return *report.Events[index].Frame
		}
	}
	t.Fatal("the session recorded no frame")
	return harness.FrameTelemetry{}
}

func writeGoldenPNG(t *testing.T, path string, result *preview.Result) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := preview.WritePNG(file, result); err != nil {
		t.Fatal(err)
	}
}

func readMatrixMetrics(t *testing.T) map[string]matrixMetrics {
	t.Helper()
	data, err := os.ReadFile(matrixMetricPath)
	if err != nil {
		if os.IsNotExist(err) && *updateGoldens {
			return map[string]matrixMetrics{}
		}
		t.Fatalf("read %s: %v; run the test with -update to create it", matrixMetricPath, err)
	}
	var out map[string]matrixMetrics
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %s: %v", matrixMetricPath, err)
	}
	return out
}

// writeMatrixMetrics writes the metric file with sorted keys and one entry per
// line group, so a regenerated fixture produces a readable diff.
func writeMatrixMetrics(t *testing.T, metrics map[string]matrixMetrics) {
	t.Helper()
	names := make([]string, 0, len(metrics))
	for name := range metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make(map[string]matrixMetrics, len(names))
	for _, name := range names {
		ordered[name] = metrics[name]
	}
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(matrixMetricPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(matrixMetricPath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
