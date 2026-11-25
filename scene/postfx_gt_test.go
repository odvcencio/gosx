package scene

import (
	"encoding/json"
	"testing"
)

// The Uchimura curve reaches the bundle with its contrast, and other
// curves keep their exact wire shape.
func TestTonemapGTCarriesContrast(t *testing.T) {
	p := Props{PostFX: PostFX{Effects: []PostEffect{Tonemap{Mode: TonemapGT, Exposure: 1.13, Contrast: 1.1}}}}
	ir := p.SceneIR().PostEffects[0].(TonemapIR)
	if ir.Mode != "gt" || ir.Contrast != float64(float32(1.1)) {
		t.Fatalf("lowered %+v", ir)
	}
	b, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	var back TonemapIR
	if err := json.Unmarshal(b, &back); err != nil || back != ir {
		t.Fatalf("round trip %s -> %+v (%v)", b, back, err)
	}
	plain, _ := json.Marshal(TonemapIR{Mode: "aces", Exposure: .8})
	if string(plain) != `{"kind":"toneMapping","exposure":0.8,"mode":"aces"}` {
		t.Fatalf("aces wire changed: %s", plain)
	}
	if got := (Props{Environment: Environment{ToneMapping: "gt", Exposure: 1.2}}).SceneIR().PostEffects; len(got) != 1 || got[0].(TonemapIR).Mode != "gt" {
		t.Fatalf("environment gt: %+v", got)
	}
}
