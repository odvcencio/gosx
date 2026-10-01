package docs

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/capability"
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
	payload, err := json.Marshal(props)
	if err != nil || props.Camera.FOV != 50 || props.Camera.PortraitFOV != 80 || !strings.Contains(string(payload), `"portraitFOV":80`) {
		t.Fatalf("coast must preserve its portrait framing in the browser contract: %s, %v", payload, err)
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
	water, ok := props.Graph.Nodes[4].(scene.WaterSystem)
	if !ok {
		t.Fatalf("node 4 = %T, want WaterSystem", props.Graph.Nodes[4])
	}
	contract := BlackglassCoastRuntimeContract()
	if water.ID != contract.Water.ID || water.PoolWidth != contract.Water.Size.X/2 || water.PoolLength != contract.Water.Size.Z/2 || water.InteractionProfile != contract.Water.RuntimeProfile {
		t.Fatalf("WaterSystem zone mismatch: id=%q width=%.1f length=%.1f profile=%q", water.ID, water.PoolWidth, water.PoolLength, water.InteractionProfile)
	}
	if water.Resolution != 128 || water.SurfaceResolution != 96 || water.ObjectTexturePixelBudget > 786432 {
		t.Fatalf("unexpected water budget: %#v", water)
	}
	ir := props.SceneIR()
	if len(ir.WaterSystems) != 1 || ir.WaterSystems[0].ID != contract.Water.ID || len(ir.Models) != 4 || len(ir.HTML) != 3 || len(ir.ComputeParticles) != 1 {
		t.Fatalf("lowered world lost authored layers: water=%d models=%d surfaces=%d particles=%d", len(ir.WaterSystems), len(ir.Models), len(ir.HTML), len(ir.ComputeParticles))
	}
	if ir.BackendCaps == nil || !reflect.DeepEqual(ir.BackendCaps.Capable, []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL}) {
		t.Fatalf("renderer honesty verdict = %#v, want WebGPU and WebGL2", ir.BackendCaps)
	}
	if !reflect.DeepEqual(ir.BackendCaps.Degraded[capability.BackendWebGL], []capability.Feature{capability.FeatureComputeParts}) {
		t.Fatalf("WebGL2 must support IBL and report its CPU particle mirror: %#v", ir.BackendCaps.Degraded)
	}
	if water.RenderPool == nil || *water.RenderPool || water.WaveSpeed > 1 || water.SurfaceSelenaWGSL == "" || water.SurfaceFragmentGLES == "" {
		t.Fatal("cove must keep the stable Selena water surface without a tank")
	}
}

func TestBlackglassCoastIBLAssets(t *testing.T) {
	var totalBytes int64
	for _, period := range []string{"daybreak", "high-sun", "ember-hour"} {
		value := BlackglassCoastProgram("overlook", period).SceneIR().Environment.IBL
		if value.SchemaVersion != 1 || value.BRDFModel == "" || len(value.RoughnessPerLevel) != 6 {
			t.Fatalf("%s IBL metadata is incomplete", period)
		}
		for _, descriptor := range []scene.TextureDescriptor{value.Radiance, value.Irradiance, value.BRDFLUT} {
			if !strings.HasPrefix(descriptor.URI, "/env/blackglass/") || descriptor.Format == "" {
				t.Fatalf("%s IBL product is not served: %#v", period, descriptor)
			}
			path := filepath.Join("..", "..", "..", "public", strings.TrimPrefix(descriptor.URI, "/"))
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("%s IBL product %s: %v", period, descriptor.URI, err)
			}
			totalBytes += info.Size()
		}
	}
	if totalBytes > 400*1024 {
		t.Fatalf("IBL products use %d bytes, budget 400 KiB", totalBytes)
	}
}

func TestBlackglassCoastModelAssetsStayWithinDeclaredBudget(t *testing.T) {
	var totalBytes, expandedVertices int
	for _, model := range BlackglassBeaconProgram().SceneIR().Models {
		path := filepath.Join("..", "..", "..", "public", strings.TrimPrefix(model.Src, "/"))
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read model %q: %v", model.Src, err)
		}
		if len(blob) < 20 || string(blob[:4]) != "glTF" || binary.LittleEndian.Uint32(blob[4:8]) != 2 {
			t.Fatalf("model %q is not GLB 2.0", model.Src)
		}
		jsonBytes := int(binary.LittleEndian.Uint32(blob[12:16]))
		if jsonBytes <= 0 || 20+jsonBytes > len(blob) || string(blob[16:20]) != "JSON" {
			t.Fatalf("model %q has an invalid GLB JSON chunk", model.Src)
		}
		var gltf struct {
			Accessors []struct {
				Count int `json:"count"`
			} `json:"accessors"`
			Meshes []struct {
				Primitives []struct {
					Indices int `json:"indices"`
				} `json:"primitives"`
			} `json:"meshes"`
		}
		if err := json.Unmarshal(blob[20:20+jsonBytes], &gltf); err != nil {
			t.Fatalf("decode model %q: %v", model.Src, err)
		}
		for _, mesh := range gltf.Meshes {
			for _, primitive := range mesh.Primitives {
				if primitive.Indices < 0 || primitive.Indices >= len(gltf.Accessors) {
					t.Fatalf("model %q has invalid index accessor %d", model.Src, primitive.Indices)
				}
				expandedVertices += gltf.Accessors[primitive.Indices].Count
			}
		}
		totalBytes += len(blob)
	}
	if totalBytes == 0 || totalBytes > 400*1024 || expandedVertices == 0 || expandedVertices > blackglassCoastExpandedVertexBudget {
		t.Fatalf("model assets = %d bytes, %d expanded vertices; budgets 400 KiB and %d vertices", totalBytes, expandedVertices, blackglassCoastExpandedVertexBudget)
	}
}

func TestBlackglassCoastHasAStableWorldAndNoAutonomousMotion(t *testing.T) {
	props := BlackglassBeaconProgram()
	if props.AutoRotate == nil || *props.AutoRotate || props.FillHeight == nil || !*props.FillHeight {
		t.Fatal("coast must be fill-height without autonomous camera motion")
	}
	want := []string{"coast-sun", "coast-sky", "beacon-fire", "beacon-terrace-light", "blackglass-cove", "shore", "basalt", "ruins", "beacon", "beacon-lens", "beacon-ember-plume", "arrival-stele", "keeper-ledger", "harbor-plaque"}
	got := make([]string, 0, len(props.Graph.Nodes))
	for _, node := range props.Graph.Nodes {
		switch value := node.(type) {
		case scene.DirectionalLight:
			got = append(got, value.ID)
		case scene.HemisphereLight:
			got = append(got, value.ID)
		case scene.PointLight:
			got = append(got, value.ID)
		case scene.SpotLight:
			got = append(got, value.ID)
		case scene.WaterSystem:
			got = append(got, value.ID)
		case scene.Model:
			got = append(got, value.ID)
		case scene.Mesh:
			got = append(got, value.ID)
			if value.Spin != (scene.Euler{}) || value.Drift != (scene.Vector3{}) {
				t.Errorf("mesh %q has autonomous motion", value.ID)
			}
		case scene.ComputeParticles:
			got = append(got, value.ID)
			if value.Count != blackglassEmberPlumeCount {
				t.Errorf("ember count = %d, want %d", value.Count, blackglassEmberPlumeCount)
			}
		case scene.HTML:
			got = append(got, value.ID)
			if value.Mode != scene.HTMLTexture || value.Fallback == "" {
				t.Errorf("world surface %q lacks texture mode or fallback", value.ID)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stable world node IDs = %#v", got)
	}
}
