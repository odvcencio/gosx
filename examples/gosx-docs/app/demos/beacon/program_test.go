package docs

import (
	"reflect"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/geom"
	"m31labs.dev/gosx/scene/preview"
)

func TestBlackglassCoastRuntimeContractMatchesStudioWorldSemantics(t *testing.T) {
	contract := BlackglassCoastRuntimeContract()
	if contract.Schema != "gosx.scene3d.world/v1" || contract.DocumentID != "blackglass-coast" || contract.Revision != 1 || contract.DocumentFingerprint != blackglassCoastSceneDocSHA256 {
		t.Fatalf("unexpected contract identity: %#v", contract)
	}
	if contract.ArtDirection != "sunlit volcanic naturalism" || contract.Water.RuntimeProfile != "blackglass-coast" {
		t.Fatalf("authored art/runtime contract was lost: %#v", contract)
	}
	if contract.Water.Size != scene.Vec3(34, 10, 24) || contract.Water.SurfaceY != 0 || contract.Water.BuoyancyScale != 1.15 || contract.Water.LinearDrag != 0.35 {
		t.Fatalf("water zone differs from Studio contract: %#v", contract.Water)
	}
	for _, id := range []string{"arrival", "opening-camera", "beacon-terrace", "beacon-lens", "cinematic-beacon"} {
		if _, ok := contract.Marker(id); !ok {
			t.Errorf("missing authored marker %q", id)
		}
	}
	if got := contract.Local(scene.Vec3(-12, 3, -6)); got != scene.Vec3(0, 3, 0) {
		t.Fatalf("water-centered transform = %#v, want local origin", got)
	}
}

func TestBlackglassCoastStaysWithinDeclaredBudget(t *testing.T) {
	props := BlackglassBeaconProgram()
	if len(props.Graph.Nodes) != blackglassCoastNodeBudget {
		t.Fatalf("node count = %d, want %d", len(props.Graph.Nodes), blackglassCoastNodeBudget)
	}
	if props.MaxFPS != 60 || props.MaxDevicePixelRatio != 1.5 || props.MaxPixels != blackglassCoastMaxPixels {
		t.Errorf("render budget = fps %.0f, dpr %.1f, pixels %d", props.MaxFPS, props.MaxDevicePixelRatio, props.MaxPixels)
	}
	if props.Stats == nil || !*props.Stats {
		t.Error("live renderer telemetry must be enabled")
	}
	if props.AdaptiveTargetFrameMS != 16.7 || props.AdaptiveQuality == nil || !*props.AdaptiveQuality {
		t.Error("adaptive 16.7ms quality governor must be enabled")
	}
	if props.PostFX.MaxPixels != scene.PostFXMaxPixels540p || props.Shadows.MaxPixels != scene.ShadowMaxPixels512 {
		t.Errorf("postfx/shadow caps = %d/%d", props.PostFX.MaxPixels, props.Shadows.MaxPixels)
	}
	water, ok := props.Graph.Nodes[3].(scene.WaterSystem)
	if !ok {
		t.Fatalf("node 3 = %T, want WaterSystem", props.Graph.Nodes[3])
	}
	contract := BlackglassCoastRuntimeContract()
	if water.ID != contract.Water.ID || water.PoolWidth != contract.Water.Size.X || water.PoolLength != contract.Water.Size.Z || water.InteractionProfile != contract.Water.RuntimeProfile {
		t.Fatalf("WaterSystem is not bound to Studio contract: %#v", water)
	}
	if water.Resolution != 128 || water.SurfaceResolution != 96 || water.ObjectTexturePixelBudget > 786432 {
		t.Fatalf("unexpected water budget: %#v", water)
	}
	ir := props.SceneIR()
	if len(ir.WaterSystems) != 1 || ir.WaterSystems[0].ID != contract.Water.ID || len(ir.InstancedMeshes) != 1 || ir.InstancedMeshes[0].Count != 12 {
		t.Fatalf("lowered world lost water or instancing: water=%#v instances=%#v", ir.WaterSystems, ir.InstancedMeshes)
	}
}

func TestBlackglassCoastExpandedGeometryStaysWithinDeclaredBudget(t *testing.T) {
	props := BlackglassBeaconProgram()
	result, err := preview.Render(props, preview.Options{
		Width: 320, Height: 180, Background: props.Background, DisableShadows: true, DisablePostFX: true,
	})
	if err != nil {
		t.Fatalf("lower renderer-facing coast geometry: %v", err)
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
		// Instancing uploads one expanded geometry and reuses it for every
		// transform, so the upload budget counts the mesh once.
		expandedVertices += vertices
	}
	if expandedVertices <= 0 || expandedVertices > blackglassCoastExpandedVertexBudget {
		t.Fatalf("expanded renderer-facing vertices = %d, budget = %d", expandedVertices, blackglassCoastExpandedVertexBudget)
	}
}

func TestBlackglassCoastHasAStableWorldAndNoAutonomousMotion(t *testing.T) {
	props := BlackglassBeaconProgram()
	if props.AutoRotate == nil || *props.AutoRotate || props.FillHeight == nil || !*props.FillHeight {
		t.Fatal("coast must be fill-height without autonomous camera motion")
	}
	want := []string{"coast-sun", "coast-sky", "beacon-fire", "blackglass-cove", "coast-sand", "coast-cliff-west", "coast-cliff-east", "coast-cliff-overlook", "coast-basalt-scatter", "ruin-arch-left", "ruin-arch-right", "ruin-arch-lintel", "beacon-plinth", "blackglass-beacon", "beacon-lens", "arrival-marker"}
	got := make([]string, 0, len(props.Graph.Nodes))
	for _, node := range props.Graph.Nodes {
		switch value := node.(type) {
		case scene.DirectionalLight:
			got = append(got, value.ID)
		case scene.HemisphereLight:
			got = append(got, value.ID)
		case scene.PointLight:
			got = append(got, value.ID)
		case scene.WaterSystem:
			got = append(got, value.ID)
		case scene.Mesh:
			got = append(got, value.ID)
			if value.Spin != (scene.Euler{}) || value.Drift != (scene.Vector3{}) {
				t.Errorf("mesh %q has autonomous motion", value.ID)
			}
		case scene.InstancedMesh:
			got = append(got, value.ID)
			if value.Count != 12 || len(value.Positions) != value.Count || len(value.Scales) != value.Count {
				t.Errorf("instanced basalt batch is incoherent: %#v", value)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stable world node IDs = %#v", got)
	}
}
