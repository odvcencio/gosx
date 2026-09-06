package bundle

import (
	"encoding/binary"
	"math"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

func TestFogUniformAppended(t *testing.T) {
	if sceneUniformSize != 416 {
		t.Fatalf("fog vec4 absent: scene uniform size=%d", sceneUniformSize)
	}
	for _, d := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1), .6, math.MaxFloat64} {
		f := environmentFog(engine.RenderEnvironment{FogDensity: d})
		if f[0] != .5 || f[1] != .5 || f[2] != .5 || math.IsInf(float64(f[3]), 0) {
			t.Fatal(f)
		}
		if (f[3] > 0) != (d > 0 && !math.IsInf(d, 0)) {
			t.Fatalf("density %g: %v", d, f)
		}
		r := Renderer{}
		data := r.sceneUniformBytes(sceneUniformBlock{fog: f})
		if math.Float32frombits(binary.LittleEndian.Uint32(data[412:416])) != f[3] {
			t.Fatal("packing")
		}
	}
	if got := environmentFog(engine.RenderEnvironment{FogColor: "#ff0080", FogDensity: .5}); got != [4]float32{1, 0, 128.0 / 255, .5} {
		t.Fatalf("authored color %v", got)
	}
	for _, shader := range []string{litWGSL, skinnedLitWGSL()} {
		scene := regexp.MustCompile(`(?s)struct Scene\s*\{(.*?)\};`).FindStringSubmatch(shader)
		if len(scene) != 2 || !regexp.MustCompile(`(?s)lightParams\s*:\s*vec4<f32>,[^\n]*\n\s*fog\s*:\s*vec4<f32>,[^\n]*\n\s*$`).MatchString(scene[1]) {
			t.Fatal("Scene must append fog vec4 immediately after lightParams")
		}
		if !strings.Contains(shader, "distance(in.worldPos, scene.cameraPos.xyz)") || !strings.Contains(shader, "exp(-fogDistance * fogDistance)") {
			t.Fatal("fog shader missing")
		}
		if strings.Index(shader, "let transmission") > strings.Index(shader, "let fogDistance") {
			t.Fatal("fog before transmission")
		}
	}
}
