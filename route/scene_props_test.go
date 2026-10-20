package route

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/internal/sceneprobe"
	"m31labs.dev/gosx/scene"
)

func TestScene3DTypedPropsRetainSceneWhenAddingChildren(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.gsx")
	source := `package docs
func Page() Node {
 return <Scene3D {...data.scene}>
  <Mesh id="child" />
 </Scene3D>
}
`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := &RouteContext{Data: map[string]any{"scene": scene.Props{Graph: scene.NewGraph(scene.Mesh{ID: "typed", Geometry: scene.BoxGeometry{}})}}}
	if _, err := DefaultFileRenderer(ctx, FilePage{FilePath: path, Pattern: "/"}); err != nil {
		t.Fatal(err)
	}
	head := gosx.RenderHTML(ctx.Runtime().Head())
	for _, id := range []string{`"id":"typed"`, `"id":"child"`} {
		if !strings.Contains(head, id) {
			t.Fatalf("missing %s after merge", id)
		}
	}
}

func TestScene3DTypedMergeTargetsRetainUntouchedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.gsx")
	source := `package docs
func Page() Node {
 return <Scene3D {...data.scene}>
  <Camera z={7} />
  <Environment fogDensity={0.02} />
  <PointLight id="child-light" intensity={2} />
  <PostFX.Vignette intensity={0.3} />
 </Scene3D>
}
`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	camera := scene.IRCamera{Kind: "perspective", X: 1, Y: 2, Z: 5, RotationX: 0.1, RotationY: 0.2, RotationZ: 0.3, FOV: 54, PortraitFOV: 48, Near: 0.1, Far: 96, TransitionMS: 250}
	light := scene.LightIR{ID: "typed-light", Kind: "directional", Color: "#abcdef", Intensity: 0.8}
	effect := scene.BloomIR{Threshold: 0.7, Strength: 0.4, Radius: 3}
	environment := scene.EnvironmentIR{AmbientColor: "#123456", AmbientIntensity: 0.5, FogColor: "#654321", FogDensity: 0.01, EnvMap: "/sky.hdr", Exposure: 1.2}
	wireObject := func(value any) map[string]any {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		return fields
	}
	for _, shape := range []string{"typed-fields", "typed-scene", "raw-scene"} {
		t.Run(shape, func(t *testing.T) {
			var sceneValue any = map[string]any{"environment": environment, "lights": []scene.LightIR{light}, "postEffects": []scene.PostEffectIR{effect}}
			if shape != "typed-fields" {
				sceneValue = scene.SceneIR{Environment: environment, Lights: []scene.LightIR{light}, PostEffects: []scene.PostEffectIR{effect}}
				if shape == "raw-scene" {
					raw, err := json.Marshal(sceneValue)
					if err != nil {
						t.Fatal(err)
					}
					sceneValue = json.RawMessage(raw)
				}
			}
			ctx := &RouteContext{Data: map[string]any{"scene": map[string]any{"camera": camera, "scene": sceneValue}}}
			if _, err := DefaultFileRenderer(ctx, FilePage{FilePath: path, Pattern: "/"}); err != nil {
				t.Fatal(err)
			}
			head := gosx.RenderHTML(ctx.Runtime().Head())
			_, tail, ok := strings.Cut(head, `<script id="gosx-manifest"`)
			if !ok {
				t.Fatal("missing engine manifest")
			}
			_, manifest, _ := strings.Cut(tail, ">")
			manifest, _, _ = strings.Cut(manifest, "</script>")
			var entries struct {
				Engines []struct{ Props json.RawMessage }
			}
			if err := json.Unmarshal([]byte(manifest), &entries); err != nil {
				t.Fatal(err)
			}
			if len(entries.Engines) != 1 {
				t.Fatalf("engines = %d, want 1", len(entries.Engines))
			}
			props := wireObject(entries.Engines[0].Props)
			wantCamera := wireObject(camera)
			wantCamera["z"] = float64(7)
			if !reflect.DeepEqual(props["camera"], wantCamera) {
				t.Errorf("camera = %#v, want %#v", props["camera"], wantCamera)
			}
			mergedScene := props["scene"].(map[string]any)
			wantEnvironment := wireObject(environment)
			wantEnvironment["fogDensity"] = 0.02
			if !reflect.DeepEqual(mergedScene["environment"], wantEnvironment) {
				t.Errorf("environment = %#v, want %#v", mergedScene["environment"], wantEnvironment)
			}
			for key, original := range map[string]any{"lights": light, "postEffects": effect} {
				records, _ := mergedScene[key].([]any)
				if len(records) != 2 || !reflect.DeepEqual(records[0], wireObject(original)) {
					t.Errorf("%s = %#v, want original record followed by child", key, records)
				}
			}
		})
	}
}

func TestScene3DProbeCapabilitiesTrimComputeKinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.gsx")
	if err := os.WriteFile(path, []byte(`package docs
func Page() Node {
 return <Scene3D capabilities="canvas" {...data.scene} />
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"compute-particles", " \tcompute-particles\n "} {
		for _, nested := range []bool{false, true} {
			props := map[string]any{"nodes": []map[string]any{{"kind": kind, "id": "  particles  "}}}
			if nested {
				props = map[string]any{"scene": props}
			}
			raw, err := json.Marshal(props)
			if err != nil {
				t.Fatal(err)
			}
			probe := sceneprobe.Decode(raw)
			if probe.Err != nil {
				t.Fatal(probe.Err)
			}
			legacy := validateScene3DCompilerCapabilities(raw, []engine.Capability{engine.CapCanvas})
			got := validateScene3DProbeCapabilities(probe, []engine.Capability{engine.CapCanvas})
			if legacy == nil || got == nil || got.Error() != legacy.Error() {
				t.Errorf("kind=%q nested=%v: probe error = %v, legacy = %v", kind, nested, got, legacy)
			}
			ctx := &RouteContext{Data: map[string]any{"scene": props}}
			if _, err := DefaultFileRenderer(ctx, FilePage{FilePath: path, Pattern: "/"}); err == nil || !strings.Contains(err.Error(), legacy.Error()) {
				t.Errorf("kind=%q nested=%v: route error = %v", kind, nested, err)
			}
		}
	}
}
