package headless

import (
	"fmt"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu"
	"math"
	"testing"
)

func TestViewportProjectsEdgesAndCenters(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {2, 2}, {8, 6}, {9, 7}, {65, 65}} {
		for _, p := range [][3]float32{{-1, 1, 0}, {1, -1, 0}, {0, 0, 0}} {
			wantX := (p[0] + 1) * float32(size[0]) / 2
			wantY := (1 - p[1]) * float32(size[1]) / 2
			x, y, _, ok := projectClip([4]float32{p[0], p[1], p[2], 1}, size[0], size[1])
			px, py, _, pok := transformToScreen(identityMat4(), p, size[0], size[1])
			if !ok || !pok || x != wantX || y != wantY || px != wantX || py != wantY {
				t.Errorf("size=%v ndc=%v triangle=(%g,%g) particle=(%g,%g) want=(%g,%g)", size, p, x, y, px, py, wantX, wantY)
			}
		}
	}
}

func TestViewportExactFrustumQuadCoversEveryPixel(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {2, 2}, {8, 6}, {9, 7}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			d, surface := New(size[0], size[1])
			r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Destroy()
			h := 5 * math.Tan(.5)
			w := h * float64(size[0]) / float64(size[1])
			positions := []float64{-w, -h, 0, w, -h, 0, -w, h, 0, -w, h, 0, w, -h, 0, w, h, 0}
			colors := make([]float64, 18)
			for i := 0; i < 18; i += 3 {
				colors[i] = 1
			}
			frame := engine.RenderBundle{Background: "#000000", Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: .1, Far: 100}, Environment: engine.RenderEnvironment{ToneMapping: "none"}, Passes: []engine.RenderPassBundle{{Positions: positions, Colors: colors, VertexCount: 6}}}
			if err := r.Frame(frame, size[0], size[1], 0); err != nil {
				t.Fatal(err)
			}
			missing := 0
			for y := 0; y < size[1]; y++ {
				for x := 0; x < size[0]; x++ {
					if d.Framebuffer().RGBAAt(x, y).R != 255 {
						missing++
					}
				}
			}
			if missing != 0 {
				t.Errorf("exact-edge quad lost %d/%d pixels", missing, size[0]*size[1])
			}
		})
	}
}

func viewportParticle(t *testing.T, w, h int, ndcX, ndcY float32, occludeCenter bool) *Device {
	t.Helper()
	d, _ := New(w, h)
	scene := &Buffer{data: make([]byte, 128)}
	particles := &Buffer{data: make([]byte, 32)}
	for i, v := range identityMat4() {
		writeFloat32(scene.data, i*4, v)
	}
	for _, off := range []int{80, 84, 88, 92, 96, 100, 104, 108} {
		writeFloat32(scene.data, off, 1)
	}
	writeFloat32(scene.data, 112, 1)
	writeFloat32(scene.data, 116, 1)
	writeFloat32(particles.data, 0, ndcX)
	writeFloat32(particles.data, 4, ndcY)
	writeFloat32(particles.data, 12, .5)
	writeFloat32(particles.data, 28, 1)
	pipeline := &RenderPipeline{desc: gpu.RenderPipelineDesc{Label: "bundle.particles.render"}}
	pass := &RenderPassEncoder{device: d, pipeline: pipeline, desc: gpu.RenderPassDesc{ColorAttachments: []gpu.RenderPassColorAttachment{{View: &SurfaceView{device: d}, StoreOp: gpu.StoreOpStore}}}}
	pass.bindGroups = map[int]*BindGroup{0: {desc: gpu.BindGroupDesc{Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: scene}, {Binding: 1, Buffer: particles}}}}}
	if occludeCenter {
		depth, err := d.CreateTexture(gpu.TextureDesc{Width: w, Height: h, Format: gpu.FormatDepth32Float})
		if err != nil {
			t.Fatal(err)
		}
		tex := depth.(*Texture)
		clearDepthTexture(tex, 1)
		writeDepth(tex, 0, w/2, h/2, 0)
		pass.desc.DepthStencilAttachment = &gpu.RenderPassDepthStencilAttachment{View: depth.CreateView()}
		pass.pipeline.desc.DepthStencil = &gpu.DepthStencilState{DepthCompare: gpu.CompareLess, Format: gpu.FormatDepth32Float}
	}
	pass.Draw(6, 1, 0, 0)
	return d
}

func TestViewportParticleUsesContinuousSampleCenters(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {2, 2}, {8, 6}, {9, 7}} {
		d := viewportParticle(t, size[0], size[1], 0, 0, false)
		radius := math.Max(1, math.Ceil(float64(min(size[0], size[1]))*.06))
		for y := 0; y < size[1]; y++ {
			for x := 0; x < size[0]; x++ {
				dx, dy := float64(x)+.5-float64(size[0])/2, float64(y)+.5-float64(size[1])/2
				want := dx*dx+dy*dy <= radius*radius
				if got := d.Framebuffer().RGBAAt(x, y).R > 0; got != want {
					t.Errorf("size=%v sample=(%d,%d) filled=%v want=%v", size, x, y, got, want)
				}
			}
		}
	}
}

func TestViewportParticleVisibleEdgeAndOccludedCenter(t *testing.T) {
	d := viewportParticle(t, 8, 8, 0, 0, true)
	if d.Framebuffer().RGBAAt(4, 4).R != 0 {
		t.Error("occluded center wrote color")
	}
	if d.Framebuffer().RGBAAt(3, 3).R == 0 {
		t.Error("center depth probe discarded visible particle neighbor")
	}
	for _, p := range [][2]float32{{-1, 0}, {1, 0}, {0, 1}, {0, -1}} {
		d := viewportParticle(t, 8, 8, p[0], p[1], false)
		lit := 0
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				if d.Framebuffer().RGBAAt(x, y).R != 0 {
					lit++
				}
			}
		}
		if lit == 0 {
			t.Errorf("edge %v disappeared", p)
		}
	}
}
