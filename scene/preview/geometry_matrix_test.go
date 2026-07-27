package preview_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/geom"
	"m31labs.dev/gosx/scene/preview"
)

// This file enumerates every geometry the public scene API can express and
// records which ones reach a CPU pixel.
//
// The rule is one-sided in both directions, and both directions have failed
// here. A kind that renders must not be reported unsupported, or the author
// deletes working geometry. A kind that renders nothing must carry a diagnostic,
// or the author gets a blank PNG and a success exit code.

// matrixWidth and matrixHeight size every probe frame in this file. A 96x64
// frame is enough to separate a solid from a blank frame and costs little.
const (
	matrixWidth  = 96
	matrixHeight = 64
)

// geomGenerator names one generator that package scene/geom builds from a
// Params.Kind, together with the floors its frame must clear.
//
// The three floors catch three different regressions, so every case states all
// three and the test reports all three on failure:
//
//   - minCoverage catches a lost silhouette, which is a dropped draw or a bad
//     transform.
//   - minColors catches lost shading. A faceted solid shows one colour per
//     visible face, so its floor is small. A curved body shades per pixel, so
//     its floor is large; a fall to a handful of colours means the shading went
//     per-vertex or per-face.
//   - minVariance catches a flat fill. A silhouette painted one constant colour
//     keeps its coverage and loses its variance.
type geomGenerator struct {
	kind        string
	minCoverage float64
	minColors   int
	minVariance float64
}

// geomGenerators lists every generator package scene/geom can build from a
// Params.Kind. Keep it in step with the geom.Kind constants;
// TestGeomGeneratorsAreAllListedAsRasterizable fails when it drifts.
//
// The floors sit near sixty percent of the measured values, which leaves room
// for a tessellation tweak and still rejects a lost feature.
//
// The cone and cylinder colour floors dropped on 2026-07-26, when the headless
// device started running the tone-map pass. The curve costs distinct colours,
// because the device holds its high dynamic range target in eight bits per
// channel and the curve then quantizes a second time. Measured on this probe:
// the cylinder fell from 105 colours to 56 and the cone from 84 to 50. The
// sphere, torus and torus knot lost the same fraction and still clear their
// floors. Raise all five again when the device holds that target in float.
var geomGenerators = []geomGenerator{
	{geom.KindBox, 0.06, 3, 0.004},
	{geom.KindCone, 0.03, 30, 0.008},
	{geom.KindCube, 0.04, 3, 0.003},
	{geom.KindCylinder, 0.04, 33, 0.006},
	{geom.KindPlane, 0.01, 2, 0.004},
	{geom.KindPyramid, 0.03, 3, 0.007},
	{geom.KindSphere, 0.05, 80, 0.007},
	{geom.KindTorus, 0.03, 80, 0.008},
	{geom.KindTorusKnot, 0.10, 150, 0.014},
}

// TestGeomGeneratorsAreAllListedAsRasterizable stops the preview coverage table
// from drifting behind the generator again.
//
// The torus knot sat outside the table while render/bundle drew it correctly.
// The preview answered CanRasterizeKind("torusknot") with false and attached a
// warning that said no backend draws the kind. Both statements were wrong, and
// an author who trusted them would have removed geometry that worked.
func TestGeomGeneratorsAreAllListedAsRasterizable(t *testing.T) {
	listed := map[string]bool{}
	for _, kind := range preview.RasterizableKinds() {
		listed[kind] = true
	}
	for _, generator := range geomGenerators {
		if !listed[generator.kind] {
			t.Errorf("package scene/geom builds %q but preview.RasterizableKinds() omits it, "+
				"so the preview will report working geometry as unsupported", generator.kind)
		}
		if !preview.CanRasterizeKind(generator.kind) {
			t.Errorf("preview.CanRasterizeKind(%q) = false, but geom.Build returns a mesh for it", generator.kind)
		}
	}
	for kind := range listed {
		if geom.NormalizeKind(kind) == "" {
			t.Errorf("preview lists %q as rasterizable, but geom.NormalizeKind rejects it, "+
				"so render/bundle builds nothing and the frame is silently empty", kind)
		}
	}
}

// TestEveryGeomGeneratorRendersPixels renders one mesh per generator and gates
// each frame on real content. A generator that stops drawing fails here instead
// of pinning an empty golden.
func TestEveryGeomGeneratorRendersPixels(t *testing.T) {
	for _, generator := range geomGenerators {
		t.Run(generator.kind, func(t *testing.T) {
			result, err := preview.RenderJSON([]byte(geomProbeDocument(generator.kind)), preview.Options{
				Width: matrixWidth, Height: matrixHeight, Background: "#000000",
				Camera: cameraAt(0, 1.2, 5.5), DisableShadows: true, DisablePostFX: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			// Report every metric. A gate that checks coverage and stops hides a
			// solid silhouette that lost all its shading.
			coverage, unique, variance := frameMetrics(result)
			t.Logf("kind %s: coverage %.4f, unique colours %d, luminance variance %.6f",
				generator.kind, coverage, unique, variance)
			if coverage < generator.minCoverage || unique < generator.minColors || variance < generator.minVariance {
				t.Fatalf("kind %q lost content: coverage %.4f (floor %.4f), unique colours %d (floor %d), "+
					"luminance variance %.6f (floor %.6f)",
					generator.kind, coverage, generator.minCoverage, unique, generator.minColors,
					variance, generator.minVariance)
			}
			for _, diagnostic := range result.Bundle.Diagnostics {
				if diagnostic.Code == "scene.preview.unsupported_geometry" {
					t.Fatalf("kind %q drew %.1f%% of the frame but was reported unsupported: %s",
						generator.kind, coverage*100, diagnostic.Message)
				}
			}
		})
	}
}

// geomProbeDocument builds one SceneIR document holding a single mesh of the
// named kind. It sets every dimension field, so one document serves every
// generator and each generator reads only the fields it needs.
func geomProbeDocument(kind string) string {
	return `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"probe","kind":"` + kind +
		`","size":1.6,"width":1.8,"height":1.8,"depth":1.8,"radius":1.1,"radiusTop":0.5,` +
		`"radiusBottom":1,"tube":0.34,"color":"#ff8060"}],` +
		`"lights":[{"id":"sun","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.2}]}`
}

// bufferGeometryGenerator names one public scene generator that lowers to a
// BufferGeometry, together with a call that builds a shape big enough to fill a
// visible part of the probe frame.
type bufferGeometryGenerator struct {
	name     string
	geometry scene.Geometry
}

func bufferGeometryGenerators() []bufferGeometryGenerator {
	square := scene.Shape{Outline: []float64{-1.2, -1.2, 1.2, -1.2, 1.2, 1.2, -1.2, 1.2}}
	return []bufferGeometryGenerator{
		{"PolyhedronGeometry", polyhedronProbe()},
		{"TetrahedronGeometry", scene.TetrahedronGeometry(1.5, 0)},
		{"OctahedronGeometry", scene.OctahedronGeometry(1.5, 0)},
		{"IcosahedronGeometry", scene.IcosahedronGeometry(1.5, 1)},
		{"DodecahedronGeometry", scene.DodecahedronGeometry(1.5, 0)},
		{"CircleGeometry", scene.CircleGeometry(1.5, 32, 0, 0)},
		{"RingGeometry", scene.RingGeometry(0.6, 1.5, 32, 1, 0, 0)},
		{"ShapeGeometry", scene.ShapeGeometry(square, 0)},
		{"ExtrudeGeometry", scene.ExtrudeGeometry(square, scene.ExtrudeOptions{Depth: 1.2})},
		{"PolygonGeometry", scene.PolygonGeometry([]float64{-1.2, -1.2, 1.2, -1.2, 1.2, 1.2, -1.2, 1.2}, nil, 0)},
	}
}

func polyhedronProbe() scene.Geometry {
	vertices, indices := geom.OctahedronHull()
	return scene.PolyhedronGeometry(vertices, indices, 1.5, 1)
}

// TestBufferGeometryGeneratorsAreHonestlyUnsupported records the whole
// BufferGeometry family as an admitted gap rather than a silent one.
//
// Every generator in scene/generators.go lowers to a "gltf-mesh" object that
// carries its vertices inline. engine.RenderInstancedMesh has no vertex buffer
// field, so those vertices never reach any native backend and the mesh draws
// nothing. The test demands the diagnostic, and it also fails when a generator
// starts drawing, because a drawing generator must then join the rasterizable
// table instead of keeping a warning that is no longer true.
func TestBufferGeometryGeneratorsAreHonestlyUnsupported(t *testing.T) {
	for _, generator := range bufferGeometryGenerators() {
		t.Run(generator.name, func(t *testing.T) {
			props := scene.Props{
				Background: "#000000",
				Camera:     *cameraAt(0, 1.6, 5.5),
				Graph: scene.Graph{Nodes: []scene.Node{
					scene.Mesh{ID: "probe", Geometry: generator.geometry,
						Material: scene.StandardMaterial{Color: "#ff8060"}},
					scene.DirectionalLight{Intensity: 1.2, Direction: scene.Vec3(-0.4, -1, -0.3)},
				}},
			}
			result, err := preview.Render(props, preview.Options{
				Width: matrixWidth, Height: matrixHeight,
				DisableShadows: true, DisablePostFX: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			coverage, unique, variance := frameMetrics(result)
			diagnostic, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.unsupported_geometry")
			if coverage > 0.001 {
				t.Fatalf("%s now draws %.2f%% of the frame with %d colours and variance %.6f; "+
					"add its kind to the rasterizable table and drop the unsupported warning",
					generator.name, coverage*100, unique, variance)
			}
			if !reported {
				t.Fatalf("%s drew no pixels and reported nothing; a silent blank frame is the failure this test exists to catch",
					generator.name)
			}
			if !strings.Contains(diagnostic.Message, "carries no vertex buffer field") {
				t.Fatalf("%s diagnostic does not name the real blocker: %s", generator.name, diagnostic.Message)
			}
		})
	}
}

// TestLineGeometryIsHonestlyUnsupported pins the other admitted geometry gap.
// rasterizeDraw serves the lit, unlit, shadow and particle pipelines, and skips
// bundle.worldLine, so every line list and every line-based helper draws nothing.
func TestLineGeometryIsHonestlyUnsupported(t *testing.T) {
	props := scene.Props{
		Background: "#000000",
		Camera:     *cameraAt(0, 1.6, 5.5),
		Graph: scene.Graph{Nodes: []scene.Node{
			scene.Mesh{ID: "wires", Material: scene.LineBasicMaterial{Width: 2},
				Geometry: scene.LinesGeometry{
					Points:   []scene.Vector3{scene.Vec3(-1.5, 0, 0), scene.Vec3(1.5, 0, 0), scene.Vec3(0, 1.5, 0)},
					Segments: [][2]int{{0, 1}, {1, 2}, {2, 0}},
				}},
		}},
	}
	result, err := preview.Render(props, preview.Options{
		Width: matrixWidth, Height: matrixHeight, DisableShadows: true, DisablePostFX: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	coverage, unique, variance := frameMetrics(result)
	if coverage > 0.001 {
		t.Fatalf("line geometry now draws %.2f%% of the frame with %d colours and variance %.6f; "+
			"rasterizeDraw gained the worldLine pipeline, so update the coverage table",
			coverage*100, unique, variance)
	}
	diagnostic, reported := findDiagnostic(result.Bundle.Diagnostics, "scene.preview.unsupported_geometry")
	if !reported || !strings.Contains(diagnostic.Message, "bundle.worldLine line-list pipeline") {
		t.Fatalf("line geometry drew nothing without naming the missing pipeline: %+v", result.Bundle.Diagnostics)
	}
}

// TestIgnoredLightKindsAreAllReported walks every light the typed scene package
// can emit. Only the first directional light reaches a CPU pixel, so each other
// kind must name itself in the frame.
func TestIgnoredLightKindsAreAllReported(t *testing.T) {
	props := scene.Props{
		Background: "#000000",
		Camera:     *cameraAt(0, 1.2, 5.5),
		Graph: scene.Graph{Nodes: []scene.Node{
			scene.Mesh{ID: "probe", Geometry: scene.SphereGeometry{Radius: 1.4},
				Material: scene.StandardMaterial{Color: "#cccccc"}},
			scene.DirectionalLight{ID: "key", Intensity: 1.2, Direction: scene.Vec3(-0.4, -1, -0.3)},
			scene.DirectionalLight{ID: "fill", Intensity: 0.4, Direction: scene.Vec3(1, -0.2, 0.5)},
			scene.AmbientLight{ID: "amb", Intensity: 0.3},
			scene.PointLight{ID: "lamp", Intensity: 3, Position: scene.Vec3(2, 4, 2)},
			scene.SpotLight{ID: "spot", Intensity: 3, Position: scene.Vec3(-2, 4, 2), Angle: 0.6},
			scene.HemisphereLight{ID: "hemi", Intensity: 1, SkyColor: "#88ccff", GroundColor: "#442200"},
			scene.RectAreaLight{ID: "panel", Intensity: 2, Width: 4, Height: 4},
		}},
	}
	result, err := preview.Render(props, preview.Options{
		Width: matrixWidth, Height: matrixHeight, DisableShadows: true, DisablePostFX: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ignored := map[string]string{}
	for _, diagnostic := range result.Bundle.Diagnostics {
		if diagnostic.Code == "scene.preview.unsupported_light" {
			ignored[diagnostic.Target] = diagnostic.Message
		}
	}
	// Only "panel", the rect-area light, is still reported. Every other kind now
	// shades through the runtime light loop, so this list shrank from six to one.
	// Rect-area is blocked structurally: engine.RenderLight carries no width and
	// no height, so the rectangle the form factor integrates over cannot exist.
	if len(ignored) != 1 {
		t.Fatalf("expected only the rect-area light to be reported, got %v", ignored)
	}
	for _, id := range []string{"key", "fill", "amb", "lamp", "spot", "hemi"} {
		if message, ok := ignored[id]; ok {
			t.Fatalf("%q shades on the CPU path and must not be reported ignored: %q", id, message)
		}
	}
	for _, id := range []string{"panel"} {
		if _, ok := ignored[id]; !ok {
			t.Errorf("light %q changes no pixel and carries no diagnostic: %v", id, ignored)
		}
	}
	// The frame must still show the sphere. A light table that drops every light
	// would pass the checks above and render nothing.
	coverage, unique, variance := frameMetrics(result)
	if coverage < 0.01 || unique < 8 || variance < 0.0005 {
		t.Fatalf("the lit sphere vanished: coverage %.4f, unique colours %d, luminance variance %.6f",
			coverage, unique, variance)
	}
}

// frameMetrics returns coverage against the corner pixel, the number of unique
// colours, and the luminance variance. Report all three: a solid silhouette that
// lost its shading keeps its coverage and loses its variance.
func frameMetrics(result *preview.Result) (coverage float64, uniqueColors int, luminanceVariance float64) {
	img := result.Image
	bounds := img.Bounds()
	background := img.RGBAAt(bounds.Min.X, bounds.Min.Y)
	colors := map[uint32]struct{}{}
	changed := 0
	var sum, sumSquares float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			colors[uint32(c.R)<<24|uint32(c.G)<<16|uint32(c.B)<<8|uint32(c.A)] = struct{}{}
			if c != background {
				changed++
			}
			lum := 0.2126*float64(c.R)/255 + 0.7152*float64(c.G)/255 + 0.0722*float64(c.B)/255
			sum += lum
			sumSquares += lum * lum
		}
	}
	count := float64(bounds.Dx() * bounds.Dy())
	mean := sum / count
	variance := sumSquares/count - mean*mean
	if variance < 0 {
		variance = 0
	}
	return float64(changed) / count, len(colors), variance
}

func findDiagnostic(diagnostics []engine.RenderDiagnostic, code string) (engine.RenderDiagnostic, bool) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return diagnostic, true
		}
	}
	return engine.RenderDiagnostic{}, false
}
