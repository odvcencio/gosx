package scene

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"testing"
)

// postEffectRoundTripCase is one concrete PostEffect and the IR value a JSON
// round trip must return. Every field of every effect carries a value that is
// not the runtime default, because a default would hide a dropped field: the IR
// marshaler substitutes the default for a zero, so a decoder that lost the field
// would still produce matching bytes.
type postEffectRoundTripCase struct {
	effect PostEffect
	wantIR PostEffectIR
}

// float32 inputs, kept as named constants so the expected float64 values are
// the exact widened bits and not a hand-typed decimal that only looks equal.
const (
	rtExposure      float32 = 1.35
	rtThreshold     float32 = 0.71
	rtStrength      float32 = 0.62
	rtRadius        float32 = 11.5
	rtScale         float32 = 0.25
	rtIntensity     float32 = 0.44
	rtContrast      float32 = 1.15
	rtSaturation    float32 = 0.83
	rtSSAORadius    float32 = 6.5
	rtSSAOIntensity float32 = 0.77
	rtSSAOBias      float32 = 0.03
	rtFocusDistance float32 = 12.5
	rtAperture      float32 = 0.08
	rtMaxBlur       float32 = 5.5
)

// postEffectRoundTripCases is keyed by the concrete PostEffect type name.
// TestPostEffectIRRoundTripCoversEveryType reads scene/postfx.go and fails when a
// PostEffect type has no entry here, so a new effect cannot ship without a
// decoder and a round-trip proof.
var postEffectRoundTripCases = map[string]postEffectRoundTripCase{
	"Tonemap": {
		effect: Tonemap{Mode: TonemapFilmic, Exposure: rtExposure},
		wantIR: TonemapIR{Mode: "filmic", Exposure: float64(rtExposure)},
	},
	"Bloom": {
		effect: Bloom{Threshold: rtThreshold, Strength: rtStrength, Radius: rtRadius, Scale: rtScale},
		wantIR: BloomIR{
			Threshold: float64(rtThreshold),
			Strength:  float64(rtStrength),
			Radius:    float64(rtRadius),
			Scale:     float64(rtScale),
		},
	},
	"Vignette": {
		effect: Vignette{Intensity: rtIntensity},
		wantIR: VignetteIR{Intensity: float64(rtIntensity)},
	},
	"ColorGrade": {
		effect: ColorGrade{Exposure: rtExposure, Contrast: rtContrast, Saturation: rtSaturation},
		wantIR: ColorGradeIR{
			Exposure:   float64(rtExposure),
			Contrast:   float64(rtContrast),
			Saturation: float64(rtSaturation),
		},
	},
	"SSAO": {
		effect: SSAO{Radius: rtSSAORadius, Intensity: rtSSAOIntensity, Bias: rtSSAOBias},
		wantIR: SSAOIR{
			Radius:    float64(rtSSAORadius),
			Intensity: float64(rtSSAOIntensity),
			Bias:      float64(rtSSAOBias),
		},
	},
	"DOF": {
		effect: DOF{FocusDistance: rtFocusDistance, Aperture: rtAperture, MaxBlur: rtMaxBlur},
		wantIR: DOFIR{
			FocusDistance: float64(rtFocusDistance),
			Aperture:      float64(rtAperture),
			MaxBlur:       float64(rtMaxBlur),
		},
	},
	"FXAA": {
		effect: FXAA{},
		wantIR: FXAAIR{},
	},
	"CustomPost": {
		effect: CustomPost{
			Name:  "flare-shield",
			Stage: CustomPostAfterTonemap,
			Material: &CustomMaterial{
				FragmentWGSL:  "fn fragmentMain() {}",
				VertexWGSL:    "fn vertexMain() {}",
				FragmentGLSL:  "void main() {}",
				VertexGLSL:    "void vmain() {}",
				ShaderBackend: "selena",
				ShaderLayout:  map[string]any{"group": "0", "binding": "4"},
				Uniforms:      map[string]any{"shieldStrength": 0.58},
			},
			Uniforms: map[string]any{"iris": true},
			DOMRegions: CustomPostDOMRegions{
				Selector: "[data-flare-shield]",
				Max:      5,
				Uniforms: DOMRegionUniforms{
					Count:  "shieldCount",
					Aspect: "shieldAspect",
					Rect:   "shield%dRect",
					Meta:   "shield%dMeta",
				},
				Bounds: CustomPostDOMRegionBounds{
					Mode:      CustomPostDOMRegionBoundsUnion,
					PaddingPx: 64,
				},
			},
		},
		wantIR: CustomPostIR{
			Name:          "flare-shield",
			Stage:         "afterTonemap",
			FragmentWGSL:  "fn fragmentMain() {}",
			VertexWGSL:    "fn vertexMain() {}",
			FragmentGLSL:  "void main() {}",
			VertexGLSL:    "void vmain() {}",
			ShaderBackend: "selena",
			ShaderLayout:  map[string]any{"group": "0", "binding": "4"},
			Uniforms:      map[string]any{"shieldStrength": 0.58, "iris": true},
			DOMRegions: &CustomPostDOMRegionsIR{
				Selector: "[data-flare-shield]",
				Max:      5,
				Uniforms: DOMRegionUniformsIR{
					Count:  "shieldCount",
					Aspect: "shieldAspect",
					Rect:   "shield%dRect",
					Meta:   "shield%dMeta",
				},
				Bounds: &CustomPostDOMRegionBoundsIR{
					Mode:      "union",
					PaddingPx: 64,
				},
			},
		},
	},
}

// TestSceneIRRoundTripsEveryPostEffect proves a scene carrying one post effect
// survives marshal and unmarshal. Before the kind dispatcher existed, the
// unmarshal failed for every post effect, and the asymmetry stayed hidden because
// marshal worked: only a caller that read back what it wrote ever saw it. The
// scene harness reads back, so `gosx scene check --json` failed on any scene with
// a post effect.
//
// Each case asserts four things and reports all four, not just the first:
//   - the typed effect lowers to the expected IR value;
//   - the decoded IR equals that value field for field;
//   - a second marshal reproduces the first byte for byte;
//   - the decoded chain still has exactly one effect.
func TestSceneIRRoundTripsEveryPostEffect(t *testing.T) {
	for name, testCase := range postEffectRoundTripCases {
		t.Run(name, func(t *testing.T) {
			props := Props{PostFX: PostFX{Effects: []PostEffect{testCase.effect}}}
			lowered := props.SceneIR()
			if len(lowered.PostEffects) != 1 {
				t.Fatalf("lowered post effects = %d, want 1", len(lowered.PostEffects))
			}
			if !reflect.DeepEqual(lowered.PostEffects[0], testCase.wantIR) {
				t.Errorf("lowered IR:\n got %#v\nwant %#v", lowered.PostEffects[0], testCase.wantIR)
			}

			data, err := json.Marshal(lowered)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var back SceneIR
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal %s: %v", data, err)
			}
			if len(back.PostEffects) != 1 {
				t.Fatalf("decoded post effects = %d, want 1 (payload %s)", len(back.PostEffects), data)
			}
			if !reflect.DeepEqual(back.PostEffects[0], testCase.wantIR) {
				t.Errorf("decoded IR:\n got %#v\nwant %#v", back.PostEffects[0], testCase.wantIR)
			}

			again, err := json.Marshal(back)
			if err != nil {
				t.Fatalf("second marshal: %v", err)
			}
			if string(again) != string(data) {
				t.Errorf("second marshal differs:\n got %s\nwant %s", again, data)
			}
		})
	}
}

// TestSceneIRRoundTripsWholePostChain proves the decoder keeps chain order and
// every entry. Post-FX order is semantic: FXAA before Tonemap searches HDR data
// and searches wrong, so a decoder that reordered or merged entries would render
// a different image with no error.
func TestSceneIRRoundTripsWholePostChain(t *testing.T) {
	names := sortedCaseNames()
	effects := make([]PostEffect, 0, len(names))
	want := make([]PostEffectIR, 0, len(names))
	for _, name := range names {
		effects = append(effects, postEffectRoundTripCases[name].effect)
		want = append(want, postEffectRoundTripCases[name].wantIR)
	}

	data, err := json.Marshal(Props{PostFX: PostFX{Effects: effects}}.SceneIR())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back SceneIR
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.PostEffects) != len(want) {
		t.Fatalf("decoded %d effects, want %d", len(back.PostEffects), len(want))
	}
	for index := range want {
		if !reflect.DeepEqual(back.PostEffects[index], want[index]) {
			t.Errorf("effect %d (%s):\n got %#v\nwant %#v", index, names[index], back.PostEffects[index], want[index])
		}
	}
}

// TestPostEffectIRRoundTripCoversEveryType reads the source of truth instead of
// trusting a hand-kept list. It parses scene/postfx.go for every type that
// implements PostEffect, and scene/postfx_ir.go for every type that implements
// PostEffectIR, then requires:
//   - one round-trip case per PostEffect type;
//   - one decoder entry per PostEffectIR type, and no decoder for a type that
//     does not exist;
//   - the kind each IR type marshals is the key its decoder is registered under.
//
// So a new effect that ships without a decoder fails here, at the seam where a
// missing decoder becomes a dropped effect.
func TestPostEffectIRRoundTripCoversEveryType(t *testing.T) {
	effectTypes := methodReceiverTypes(t, "postfx.go", "isPostEffect")
	if len(effectTypes) == 0 {
		t.Fatal("found no PostEffect type in postfx.go; the parser or the file layout changed")
	}
	for _, name := range effectTypes {
		if _, ok := postEffectRoundTripCases[name]; !ok {
			t.Errorf("PostEffect type %s has no round-trip case in postEffectRoundTripCases", name)
		}
	}
	for name := range postEffectRoundTripCases {
		if !slicesContain(effectTypes, name) {
			t.Errorf("round-trip case %s names no PostEffect type in postfx.go", name)
		}
	}

	irTypes := methodReceiverTypes(t, "postfx_ir.go", "legacyProps")
	if len(irTypes) == 0 {
		t.Fatal("found no PostEffectIR type in postfx_ir.go; the parser or the file layout changed")
	}
	decoded := make(map[string]string, len(postEffectIRDecoders))
	for kind := range postEffectIRDecoders {
		effect, err := DecodePostEffectIR([]byte(`{"kind":` + jsonString(kind) + `}`))
		if err != nil {
			t.Errorf("decode kind %q: %v", kind, err)
			continue
		}
		typeName := reflect.TypeOf(effect).Name()
		decoded[typeName] = kind

		// The kind literal lives inside each MarshalJSON. Prove the literal and
		// the registry key agree, or a renamed literal would route to the wrong
		// decoder and lose fields silently.
		encoded, err := json.Marshal(effect)
		if err != nil {
			t.Errorf("marshal %s: %v", typeName, err)
			continue
		}
		var probe struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(encoded, &probe); err != nil {
			t.Errorf("read kind of %s: %v", typeName, err)
			continue
		}
		if probe.Kind != kind {
			t.Errorf("%s marshals kind %q but is registered under %q", typeName, probe.Kind, kind)
		}
	}
	for _, name := range irTypes {
		if _, ok := decoded[name]; !ok {
			t.Errorf("PostEffectIR type %s has no entry in postEffectIRDecoders", name)
		}
	}
	for name := range decoded {
		if !slicesContain(irTypes, name) {
			t.Errorf("postEffectIRDecoders decodes %s, which implements no legacyProps in postfx_ir.go", name)
		}
	}
}

// TestDecodePostEffectIRRejectsUnknownKind proves an unknown kind fails loudly.
// A skipped entry would render a different scene and report nothing, so silence
// is the one outcome this must never produce.
func TestDecodePostEffectIRRejectsUnknownKind(t *testing.T) {
	cases := map[string]string{
		"unknown kind": `{"kind":"filmGrain","intensity":0.4}`,
		"missing kind": `{"intensity":0.4}`,
		"empty kind":   `{"kind":""}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			effect, err := DecodePostEffectIR([]byte(payload))
			if err == nil {
				t.Fatalf("decode returned %#v and no error", effect)
			}
			if !errors.Is(err, ErrUnknownPostEffectKind) {
				t.Errorf("error = %v, want it to wrap ErrUnknownPostEffectKind", err)
			}
		})
	}

	// The same rejection has to survive the whole-scene path, with the failing
	// entry's index, or a caller cannot find which effect broke.
	var back SceneIR
	err := json.Unmarshal([]byte(`{"postEffects":[{"kind":"bloom"},{"kind":"filmGrain"}]}`), &back)
	if err == nil {
		t.Fatalf("SceneIR decoded an unknown post effect: %#v", back.PostEffects)
	}
	if !errors.Is(err, ErrUnknownPostEffectKind) {
		t.Errorf("SceneIR error = %v, want it to wrap ErrUnknownPostEffectKind", err)
	}
	if len(back.PostEffects) != 0 {
		t.Errorf("SceneIR kept %d effects from a failed decode", len(back.PostEffects))
	}
}

// TestDecodePostEffectIRsKeepsOrder proves the exported slice decoder returns the
// encoded order. A post chain is ordered, so a reorder is a rendering change.
func TestDecodePostEffectIRsKeepsOrder(t *testing.T) {
	payload := []byte(`[{"kind":"ssao","radius":3},{"kind":"bloom","intensity":0.9},{"kind":"toneMapping","exposure":2},{"kind":"fxaa"}]`)
	effects, err := DecodePostEffectIRs(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []PostEffectIR{
		SSAOIR{Radius: 3},
		BloomIR{Strength: 0.9},
		TonemapIR{Exposure: 2},
		FXAAIR{},
	}
	if len(effects) != len(want) {
		t.Fatalf("decoded %d effects, want %d", len(effects), len(want))
	}
	for index := range want {
		if !reflect.DeepEqual(effects[index], want[index]) {
			t.Errorf("effect %d:\n got %#v\nwant %#v", index, effects[index], want[index])
		}
	}
}

// TestBloomIRDecodesIntensityIntoStrength pins the one field name the wire and
// the Go API disagree on. The shader uniform is u_intensity and the Go field is
// Strength, so a reflection-only decoder leaves Strength at zero, the marshaler
// then substitutes the 0.5 default, and a bloom silently loses its intensity.
func TestBloomIRDecodesIntensityIntoStrength(t *testing.T) {
	var bloom BloomIR
	if err := json.Unmarshal([]byte(`{"kind":"bloom","threshold":0.9,"intensity":0.31,"radius":7,"scale":0.5}`), &bloom); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := BloomIR{Threshold: 0.9, Strength: 0.31, Radius: 7, Scale: 0.5}
	if bloom != want {
		t.Fatalf("bloom = %#v, want %#v", bloom, want)
	}
}

func sortedCaseNames() []string {
	names := make([]string, 0, len(postEffectRoundTripCases))
	for name := range postEffectRoundTripCases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// methodReceiverTypes parses one file in this package and returns every type that
// declares the named method. It reads the source rather than a registry, because
// a registry is exactly the thing a new type forgets to join.
func methodReceiverTypes(t *testing.T, file, method string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var names []string
	for _, decl := range parsed.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv == nil || len(function.Recv.List) != 1 || function.Name.Name != method {
			continue
		}
		expression := function.Recv.List[0].Type
		if star, isStar := expression.(*ast.StarExpr); isStar {
			expression = star.X
		}
		identifier, ok := expression.(*ast.Ident)
		if !ok {
			continue
		}
		names = append(names, identifier.Name)
	}
	sort.Strings(names)
	return names
}
