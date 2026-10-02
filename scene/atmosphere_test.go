package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestAtmosphereContracts(t *testing.T) {
	p := Props{Environment: Environment{Haze: &Haze{Density: 0.002, HeightFalloff: 0.06, SunScatter: 0.4}}, PostFX: PostFX{Effects: []PostEffect{GodRays{Intensity: 0.2, Decay: 0.9, Density: 0.8, Samples: 24}, Tonemap{Mode: TonemapAgX}, Grain{Intensity: 0.01}}}}
	for _, env := range []any{p.SceneIR().Environment, p.CanonicalIR().Environment} {
		b, e := json.Marshal(env)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(string(b), `"haze":{"density":0.002,"heightFalloff":0.06,"sunScatter":0.4}`) {
			t.Fatalf("wire %s", b)
		}
	}
	effects := p.SceneIR().PostEffects
	if effects[1].(TonemapIR).Mode != "agx" {
		t.Fatal("AgX mode lost")
	}
	b, e := json.Marshal(effects)
	if e != nil {
		t.Fatal(e)
	}
	round, e := DecodePostEffectIRs(b)
	if e != nil {
		t.Fatal(e)
	}
	b2, _ := json.Marshal(round)
	if string(b) != string(b2) {
		t.Fatalf("round trip: %s / %s", b, b2)
	}
	defaults := (PostFX{Effects: []PostEffect{GodRays{}, Grain{}}}).sceneIR()
	b, _ = json.Marshal(defaults)
	if string(b) != `[{"kind":"godRays"},{"kind":"grain"}]` {
		t.Fatalf("Go applied browser defaults: %s", b)
	}
	h := normalizeHaze(&Haze{Density: 2, HeightFalloff: 3, SunScatter: 4})
	if h.Density != 0.1 || h.HeightFalloff != 1 || h.SunScatter != 1 {
		t.Fatalf("haze clamp %#v", h)
	}
	b, _ = json.Marshal((Props{}).SceneIR().Environment)
	if strings.Contains(string(b), "haze") {
		t.Fatalf("existing wire changed %s", b)
	}
	rays := (PostFX{Effects: []PostEffect{GodRays{Intensity: 3, Decay: 2, Density: 4, Samples: 90}, Grain{Intensity: 0.5}}}).sceneIR()
	r := rays[0].(GodRaysIR)
	if r.Intensity != 2 || r.Decay != 1 || r.Density != 2 || r.Samples != 64 || rays[1].(GrainIR).Intensity != 0.1 {
		t.Fatalf("post clamps %#v", rays)
	}
	if p.SceneIR().Environment.legacyProps()["haze"] == nil {
		t.Fatal("legacy environment lost haze")
	}
}

func TestAtmosphereInvalidNumbersRemainSerializable(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)
	p := Props{Environment: Environment{Haze: &Haze{Density: nan, HeightFalloff: inf, SunScatter: nan}, Sky: &Sky{Mode: "physical", Clouds: &SkyClouds{Coverage: nan, Altitude: inf, Scale: nan, Speed: inf, Direction: nan, Opacity: inf}}, Ocean: &Ocean{Reflections: &OceanReflections{Mode: "ssr", Resolution: inf, Strength: nan}}}, PostFX: PostFX{Effects: []PostEffect{GodRays{Intensity: float32(nan), Decay: float32(inf), Density: float32(nan)}, Grain{Intensity: float32(inf)}}}}
	if _, err := json.Marshal(p.SceneIR()); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(p.CanonicalIR()); err != nil {
		t.Fatal(err)
	}
}
