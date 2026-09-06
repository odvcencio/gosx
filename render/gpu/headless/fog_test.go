package headless

import (
	"math"
	"testing"

	"m31labs.dev/gosx/render/gpu"
)

func TestFogLightingDecode(t *testing.T) {
	b := &Buffer{data: make([]byte, sceneUniformSize)}
	want := [4]float32{.2, .4, .6, .7}
	for i, v := range want {
		writeFloat32(b.data, 400+i*4, v)
	}
	p := &RenderPassEncoder{bindGroups: map[int]*BindGroup{0: {desc: gpu.BindGroupDesc{Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: b}}}}}}
	if got := p.activeLighting().fog; got != want {
		t.Fatalf("decoded %v, want %v", got, want)
	}
}

func TestFogLitDistanceAndTransmission(t *testing.T) {
	for _, transmission := range []float32{0, .8} {
		m := defaultMaterialState()
		m.transmission = transmission
		for _, world := range [][3]float32{{1, 2, 4}, {4, 6, 3}, {1, 2, 13}} {
			l := defaultSceneLighting()
			l.cameraPos = [3]float32{1, 2, 3}
			f := fragment{world: world, base: [3]float32{.8, .3, .1}, normal: [3]float32{0, 0, 1}}
			plain := newLitProgram(l, m)
			base := plain.shade(f)
			l.fog = [4]float32{.1, .7, .9, .2}
			fogged := newLitProgram(l, m)
			got := fogged.shade(f)
			d2 := float64(0)
			for i := range world {
				d := float64(world[i] - l.cameraPos[i])
				d2 += d * d
			}
			w := float32(math.Exp(-float64(l.fog[3]) * float64(l.fog[3]) * d2))
			for i := range got {
				want := l.fog[i]*(1-w) + base[i]*w
				if math.Abs(float64(got[i]-want)) > 1e-6 {
					t.Fatalf("world=%v transmission=%g got=%v want[%d]=%g", world, transmission, got, i, want)
				}
			}
			l.fog[3] = 0
			inert := newLitProgram(l, m)
			if inert.shade(f) != base {
				t.Fatal("zero fog changed material")
			}
		}
	}
}
