package scene

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The SceneIR marshal path feeds the wire format, the capability verdict,
// the headless oracle and the build-time poster frames. A change that
// makes it faster and moves one field is a wrong image everywhere at
// once. The golden file below pins the exact bytes the pre-optimization
// implementation produced for 400 generated scenes.
//
// Regenerate ONLY with a deliberate, reviewed wire-format change:
//
//	GOSX_UPDATE_SCENEIR_GOLDEN=1 go test ./scene/ -run TestSceneIRMarshalMatchesGoldenBytes
//
// The corpus and the golden both live under source control, so a
// regeneration shows up as a reviewable diff of every changed digest.
//
// REGENERATED on 2026-07-27 for the environment-map capability feature.
//
// WHAT MOVED: 178 of the 400 cases, and only those. Each writes a non-empty
// Environment.EnvMap, so collectFeatures now raises environment-map beside ibl,
// and the verdict grows one degraded name for WebGPU, one for Canvas2D, and one
// reason record for each. WebGL2 grows nothing, because it reads the image.
//
// WHAT DID NOT MOVE: the other 222 cases, byte for byte. No field was renamed,
// reordered or reshaped.
//
// WHY THE OLD VALUE WAS WRONG: it reported WebGPU and WebGL2 as equally capable
// of an authored environment map. WebGL2 samples the image; the WebGPU renderer
// carries the identifiers envMap, envIntensity and envRotation zero times. See
// the environment-map row in scene/capability/capability.go.
//
// EVIDENCE THE DELTA IS EXACTLY THAT: deleting every "environment-map" substring
// and every reason record naming it, from the NEW bytes of all 400 cases,
// reproduces the OLD digest in every case. Residual: zero.
//
// REGENERATED again for the Scene3D parity cluster A lighting-environment
// work. Matrix[ibl][webgpu] and Matrix[environment-map][webgpu] both flipped
// to true (the WebGPU renderer now consumes IBL products unconditionally and
// reads the equirect environment map). The same 178 cases moved again: each
// authors a non-empty Environment.EnvMap, which collectFeatures still pairs
// with FeatureIBL, and both features now report clean on WebGPU, so their
// degraded entries and reason records dropped out of the verdict. WebGL2
// still gates ibl on fragment texture units, so its degraded entry for ibl
// stays. See scene/capability/capability.go for both row rewrites.
//
// REGENERATED on 2026-09-02 for the StandardMaterial solid-default fix.
// StandardMaterial records whose Wireframe pointer is nil now emit the explicit
// `"wireframe":false` contract. That is the deliberate byte delta: typed PBR
// primitives become filled surfaces while raw SceneIR that omits the field
// keeps the browser runtime's historical compatibility default.
const sceneIRGoldenPath = "testdata/sceneir_marshal_golden.json"

// sceneIRGoldenCases is the number of generated scenes in the corpus.
const sceneIRGoldenCases = 400

// sceneIRGoldenEntry records one corpus case. The digests are SHA-256
// over the marshaled bytes. Lengths are stored beside them so a reader
// can see the corpus is not a set of empty scenes.
type sceneIRGoldenEntry struct {
	Case        int    `json:"case"`
	SceneDigest string `json:"sceneDigest"`
	SceneBytes  int    `json:"sceneBytes"`
	PropsDigest string `json:"propsDigest"`
	PropsBytes  int    `json:"propsBytes"`
}

// TestSceneIRMarshalMatchesGoldenBytes proves the marshal path still emits
// the bytes it emitted before the optimization, for every generated scene.
//
// A digest comparison is the honest form of "byte-identical": any single
// changed, reordered, added or dropped byte moves the digest.
func TestSceneIRMarshalMatchesGoldenBytes(t *testing.T) {
	got := buildSceneIRGolden(t)

	if os.Getenv("GOSX_UPDATE_SCENEIR_GOLDEN") == "1" {
		writeSceneIRGolden(t, got)
		t.Log("golden regenerated; re-run without GOSX_UPDATE_SCENEIR_GOLDEN")
		return
	}

	want := readSceneIRGolden(t)
	if len(want) != len(got) {
		t.Fatalf("golden has %d cases, corpus produced %d", len(want), len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("case %d no longer marshals to the golden bytes\n got %+v\nwant %+v",
				got[i].Case, got[i], want[i])
		}
	}
}

// TestSceneIRGoldenCorpusIsSubstantial guards the guard. A corpus of empty
// scenes would keep every digest stable through any change, so the golden
// test would pass while proving nothing.
func TestSceneIRGoldenCorpusIsSubstantial(t *testing.T) {
	entries := buildSceneIRGolden(t)

	var withLinePoints, withPostFX, withModels, withLights, total int
	rng := rand.New(rand.NewPCG(0x5ce9e, 0x17ea))
	for range sceneIRGoldenCases {
		props := randomMarshalScene(rng)
		ir := props.SceneIR()
		for i := range ir.Objects {
			if len(ir.Objects[i].Points) > 0 {
				withLinePoints++
				break
			}
		}
		if len(ir.PostEffects) > 0 {
			withPostFX++
		}
		if len(ir.Models) > 0 {
			withModels++
		}
		if len(ir.Lights) > 0 {
			withLights++
		}
		total++
	}

	if withLinePoints == 0 {
		t.Fatal("no generated scene carries line points; the golden cannot prove the points wire shape")
	}
	if withPostFX == 0 {
		t.Fatal("no generated scene carries a post effect")
	}
	if withModels == 0 {
		t.Fatal("no generated scene carries a model record")
	}
	if withLights == 0 {
		t.Fatal("no generated scene carries a light record")
	}
	var smallest = 1 << 30
	for _, e := range entries {
		if e.SceneBytes < smallest {
			smallest = e.SceneBytes
		}
	}
	if smallest < 200 {
		t.Fatalf("smallest generated scene marshals to %d bytes; the corpus is too thin", smallest)
	}
	t.Logf("corpus of %d scenes: %d with line points, %d with post effects, %d with models, %d with lights",
		total, withLinePoints, withPostFX, withModels, withLights)
}

// TestSceneIRLinePointsKeepEveryCoordinate pins the one wire shape the
// removed ObjectIR.MarshalJSON existed to protect: a polyline vertex must
// emit x, y and z even when a coordinate is zero, and "points" must stay
// the last key of the object record.
func TestSceneIRLinePointsKeepEveryCoordinate(t *testing.T) {
	// Live is declared near the end of ObjectIR, after the position the
	// polyline field used to hold. Setting it gives the "points is last"
	// assertion below something to be last of. Without a second late
	// field set, omitempty would drop every other key and "points" would
	// end up last no matter where the field is declared.
	obj := ObjectIR{
		ID:     "line-1",
		Kind:   "lines",
		Live:   []string{"color"},
		Points: []Vector3{{X: 0, Y: 2, Z: 0}, {X: 1, Y: 0, Z: 0}},
	}
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal object: %v", err)
	}
	if !containsSub(string(data), `"live":["color"]`) {
		t.Fatalf("the late field the ordering assertion depends on is missing: %s", data)
	}
	const wantPoints = `"points":[{"x":0,"y":2,"z":0},{"x":1,"y":0,"z":0}]`
	text := string(data)
	if !containsSub(text, wantPoints) {
		t.Fatalf("line points lost a zero coordinate\n got %s\nwant substring %s", text, wantPoints)
	}
	// "points" must be the final key, exactly where the old wrapper put it.
	if tail := text[len(text)-len(wantPoints)-1:]; tail != wantPoints+"}" {
		t.Fatalf("points is no longer the last key of the object record\n got tail %s", tail)
	}
}

// TestSceneIRMarshalRoundTripsThroughTheWire proves the wire bytes decode
// back into a SceneIR that re-marshals to the same bytes. The kind-tagged
// post-effect decoder makes this testable for scenes that carry a chain.
func TestSceneIRMarshalRoundTripsThroughTheWire(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x0d1e, 0x7a1b))
	var checked int
	for iteration := range 120 {
		props := randomMarshalScene(rng)
		ir := props.SceneIR()
		first, err := json.Marshal(ir)
		if err != nil {
			t.Fatalf("iteration %d: marshal: %v", iteration, err)
		}
		var decoded SceneIR
		if err := json.Unmarshal(first, &decoded); err != nil {
			t.Fatalf("iteration %d: unmarshal: %v", iteration, err)
		}
		second, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("iteration %d: re-marshal: %v", iteration, err)
		}
		if string(first) != string(second) {
			t.Fatalf("iteration %d: round trip changed the wire bytes\nfirst  %s\nsecond %s",
				iteration, first, second)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no scene was round-tripped; the test proves nothing")
	}
}

// FuzzSceneIRMarshalRoundTrip drives the same round-trip property from
// fuzzer-chosen seeds, so the corpus is not limited to the fixed stream
// the table tests use.
func FuzzSceneIRMarshalRoundTrip(f *testing.F) {
	f.Add(uint64(1), uint64(2))
	f.Add(uint64(0xfeed), uint64(0xbeef))
	f.Add(uint64(0), uint64(0))
	f.Fuzz(func(t *testing.T, a, b uint64) {
		props := randomMarshalScene(rand.New(rand.NewPCG(a, b)))
		ir := props.SceneIR()
		first, err := json.Marshal(ir)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded SceneIR
		if err := json.Unmarshal(first, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		second, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		if string(first) != string(second) {
			t.Fatalf("round trip changed the wire bytes\nfirst  %s\nsecond %s", first, second)
		}
	})
}

// shaderLibNeedsHoistReference is the predicate shaderLibNeedsHoist
// replaced. It builds the whole pair slice and counts every candidate, as
// the original did. It lives here, not beside the production code, so the
// dead version ships in no binary.
func shaderLibNeedsHoistReference(ir *SceneIR) bool {
	counts := make(map[string]int)
	for _, p := range collectShaderLibPairs(ir) {
		s := *p.inline
		if len(s) < shaderLibThreshold {
			continue
		}
		counts[s]++
		if counts[s] >= 2 {
			return true
		}
	}
	return false
}

// TestShaderLibNeedsHoistMatchesTheReferencePredicate runs the fast
// predicate beside the one it replaced, over the same generated corpus.
// The predicate decides whether MarshalJSON clones and rewrites the scene
// collections, so a wrong answer either drops a hoist or mutates a scene
// that asked for none.
func TestShaderLibNeedsHoistMatchesTheReferencePredicate(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5ce9e, 0x17ea))
	var trueAnswers int
	for iteration := range sceneIRGoldenCases {
		ir := randomMarshalScene(rng).SceneIR()
		// Duplicate one long shader source often enough that the true
		// branch is exercised, not only the false one.
		// hoistShaderLib ignores any source below shaderLibThreshold bytes,
		// so the padding here is what makes the true branch reachable.
		if iteration%3 == 0 && len(ir.Objects) >= 2 {
			src := "void main(){gl_FragColor=vec4(" + fmt.Sprint(iteration) + ");}" +
				strings.Repeat("/", shaderLibThreshold)
			ir.Objects[0].CustomFragment = src
			ir.Objects[1].CustomFragment = src
		}
		want := shaderLibNeedsHoistReference(&ir)
		got := shaderLibNeedsHoist(&ir)
		if got != want {
			t.Fatalf("iteration %d: predicate answered %v, reference answered %v", iteration, got, want)
		}
		if want {
			trueAnswers++
		}
	}
	if trueAnswers == 0 {
		t.Fatal("no generated scene needed a hoist; the test only proves the false branch")
	}
	if trueAnswers == sceneIRGoldenCases {
		t.Fatal("every generated scene needed a hoist; the test only proves the true branch")
	}
	t.Logf("%d of %d generated scenes needed a hoist", trueAnswers, sceneIRGoldenCases)
}

// buildSceneIRGolden marshals the whole generated corpus and returns one
// entry per case. The random stream is fixed, so every run sees the same
// 400 scenes.
func buildSceneIRGolden(t *testing.T) []sceneIRGoldenEntry {
	t.Helper()
	rng := rand.New(rand.NewPCG(0x5ce9e, 0x17ea))
	out := make([]sceneIRGoldenEntry, 0, sceneIRGoldenCases)
	for i := range sceneIRGoldenCases {
		props := randomMarshalScene(rng)
		sceneBytes, err := json.Marshal(props.SceneIR())
		if err != nil {
			t.Fatalf("case %d: marshal scene: %v", i, err)
		}
		propsBytes, err := json.Marshal(props)
		if err != nil {
			t.Fatalf("case %d: marshal props: %v", i, err)
		}
		out = append(out, sceneIRGoldenEntry{
			Case:        i,
			SceneDigest: digestOf(sceneBytes),
			SceneBytes:  len(sceneBytes),
			PropsDigest: digestOf(propsBytes),
			PropsBytes:  len(propsBytes),
		})
	}
	return out
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func readSceneIRGolden(t *testing.T) []sceneIRGoldenEntry {
	t.Helper()
	raw, err := os.ReadFile(sceneIRGoldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var out []sceneIRGoldenEntry
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	return out
}

func writeSceneIRGolden(t *testing.T, entries []sceneIRGoldenEntry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(sceneIRGoldenPath), 0o755); err != nil {
		t.Fatalf("create testdata: %v", err)
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("encode golden: %v", err)
	}
	if err := os.WriteFile(sceneIRGoldenPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}

func containsSub(haystack, needle string) bool {
	return len(needle) <= len(haystack) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// randomMarshalScene builds one pseudo-random Props. The generator reaches
// every record kind the marshal path can emit, so the golden covers the
// whole wire surface rather than the mesh happy path.
//
// Zero coordinates appear in the generated polylines on purpose. They are
// the exact case the old ObjectIR.MarshalJSON wrapper protected.
func randomMarshalScene(rng *rand.Rand) Props {
	nodeCount := 1 + rng.IntN(8)
	nodes := make([]Node, 0, nodeCount)
	for range nodeCount {
		nodes = append(nodes, randomMarshalNode(rng, 2))
	}
	props := Props{
		Width:      640 + rng.IntN(800),
		Height:     360 + rng.IntN(600),
		Background: randomPick(rng, "", "#05080f", "#ffffff"),
		Controls:   randomPick(rng, "", "orbit", "fly"),
		Camera: PerspectiveCamera{
			Position: randomMarshalVector(rng),
			FOV:      35 + rng.Float64()*40,
		},
		Environment: Environment{
			AmbientColor:     randomPick(rng, "", "#ffffff", "#101325"),
			AmbientIntensity: rng.Float64(),
			EnvironmentMap:   randomPick(rng, "", "/assets/studio.hdr"),
			ToneMapping:      randomPick(rng, "", "aces", "reinhard"),
			FogColor:         randomPick(rng, "", "#223344"),
			FogDensity:       rng.Float64() * 0.1,
		},
		Shadows: Shadows{MaxPixels: randomPick(rng, 0, ShadowMaxPixels1024, ShadowMaxPixels2048)},
		Graph:   NewGraph(nodes...),
	}
	if rng.IntN(2) == 0 {
		props.PostFX = PostFX{
			MaxPixels: randomPick(rng, 0, PostFXMaxPixels1080p, PostFXMaxPixels720p),
			Effects:   randomMarshalEffects(rng),
		}
	}
	if rng.IntN(4) == 0 {
		props.Compression = &Compression{
			BitWidth:        randomPick(rng, 8, 10, 12),
			Progressive:     rng.IntN(2) == 0,
			PreviewBitWidth: randomPick(rng, 0, 2, 4),
			LOD:             rng.IntN(2) == 0,
		}
	}
	if rng.IntN(6) == 0 {
		props.RequireWebGL = Bool(true)
	}
	return props
}

func randomMarshalEffects(rng *rand.Rand) []PostEffect {
	out := make([]PostEffect, 0, 4)
	for range 1 + rng.IntN(4) {
		switch rng.IntN(7) {
		case 0:
			out = append(out, Bloom{Threshold: rng.Float32(), Strength: rng.Float32(), Radius: rng.Float32() * 10, Scale: rng.Float32()})
		case 1:
			out = append(out, Tonemap{Mode: randomPick(rng, TonemapACES, TonemapReinhard, TonemapFilmic), Exposure: rng.Float32() * 2})
		case 2:
			out = append(out, Vignette{Intensity: rng.Float32()})
		case 3:
			out = append(out, ColorGrade{Exposure: rng.Float32(), Contrast: rng.Float32() * 2, Saturation: rng.Float32() * 2})
		case 4:
			out = append(out, SSAO{Radius: rng.Float32() * 8, Intensity: rng.Float32(), Bias: rng.Float32()})
		case 5:
			out = append(out, DOF{FocusDistance: rng.Float32() * 20, Aperture: rng.Float32(), MaxBlur: rng.Float32() * 8})
		default:
			out = append(out, FXAA{})
		}
	}
	return out
}

func randomMarshalNode(rng *rand.Rand, depth int) Node {
	kind := rng.IntN(10)
	if depth <= 0 && kind == 0 {
		kind = 1
	}
	switch kind {
	case 0:
		group := Group{
			ID:       fmt.Sprintf("group-%d", rng.IntN(100)),
			Position: randomMarshalVector(rng),
		}
		for range 1 + rng.IntN(3) {
			group.Children = append(group.Children, randomMarshalNode(rng, depth-1))
		}
		return group
	case 1, 2, 3:
		return randomMarshalMesh(rng)
	case 4:
		// A polyline. Zero coordinates are deliberate.
		return Mesh{
			Geometry: LinesGeometry{
				Points:   []Vector3{{X: 0, Y: 2, Z: 0}, {X: rng.Float64(), Y: 0, Z: 0}, {}},
				Segments: [][2]int{{0, 1}, {1, 2}},
				Width:    rng.Float64() * 5,
			},
			Material: FlatMaterial{Color: "#8ecfff", BlendMode: BlendAdditive},
			Position: randomMarshalVector(rng),
		}
	case 5:
		return Model{
			ID:         fmt.Sprintf("model-%d", rng.IntN(100)),
			Src:        randomPick(rng, "/assets/robot.glb", "  /assets/ship.glb  "),
			Position:   randomMarshalVector(rng),
			Bounds:     rng.Float64() * 4,
			Fit:        randomPick(rng, "", " contain ", "cover"),
			FitAlign:   randomPick(rng, "", " center "),
			Animation:  randomPick(rng, "", "idle"),
			CastShadow: rng.IntN(2) == 0,
		}
	case 6:
		return randomMarshalLight(rng)
	case 7:
		return Label{
			ID:       fmt.Sprintf("label-%d", rng.IntN(100)),
			Text:     randomPick(rng, "origin", "a <b> & c", "line\nbreak"),
			Position: randomMarshalVector(rng),
		}
	case 8:
		return Points{
			ID:       fmt.Sprintf("points-%d", rng.IntN(100)),
			Count:    10 + rng.IntN(50),
			Size:     rng.Float64(),
			Color:    "#ffffff",
			Position: randomMarshalVector(rng),
		}
	default:
		return InstancedMesh{
			ID:        fmt.Sprintf("inst-%d", rng.IntN(100)),
			Geometry:  SphereGeometry{Radius: rng.Float64(), Segments: 8},
			Material:  StandardMaterial{Color: "#c8a8ff", Roughness: rng.Float64()},
			Count:     4 + rng.IntN(20),
			Positions: []Vector3{randomMarshalVector(rng), randomMarshalVector(rng)},
		}
	}
}

func randomMarshalMesh(rng *rand.Rand) Mesh {
	mesh := Mesh{
		ID:            randomPick(rng, "", fmt.Sprintf("mesh-%d", rng.IntN(100))),
		Position:      randomMarshalVector(rng),
		Rotation:      Rotate(rng.Float64(), rng.Float64(), rng.Float64()),
		CastShadow:    rng.IntN(2) == 0,
		ReceiveShadow: rng.IntN(2) == 0,
	}
	switch rng.IntN(4) {
	case 0:
		mesh.Geometry = SphereGeometry{Radius: rng.Float64() * 2, Segments: 8 + rng.IntN(24)}
	case 1:
		mesh.Geometry = BoxGeometry{Width: rng.Float64(), Height: rng.Float64(), Depth: rng.Float64()}
	case 2:
		mesh.Geometry = PlaneGeometry{Width: rng.Float64() * 10, Height: rng.Float64() * 10}
	default:
		mesh.Geometry = CylinderGeometry{RadiusTop: rng.Float64(), RadiusBottom: rng.Float64(), Height: rng.Float64(), Segments: 12}
	}
	switch rng.IntN(4) {
	case 0:
		mesh.Material = StandardMaterial{Color: "#d4af37", Roughness: rng.Float64(), Metalness: rng.Float64(), Emissive: rng.Float64()}
	case 1:
		mesh.Material = FlatMaterial{Color: "#8ecfff", BlendMode: BlendAdditive}
	case 2:
		mesh.Material = LineDashedMaterial{Width: rng.Float64() * 3, DashSize: rng.Float64(), GapSize: rng.Float64()}
	default:
		mesh.Material = CustomMaterial{
			StandardMaterial: StandardMaterial{Color: "#ff00ff"},
			VertexGLSL:       "void main(){gl_Position=vec4(0.0);}",
			FragmentGLSL:     "void main(){gl_FragColor=vec4(1.0);}",
		}
	}
	if rng.IntN(3) == 0 {
		mesh.Spin = Rotate(0, rng.Float64(), 0)
	}
	if rng.IntN(4) == 0 {
		mesh.Pickable = Bool(true)
	}
	if rng.IntN(5) == 0 {
		mesh.Scale = Vec3(rng.Float64()+0.5, rng.Float64()+0.5, rng.Float64()+0.5)
	}
	if rng.IntN(6) == 0 {
		mesh.Children = append(mesh.Children, randomMarshalLight(rng))
	}
	return mesh
}

func randomMarshalLight(rng *rand.Rand) Node {
	switch rng.IntN(7) {
	case 0:
		return AmbientLight{Color: "#ffffff", Intensity: rng.Float64()}
	case 1:
		return DirectionalLight{Color: "#fff1d6", Intensity: rng.Float64(), Direction: randomMarshalVector(rng), CastShadow: true, ShadowSize: 1024}
	case 2:
		return PointLight{Color: "#5fa3ff", Intensity: rng.Float64(), Position: randomMarshalVector(rng), Range: rng.Float64() * 20}
	case 3:
		return SpotLight{Color: "#ffd9a0", Intensity: rng.Float64(), Position: randomMarshalVector(rng), Angle: rng.Float64()}
	case 4:
		return HemisphereLight{SkyColor: "#88aaff", GroundColor: "#221100", Intensity: rng.Float64()}
	case 5:
		return RectAreaLight{Color: "#ffffff", Intensity: rng.Float64(), Position: randomMarshalVector(rng), Width: rng.Float64(), Height: rng.Float64()}
	default:
		return LightProbe{Intensity: rng.Float64()}
	}
}

func randomMarshalVector(rng *rand.Rand) Vector3 {
	// Zero components appear often on purpose: omitempty behaviour on
	// coordinates is exactly what the wire shape has to pin.
	pick := func() float64 {
		if rng.IntN(3) == 0 {
			return 0
		}
		return rng.Float64()*20 - 10
	}
	return Vector3{X: pick(), Y: pick(), Z: pick()}
}

func randomPick[T any](rng *rand.Rand, options ...T) T {
	return options[rng.IntN(len(options))]
}
