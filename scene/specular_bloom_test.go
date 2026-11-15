package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBloomSourceRoundTripAndDefaultCompatibility(t *testing.T) {
	for _, source := range []BloomSource{"", BloomSourceColor, "unknown", BloomSourceSpecular} {
		effects := (PostFX{Effects: []PostEffect{Bloom{Source: source, Mode: "mip", Strength: .1}}}).sceneIR()
		raw, err := json.Marshal(effects[0])
		if err != nil {
			t.Fatal(err)
		}
		var got BloomIR
		if err = json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		want := ""
		if source == BloomSourceSpecular {
			want = "specular"
		}
		if got.Source != want {
			t.Fatalf("source %q -> %s", source, raw)
		}
		again, _ := json.Marshal(got)
		if string(raw) != string(again) {
			t.Fatalf("unstable roundtrip %s -> %s", raw, again)
		}
		if want == "" && strings.Contains(string(raw), `"source"`) {
			t.Fatal("default adds wire payload")
		}
	}
}
