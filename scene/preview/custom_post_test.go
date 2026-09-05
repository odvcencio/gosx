package preview_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/preview"
)

func customPostFixture(t *testing.T) (scene.Props, preview.Options, scene.CustomPost) {
	t.Helper()
	material, _, err := scene.CompileSelenaPost([]byte(`material MagentaUnsupported kind post {
 surface(post) -> color { return rgb(1.0, 0.0, 1.0, 1.0) }
}`), scene.SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	camera := scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5), FOV: 50, Near: .1, Far: 30}
	props := scene.Props{Background: "#102030", Camera: camera,
		Environment: scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: .2, SkyColor: "#ddeeff", SkyIntensity: .1, GroundColor: "#223344", GroundIntensity: .1},
		Graph:       scene.NewGraph(scene.DirectionalLight{Color: "#ffffff", Intensity: 1, Direction: scene.Vec3(-.4, -1, -.6)}, scene.Mesh{ID: "sphere", Geometry: scene.SphereGeometry{Radius: 1.2, Segments: 16}, Material: scene.StandardMaterial{Color: "#50d0a0", Roughness: .6}})}
	return props, preview.Options{Width: 96, Height: 64, Camera: &camera, Background: props.Background, DisableShadows: true}, scene.CustomPost{Name: "magenta", Material: &material}
}

func TestNativeCustomPostDiagnosticIdentityAndExplicitDisable(t *testing.T) {
	props, opts, custom := customPostFixture(t)
	unnamed := custom
	unnamed.Name = ""
	props.PostFX = scene.PostFX{Effects: []scene.PostEffect{scene.Vignette{Intensity: .4}, custom, custom, unnamed, unnamed}}
	props.Graph.Nodes = append(props.Graph.Nodes, scene.Label{ID: "caption", Text: "not a raster label"})
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"postEffects[1]:magenta", "postEffects[2]:magenta", "postEffects[3]", "postEffects[4]"}
	for _, disabled := range []bool{false, true} {
		opts.DisablePostFX = disabled
		for _, mode := range []string{"typed", "ir", "json"} {
			t.Run(mode+map[bool]string{false: "/enabled", true: "/disabled"}[disabled], func(t *testing.T) {
				var result *preview.Result
				switch mode {
				case "typed":
					result, err = preview.Render(props, opts)
				case "ir":
					result, err = preview.RenderIR(props.SceneIR(), opts)
				case "json":
					result, err = preview.RenderJSON(data, opts)
				}
				if err != nil {
					t.Fatal(err)
				}
				var targets []string
				label := false
				for _, d := range result.Bundle.Diagnostics {
					if d.Code == "scene.preview.unsupported_custom-post" {
						targets = append(targets, d.Target)
						if d.Backend != "headless" {
							t.Fatalf("wrong backend %+v", d)
						}
					}
					label = label || d.Code == "scene.preview.unsupported_label" && d.Target == "caption"
				}
				if !label {
					t.Error("custom handling removed unrelated label diagnostic")
				}
				if disabled {
					if len(targets) != 0 || len(result.Bundle.PostEffects) != 0 {
						t.Fatalf("explicit disable reported unsupported execution: %+v", result.Bundle.Diagnostics)
					}
				} else if !reflect.DeepEqual(targets, want) {
					t.Fatalf("ambiguous/reordered targets=%v want=%v", targets, want)
				}
			})
		}
	}
}

func TestNativeCustomPostMissingSourceStagesAndNoEffectControls(t *testing.T) {
	props, opts, custom := customPostFixture(t)
	bare, err := preview.Render(props, opts)
	if err != nil {
		t.Fatal(err)
	}
	after := custom
	after.Stage = scene.CustomPostAfterTonemap
	after.Uniforms = map[string]any{"secret-value": 12345}
	after.DOMRegions = scene.CustomPostDOMRegions{Selector: ".hud"}
	for _, tc := range []struct {
		name             string
		effect           scene.CustomPost
		warning, missing bool
	}{
		{"none", scene.CustomPost{}, false, false},
		{"nil-material", scene.CustomPost{Name: "nil"}, false, false},
		{"empty-material", scene.CustomPost{Name: "empty", Material: &scene.CustomMaterial{}}, true, true},
		{"glsl-only", scene.CustomPost{Name: "glsl", Material: &scene.CustomMaterial{FragmentGLSL: "void main(){gl_FragColor=vec4(1.0);}"}}, true, true},
		{"before", custom, true, false},
		{"after-uniform", after, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props.PostFX = scene.PostFX{Effects: []scene.PostEffect{tc.effect}}
			result, err := preview.Render(props, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bare.Image.Pix, result.Image.Pix) {
				t.Fatal("degraded/absent custom effect changed reference pixels")
			}
			found := 0
			for _, d := range result.Bundle.Diagnostics {
				if d.Code == "scene.preview.unsupported_custom-post" {
					found++
					if strings.Contains(d.Message, "12345") || strings.Contains(d.Message, "fragmentMain") {
						t.Fatal("diagnostic leaked shader/uniform contents")
					}
					if tc.missing && !strings.Contains(d.Message, "no WGSL") {
						t.Fatalf("missing-source reason not specific: %+v", d)
					}
				}
			}
			if (found == 1) != tc.warning {
				t.Fatalf("warning count=%d expected=%v diagnostics=%+v", found, tc.warning, result.Bundle.Diagnostics)
			}
		})
	}
}

func TestNativeCustomPostPosterAndTelemetrySerialization(t *testing.T) {
	props, opts, custom := customPostFixture(t)
	props.PostFX = scene.PostFX{Effects: []scene.PostEffect{custom}}
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	for _, allow := range []bool{false, true} {
		po := preview.NewPosterOptions(opts)
		po.Floors.AllowDroppedRecords = allow
		for _, mode := range []string{"props", "json", "result"} {
			t.Run(mode+map[bool]string{false: "/reject", true: "/explicit-opt-in"}[allow], func(t *testing.T) {
				var poster *preview.Poster
				switch mode {
				case "props":
					poster, err = preview.PosterFromProps(props, po)
				case "json":
					poster, err = preview.PosterFromJSON(data, po)
				case "result":
					var result *preview.Result
					result, err = preview.Render(props, opts)
					if err == nil {
						poster, err = preview.PosterFromResult(result, po)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if poster.Fidelity.OK != allow || len(poster.Fidelity.Dropped) != 1 || len(poster.Diagnostics) != 1 {
					t.Fatalf("poster hid degradation %+v", poster.Fidelity)
				}
				var buf bytes.Buffer
				err = preview.WritePoster(&buf, poster)
				if allow {
					if err != nil || buf.Len() == 0 {
						t.Fatalf("explicit opt-in failed %v", err)
					}
				} else if !errors.Is(err, preview.ErrLowFidelity) || buf.Len() != 0 {
					t.Fatalf("default gate wrote degraded poster: %v bytes=%d", err, buf.Len())
				}
				wire, err := json.Marshal(poster)
				if err != nil {
					t.Fatal(err)
				}
				var copy preview.Poster
				if err := json.Unmarshal(wire, &copy); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(copy.Diagnostics, poster.Diagnostics) || !reflect.DeepEqual(copy.Fidelity.Dropped, poster.Fidelity.Dropped) {
					t.Fatal("poster serialization dropped warning")
				}
			})
		}
	}
	session := harness.New(props, opts)
	if _, err := session.Render(0); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(session.Report())
	if err != nil {
		t.Fatal(err)
	}
	var report harness.Report
	if err := json.Unmarshal(wire, &report); err != nil {
		t.Fatal(err)
	}
	var diagnostics []engine.RenderDiagnostic
	for _, event := range report.Events {
		if event.Frame != nil {
			diagnostics = append(diagnostics, event.Frame.Diagnostics...)
		}
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "scene.preview.unsupported_custom-post" || diagnostics[0].Target != "postEffects[0]:magenta" {
		t.Fatalf("wire telemetry lost exact warning: %+v", diagnostics)
	}
}

func TestNativeCustomPostPreservesPixelsAndSupportedNeighborOrder(t *testing.T) {
	props, opts, custom := customPostFixture(t)
	render := func(effects ...scene.PostEffect) *preview.Result {
		props.PostFX = scene.PostFX{Effects: effects}
		result, err := preview.Render(props, opts)
		if err != nil {
			t.Fatal(err)
		}
		if result.Stats.DeviceLost {
			t.Fatal("device lost")
		}
		return result
	}
	bare := render()
	metrics := preview.MeasureFrame(bare.Image)
	if metrics.UniqueColors < 16 || metrics.Coverage < .05 {
		t.Fatalf("insensitive source scene %+v", metrics)
	}
	v := scene.Vignette{Intensity: .7}
	c := scene.ColorGrade{Exposure: 1.1, Contrast: .6, Saturation: .5}
	ordered := render(v, c)
	reversed := render(c, v)
	if bytes.Equal(ordered.Image.Pix, bare.Image.Pix) || bytes.Equal(ordered.Image.Pix, reversed.Image.Pix) {
		t.Fatal("supported neighbor order has no pixel sensitivity")
	}
	for _, tc := range []struct {
		name    string
		effects []scene.PostEffect
		want    *preview.Result
	}{
		{"custom-only", []scene.PostEffect{custom}, bare},
		{"start", []scene.PostEffect{custom, v, c}, ordered},
		{"middle", []scene.PostEffect{v, custom, c}, ordered},
		{"end", []scene.PostEffect{v, c, custom}, ordered},
		{"even-ping-pong", []scene.PostEffect{custom, v, custom, c}, ordered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := render(tc.effects...)
			if !bytes.Equal(got.Image.Pix, tc.want.Image.Pix) {
				t.Fatal("unsupported custom pass wiped or reordered supported pixels")
			}
		})
	}
	t.Logf("visible colors=%d; supported-order changed pixels=%d", metrics.UniqueColors, countImageDifferences(ordered.Image, reversed.Image))
}

func TestNativeCustomPostDegradationReachesCallers(t *testing.T) {
	props, opts, custom := customPostFixture(t)
	props.PostFX = scene.PostFX{Effects: []scene.PostEffect{custom}}
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"typed", "ir", "json"} {
		t.Run(mode, func(t *testing.T) {
			var result *preview.Result
			switch mode {
			case "typed":
				result, err = preview.Render(props, opts)
			case "ir":
				result, err = preview.RenderIR(props.SceneIR(), opts)
			case "json":
				result, err = preview.RenderJSON(data, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, d := range result.Bundle.Diagnostics {
				if d.Code == "scene.preview.unsupported_custom-post" && d.Backend == "headless" && d.Target == "postEffects[0]:magenta" {
					found++
				}
			}
			if found != 1 {
				t.Errorf("expected one named headless custom-post diagnostic, got %+v", result.Bundle.Diagnostics)
			}
			poster, err := preview.PosterFromResult(result, preview.NewPosterOptions(opts))
			if err != nil {
				t.Fatal(err)
			}
			if len(poster.Fidelity.Dropped) != 1 || poster.Fidelity.OK {
				t.Errorf("missing caller-visible poster degradation: %+v", poster.Fidelity)
			}
			var buf bytes.Buffer
			if err := preview.WritePoster(&buf, poster); err == nil {
				t.Error("default poster writer accepted silently degraded custom pass")
			}
		})
	}
	session := harness.New(props, opts)
	if _, err := session.Render(0); err != nil {
		t.Fatal(err)
	}
	report := session.Report()
	found := false
	for _, event := range report.Events {
		if event.Frame != nil {
			for _, d := range event.Frame.Diagnostics {
				found = found || d.Code == "scene.preview.unsupported_custom-post"
			}
		}
	}
	if !found {
		t.Error("headless harness frame telemetry omitted custom-post degradation")
	}
}
