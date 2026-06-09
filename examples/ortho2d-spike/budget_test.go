// Package ortho2dspike is a THROWAWAY M1-slice-1 budget spike (gosx-studio
// WebGPU canvas re-platform). It measures the GPU frame cost of the board's
// geometry — a bundle2d RenderBundle of N rects (unlit instanced quads, the
// exact bundle fields 16a's drawInstancedMeshes reads) — rendered through the
// native render/bundle WebGPU renderer on the headless device.
//
// Rationale (see plan.gosx-studio.m1-16a-ortho2d-board.v0.1 + the M0 verdict):
// the native and 16a renderers consume the SAME RenderBundle on the SAME GPU;
// they differ in features (PBR/skin/pick), not in instanced-quad throughput. So
// this native-headless number is a tight budget proxy for "the board through
// 16a", obtainable in pure Go with no browser/WASM/Playwright plumbing. It also
// proves bundle2d → render/bundle compatibility (relevant to M3's compositor).
//
// DELETE after M1 slice 1 (throwaway; ships no product code).
package ortho2dspike

import (
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/bundle2d"
	"m31labs.dev/gosx/render/gpu/headless"
)

func genRects(n int) []gosx.CanvasBoardNode {
	palette := []string{"#ff8866", "#88ddff", "#ffd866", "#a0ff88", "#ff88dd"}
	cols := 1
	for cols*cols < n {
		cols++
	}
	nodes := make([]gosx.CanvasBoardNode, 0, n)
	for i := 0; i < n; i++ {
		row := i / cols
		col := i % cols
		nodes = append(nodes, gosx.CanvasBoardNode{
			ID:     fmt.Sprintf("rect-%d", i),
			Kind:   "rect",
			X:      float64(col-cols/2) * 60,
			Y:      float64(row-cols/2) * 50,
			Width:  44,
			Height: 32,
			Color:  palette[i%len(palette)],
		})
	}
	return nodes
}

func TestOrtho2DBoardBudget(t *testing.T) {
	if os.Getenv("GOSX_ORTHO2D_BUDGET") == "" {
		t.Skip("throwaway M1 spike; set GOSX_ORTHO2D_BUDGET=1 to run (hits the GPU, ~10s)")
	}
	const (
		w        = 1280
		h        = 720
		warmup   = 15
		measured = 120
	)
	ms := func(dd time.Duration) float64 { return float64(dd.Microseconds()) / 1000.0 }
	p := func(s []time.Duration, q int) time.Duration { return s[len(s)*q/100] }
	var frameOnlyP50 [5]float64
	idxByN := map[int]int{0: 0, 100: 1, 1000: 2, 10000: 3}

	for _, n := range []int{0, 100, 1000, 10000} {
		nodes := genRects(n)
		rb := bundle2d.ComputeCanvasBundle(nodes, w, h, 1, 0, 0)

		d, surface := headless.New(w, h)
		r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
		if err != nil {
			t.Fatalf("N=%d bundle.New: %v", n, err)
		}

		for i := 0; i < warmup; i++ {
			if err := r.Frame(rb, w, h, float64(i)); err != nil {
				t.Fatalf("N=%d warmup Frame: %v", n, err)
			}
		}

		frameOnly := make([]time.Duration, 0, measured) // r.Frame submission+render (no readback)
		full := make([]time.Duration, 0, measured)      // r.Frame + full framebuffer readback (sync)
		for i := 0; i < measured; i++ {
			s0 := time.Now()
			if err := r.Frame(rb, w, h, float64(warmup+i)); err != nil {
				t.Fatalf("N=%d Frame: %v", n, err)
			}
			s1 := time.Now()
			_ = d.Framebuffer()
			s2 := time.Now()
			frameOnly = append(frameOnly, s1.Sub(s0))
			full = append(full, s2.Sub(s0))
		}
		r.Destroy()

		sort.Slice(frameOnly, func(i, j int) bool { return frameOnly[i] < frameOnly[j] })
		sort.Slice(full, func(i, j int) bool { return full[i] < full[j] })
		frameOnlyP50[idxByN[n]] = ms(p(frameOnly, 50))
		t.Logf("N=%-5d  frame-only p50=%.3fms p95=%.3fms   frame+readback p50=%.3fms",
			n, ms(p(frameOnly, 50)), ms(p(frameOnly, 95)), ms(p(full, 50)))
	}

	// Geometry cost isolated from fixed per-frame overhead (readback/sync floor).
	t.Logf("ISOLATED geometry render cost (frame-only delta vs N=0 baseline):")
	t.Logf("  100 rects:   %+.3fms", frameOnlyP50[1]-frameOnlyP50[0])
	t.Logf("  1000 rects:  %+.3fms", frameOnlyP50[2]-frameOnlyP50[0])
	t.Logf("  10000 rects: %+.3fms  <- board geometry budget on this GPU", frameOnlyP50[3]-frameOnlyP50[0])
}
