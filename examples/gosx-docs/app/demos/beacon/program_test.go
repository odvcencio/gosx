package docs

import (
	"reflect"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestBlackglassBeaconStaysWithinDeclaredBudget(t *testing.T) {
	props := BlackglassBeaconProgram()
	if len(props.Graph.Nodes) != beaconNodeBudget {
		t.Fatalf("node count = %d, want %d", len(props.Graph.Nodes), beaconNodeBudget)
	}
	if props.MaxFPS != 30 || props.MaxDevicePixelRatio != 1.5 || props.MaxPixels != beaconMaxPixels {
		t.Errorf("render budget = fps %.0f, dpr %.1f, pixels %d", props.MaxFPS, props.MaxDevicePixelRatio, props.MaxPixels)
	}
	if props.PostFX.MaxPixels != scene.PostFXMaxPixels540p || props.Shadows.MaxPixels != scene.ShadowMaxPixels512 {
		t.Errorf("postfx/shadow caps = %d/%d", props.PostFX.MaxPixels, props.Shadows.MaxPixels)
	}
	key, ok := props.Graph.Nodes[0].(scene.DirectionalLight)
	if !ok || key.ShadowSize != 512 {
		t.Errorf("key shadow size = %d, want 512", key.ShadowSize)
	}
	if props.AdaptiveTargetFrameMS != 24 || props.AdaptiveQuality == nil || !*props.AdaptiveQuality {
		t.Error("adaptive 24ms quality governor must be enabled")
	}
	expandedVertices := 0
	for _, node := range props.Graph.Nodes {
		mesh, ok := node.(scene.Mesh)
		if !ok {
			continue
		}
		switch geometry := mesh.Geometry.(type) {
		case scene.PlaneGeometry:
			expandedVertices += 6
		case scene.BoxGeometry:
			expandedVertices += 36
		case scene.PyramidGeometry:
			expandedVertices += 24
		case scene.SphereGeometry:
			segments := geometry.Segments
			if segments < 3 {
				segments = 32
			}
			expandedVertices += 6 * segments * segments
		case scene.TorusGeometry:
			radial := geometry.RadialSegments
			if radial < 3 {
				radial = 32
			}
			tubular := geometry.TubularSegments
			if tubular < 3 {
				tubular = 16
			}
			expandedVertices += 6 * radial * tubular
		case scene.LinesGeometry:
			expandedVertices += 2 * len(geometry.Segments)
		default:
			t.Fatalf("mesh %q uses unbudgeted geometry %T", mesh.ID, mesh.Geometry)
		}
	}
	if expandedVertices > beaconExpandedVertexBudget {
		t.Errorf("expanded vertex estimate = %d, budget %d", expandedVertices, beaconExpandedVertexBudget)
	}
	want := []reflect.Type{reflect.TypeOf(scene.Bloom{}), reflect.TypeOf(scene.Tonemap{}), reflect.TypeOf(scene.Vignette{})}
	got := make([]reflect.Type, 0, len(props.PostFX.Effects))
	for _, effect := range props.PostFX.Effects {
		got = append(got, reflect.TypeOf(effect))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("postfx order = %v, want Bloom → Tonemap → Vignette", got)
	}
}

func TestBlackglassBeaconIsAssetFreeMotionSafeAndStable(t *testing.T) {
	props := BlackglassBeaconProgram()
	if props.AutoRotate == nil || *props.AutoRotate || props.FillHeight == nil || !*props.FillHeight {
		t.Error("beacon must be fill-height without autonomous motion")
	}
	want := []string{"beacon-key", "beacon-cyan-fill", "beacon-warm-core-light", "beacon-hemi", "beacon-ground", "beacon-plinth-base", "beacon-plinth-step", "beacon-plinth-slab", "beacon-tower-foot", "beacon-tower-shaft", "beacon-tower-spine", "beacon-tower-fin-left", "beacon-tower-fin-right", "beacon-crown-deck", "beacon-cyan-crown", "beacon-lantern-cyan", "beacon-eclipse-disc", "beacon-warm-core", "beacon-eclipse-beam"}
	got := make([]string, 0, len(props.Graph.Nodes))
	for _, node := range props.Graph.Nodes {
		switch value := node.(type) {
		case scene.DirectionalLight:
			got = append(got, value.ID)
		case scene.PointLight:
			got = append(got, value.ID)
		case scene.HemisphereLight:
			got = append(got, value.ID)
		case scene.Mesh:
			got = append(got, value.ID)
			if value.Spin != (scene.Euler{}) || value.Drift != (scene.Vector3{}) {
				t.Errorf("mesh %q has autonomous motion", value.ID)
			}
			if material, ok := value.Material.(scene.StandardMaterial); ok && (material.Texture != "" || material.NormalMap != "" || material.EmissiveMap != "") {
				t.Errorf("mesh %q uses an asset", value.ID)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stable node IDs = %#v", got)
	}
}
