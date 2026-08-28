package docs

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/geom"
	"m31labs.dev/gosx/scene/preview"
)

// TestLodestarMeridianStaysWithinDeclaredBudgets pins the render and animation
// budgets the page advertises. The overlay copy must never promise more than
// this program declares.
func TestLodestarMeridianStaysWithinDeclaredBudgets(t *testing.T) {
	props := LodestarMeridianProgram()
	if len(props.Graph.Nodes) != orreryNodeBudget {
		t.Fatalf("node count = %d, want %d", len(props.Graph.Nodes), orreryNodeBudget)
	}
	if props.MaxFPS != 60 || props.MaxDevicePixelRatio != 1.5 || props.MaxPixels != orreryMaxPixels {
		t.Errorf("render budget = fps %.0f, dpr %.1f, pixels %d", props.MaxFPS, props.MaxDevicePixelRatio, props.MaxPixels)
	}
	if props.PostFX.MaxPixels != scene.PostFXMaxPixels540p || props.Shadows.MaxPixels != scene.ShadowMaxPixels512 {
		t.Errorf("postfx/shadow caps = %d/%d", props.PostFX.MaxPixels, props.Shadows.MaxPixels)
	}
	if props.Stats == nil || !*props.Stats {
		t.Error("live renderer telemetry must be enabled")
	}
	if props.AdaptiveTargetFrameMS != 16.7 || props.AdaptiveQuality == nil || !*props.AdaptiveQuality {
		t.Error("adaptive 16.7ms quality governor must be enabled")
	}
	for _, node := range props.Graph.Nodes {
		points, ok := node.(scene.Points)
		if !ok {
			continue
		}
		if points.Count > orreryStarCount || len(points.Positions) != points.Count {
			t.Errorf("starfield count = %d with %d positions, budget %d", points.Count, len(points.Positions), orreryStarCount)
		}
	}
}

// TestLodestarMeridianHasAStableWorldInventory pins the full node inventory in
// declaration order. Animation channels address nodes by index, so this order
// is load-bearing choreography state, not presentation detail.
func TestLodestarMeridianHasAStableWorldInventory(t *testing.T) {
	props := LodestarMeridianProgram()
	want := []string{
		"orrery-key",
		"orrery-heart-light",
		"orrery-horizon",
		"orrery-dais-base",
		"orrery-dais-step",
		"orrery-ecliptic-ring",
		"orrery-heart",
		"orrery-heart-halo",
		"orrery-armillary-alpha",
		"orrery-armillary-beta",
		"orrery-armillary-gamma",
		"orrery-planet-cinder",
		"orrery-planet-porcelain",
		"orrery-planet-verdigris",
		"orrery-transit-moon",
		"orrery-starfield",
		"clip:meridian-procession",
	}
	got := make([]string, 0, len(props.Graph.Nodes))
	for _, node := range props.Graph.Nodes {
		got = append(got, orreryNodeID(node))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stable world node IDs = %#v", got)
	}
	if props.Controls != scene.ControlOrbit || props.AutoRotate == nil || *props.AutoRotate || props.FillHeight == nil || !*props.FillHeight {
		t.Fatal("the meridian must stay user-directed orbit inside a bounded fill-height stage")
	}
	if props.ControlMinDistance != 6.5 || props.ControlMaxDistance != 20 {
		t.Fatalf("orbit bounds = %.1f..%.1f, want 6.5..20", props.ControlMinDistance, props.ControlMaxDistance)
	}
}

func orreryNodeID(node scene.Node) string {
	switch node := node.(type) {
	case scene.DirectionalLight:
		return node.ID
	case scene.PointLight:
		return node.ID
	case scene.HemisphereLight:
		return node.ID
	case scene.Mesh:
		return node.ID
	case scene.Points:
		return node.ID
	case scene.AnimationClip:
		return "clip:" + node.Name
	default:
		return ""
	}
}

func lodestarMeridianMeshIndex(t *testing.T, props scene.Props) map[string]int {
	t.Helper()
	index := make(map[string]int, len(props.Graph.Nodes))
	for i, node := range props.Graph.Nodes {
		if mesh, ok := node.(scene.Mesh); ok && mesh.ID != "" {
			index[mesh.ID] = i
		}
	}
	return index
}

// TestLodestarMeridianChoreographyIsStableAndDeterministic proves the demo's
// core claim: one named clip with stable channel targets, closed planetary
// orbits, a transit whose light cues land on the same key instant, and a
// byte-stable wire payload across builds.
func TestLodestarMeridianChoreographyIsStableAndDeterministic(t *testing.T) {
	props := LodestarMeridianProgram()
	ir := props.SceneIR()
	if len(ir.Animations) != 1 {
		t.Fatalf("animations = %d clips, want exactly the demonstration cycle", len(ir.Animations))
	}
	clip := ir.Animations[0]
	if clip.Name != "meridian-procession" || clip.Duration != orreryCycleSeconds {
		t.Fatalf("clip identity = %q@%.1fs", clip.Name, clip.Duration)
	}
	if len(clip.Channels) != 4 {
		t.Fatalf("channels = %d, want 4 (three planets + transit moon)", len(clip.Channels))
	}

	meshIndex := lodestarMeridianMeshIndex(t, props)
	type orbitExpectation struct {
		nodeID               string
		radius               float64
		keys                 int
		samplesPerRevolution int
		revolutions          int
	}
	expectations := []orbitExpectation{
		{nodeID: "orrery-planet-cinder", radius: orreryCinderRadius, keys: 65, samplesPerRevolution: 16, revolutions: 4},
		{nodeID: "orrery-planet-porcelain", radius: orreryPorcelainRadius, keys: 49, samplesPerRevolution: 16, revolutions: 3},
		{nodeID: "orrery-planet-verdigris", radius: orreryVerdigrisRadius, keys: 41, samplesPerRevolution: 20, revolutions: 2},
	}
	for i, want := range expectations {
		channel := clip.Channels[i]
		if got := channel.TargetNode; got != meshIndex[want.nodeID] {
			t.Errorf("%s targets node %d, want stable index %d", want.nodeID, got, meshIndex[want.nodeID])
		}
		if got := channel.TargetID; got != want.nodeID {
			t.Errorf("%s TargetID = %q, want the stable lowering-resolved ID", want.nodeID, got)
		}
		if channel.Property != "translation" || channel.Interpolation != "LINEAR" {
			t.Errorf("%s channel = %q/%q, want LINEAR translation", want.nodeID, channel.Property, channel.Interpolation)
		}
		if len(channel.Times) != want.keys || len(channel.Values) != 3*want.keys {
			t.Fatalf("%s keys = %d/%d floats, want %d keys", want.nodeID, len(channel.Times), len(channel.Values), want.keys)
		}
		for k := 1; k < len(channel.Times); k++ {
			if channel.Times[k] <= channel.Times[k-1] {
				t.Fatalf("%s key times not strictly increasing at %d", want.nodeID, k)
			}
		}
		if channel.Times[0] != 0 || channel.Times[len(channel.Times)-1] != orreryCycleSeconds {
			t.Fatalf("%s cycle bounds = %.2f..%.2f", want.nodeID, channel.Times[0], channel.Times[len(channel.Times)-1])
		}
		first := channel.Values[0:3]
		last := channel.Values[len(channel.Values)-3:]
		if !reflect.DeepEqual(first, last) {
			t.Errorf("%s loop does not close: first %v last %v", want.nodeID, first, last)
		}
		// Channel values are composed by the shared runtime as offsets from
		// the planet's authored pose, so compose them back before checking
		// the rendered orbit.
		parkX, parkY, parkZ := lodestarMeridianPlanetPark(want.nodeID)
		for k := 0; k < len(channel.Times); k++ {
			x := parkX + channel.Values[3*k]
			y := parkY + channel.Values[3*k+1]
			z := parkZ + channel.Values[3*k+2]
			if math.Abs(y-orreryHeartY) > 1e-9 {
				t.Errorf("%s key %d left the ecliptic plane (y=%.6f)", want.nodeID, k, y)
			}
			if got := math.Sqrt(x*x + z*z); math.Abs(got-want.radius) > 1e-9 {
				t.Errorf("%s key %d drifted off its ring radius (%.6f, want %.6f)", want.nodeID, k, got, want.radius)
			}
		}
		_ = want.samplesPerRevolution
	}

	// The transit moon parks below the dais, rises at 12 s, aligns at 13.2 s,
	// exits behind the armature at 14.4 s, and parks again by 14.7 s.
	moon := clip.Channels[3]
	if got := moon.TargetNode; got != meshIndex["orrery-transit-moon"] {
		t.Errorf("transit moon targets node %d, want stable index %d", got, meshIndex["orrery-transit-moon"])
	}
	wantTimes := []float64{0, orreryTransitRise - 0.3, orreryTransitRise, orreryTransitMid, orreryTransitExit, orreryTransitExit + 0.3, orreryCycleSeconds}
	if !reflect.DeepEqual(moon.Times, wantTimes) {
		t.Fatalf("transit key times = %#v, want %#v", moon.Times, wantTimes)
	}
	// Compose the moon's mid-transit key with its authored parking pose: the
	// rendered pose must align with the heart at 13.2 s.
	parkX, parkY, parkZ := orreryMoonPark().X, orreryMoonPark().Y, orreryMoonPark().Z
	midX, midY, midZ := parkX+moon.Values[3*3], parkY+moon.Values[3*3+1], parkZ+moon.Values[3*3+2]
	if midX != -0.18 || midY != orreryHeartY+0.08 || midZ != 0.52 {
		t.Errorf("mid-transit pose = (%.3f, %.3f, %.3f)", midX, midY, midZ)
	}
	parkFirst := moon.Values[0:3]
	parkLast := moon.Values[len(moon.Values)-3:]
	if !reflect.DeepEqual(parkFirst, parkLast) {
		t.Errorf("transit parking poses differ: %v vs %v", parkFirst, parkLast)
	}

	// Light answers alignment on the same beat: the heart flare and halo
	// pulse peak exactly at the moon's mid-transit key.
	nodes := props.Graph.Nodes
	heart, ok := nodes[meshIndex["orrery-heart"]].(scene.Mesh)
	if !ok || len(heart.MaterialAnims) != 1 {
		t.Fatalf("heart material anims missing: %#v", heart)
	}
	flare := heart.MaterialAnims[0]
	halo, ok := nodes[meshIndex["orrery-heart-halo"]].(scene.Mesh)
	if !ok || len(halo.MaterialAnims) != 1 {
		t.Fatalf("halo material anims missing: %#v", halo)
	}
	pulse := halo.MaterialAnims[0]
	for _, track := range []struct {
		name string
		anim scene.MaterialUniformAnim
		peak float64
	}{
		{"heart flare", flare, 3.4},
		{"halo pulse", pulse, 1.7},
	} {
		if track.anim.Uniform != "emissive" || track.anim.Arity != 1 {
			t.Errorf("%s drives wrong uniform %q arity %d", track.name, track.anim.Uniform, track.anim.Arity)
		}
		if !reflect.DeepEqual(track.anim.Times[len(track.anim.Times)-1], orreryCycleSeconds) || track.anim.Values[0] != track.anim.Values[len(track.anim.Values)-1] {
			t.Errorf("%s track does not close its cycle", track.name)
		}
		found := false
		for k, ts := range track.anim.Times {
			if ts == orreryTransitMid {
				found = true
				if track.anim.Values[k] != track.peak {
					t.Errorf("%s peaks at %.2f on the transit beat, want %.2f", track.name, track.anim.Values[k], track.peak)
				}
			}
		}
		if !found {
			t.Errorf("%s has no key pinned to the %.1fs transit beat", track.name, orreryTransitMid)
		}
	}
	if flare.Times[3] != orreryTransitMid || pulse.Times[2] != orreryTransitMid || moon.Times[3] != orreryTransitMid {
		t.Error("flare, halo pulse, and mid-transit keys must share the same instant")
	}

	// Determinism: two independently built programs serialize identically.
	again := LodestarMeridianProgram()
	first, err := json.Marshal(again.SceneIR())
	if err != nil {
		t.Fatalf("marshal IR: %v", err)
	}
	second, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("marshal IR: %v", err)
	}
	if string(first) != string(second) {
		t.Error("LodestarMeridianProgram is not byte-for-byte deterministic")
	}
}

// TestLodestarMeridianExpandedGeometryStaysWithinDeclaredBudget lowers the
// scene through the CPU preview path and measures the renderer-facing vertex
// payload, mirroring the Blackglass Coast budget proof.
func TestLodestarMeridianExpandedGeometryStaysWithinDeclaredBudget(t *testing.T) {
	props := LodestarMeridianProgram()
	result, err := preview.Render(props, preview.Options{
		Width: 320, Height: 180, Background: props.Background, DisableShadows: true, DisablePostFX: true,
	})
	if err != nil {
		t.Fatalf("lower renderer-facing meridian geometry: %v", err)
	}
	if len(result.Bundle.Points) != 1 {
		t.Fatalf("bundle points layers = %d, want the seeded starfield", len(result.Bundle.Points))
	}
	if len(result.Bundle.Animations) != 1 || len(result.Bundle.Animations[0].Channels) != 4 {
		t.Fatalf("renderer-facing payload lost the procession clip: %#v", result.Bundle.Animations)
	}
	expandedVertices := 0
	for _, mesh := range result.Bundle.InstancedMeshes {
		vertices := geom.DrawVertexCount(geom.Params{
			Kind: mesh.Kind, Size: mesh.Size, Width: mesh.Width, Height: mesh.Height, Depth: mesh.Depth,
			Radius: mesh.Radius, RadiusTop: mesh.RadiusTop, RadiusBottom: mesh.RadiusBottom, Tube: mesh.Tube,
			Segments: mesh.Segments, RadialSegments: mesh.RadialSegments, TubularSegments: mesh.TubularSegments,
		})
		if vertices <= 0 || mesh.InstanceCount <= 0 {
			t.Fatalf("mesh %q has an invalid renderer-facing count: vertices=%d instances=%d", mesh.ID, vertices, mesh.InstanceCount)
		}
		// Single-instance built-ins upload their expanded geometry once.
		expandedVertices += vertices
	}
	if expandedVertices <= 0 || expandedVertices > orreryExpandedVertexBudget {
		t.Fatalf("expanded renderer-facing vertices = %d, budget = %d", expandedVertices, orreryExpandedVertexBudget)
	}
}

// lodestarMeridianPlanetPark returns a planet's authored parking pose so tests
// can compose clip offsets back into world space exactly as the shared runtime
// bundle path does.
func lodestarMeridianPlanetPark(nodeID string) (float64, float64, float64) {
	switch nodeID {
	case "orrery-planet-cinder":
		return orreryCinderRadius, orreryHeartY, 0
	case "orrery-planet-porcelain":
		return -orreryPorcelainRadius, orreryHeartY, 0
	case "orrery-planet-verdigris":
		return 0, orreryHeartY, orreryVerdigrisRadius
	default:
		panic("lodestar meridian: unknown planet park " + nodeID)
	}
}
