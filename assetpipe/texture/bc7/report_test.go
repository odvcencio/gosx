package bc7

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// This file prints the measurement tables the package documentation quotes. Run
// it with "go test -run TestReport -v ./assetpipe/texture/bc7/".
//
// The tests here assert almost nothing. The assertions live in encode_test.go,
// so a number that moves shows up there as a failure and here as a new value.

// psnrText formats a peak signal-to-noise ratio, including the lossless case.
func psnrText(v float64) string {
	if math.IsInf(v, 1) {
		return "lossless"
	}
	return fmt.Sprintf("%.2f", v)
}

// TestReportQuality prints the peak signal-to-noise ratio per image per quality
// level, next to a plain bounding-box encoder.
//
// The bounding-box column is the reference the endpoint work has to beat: it
// takes the per-channel minimum and maximum as endpoints, refines nothing, and
// evaluates one partition. So the gap between that column and the others is what
// the principal axis, the least squares refinement and the partition search
// bought together.
func TestReportQuality(t *testing.T) {
	images := buildTestImages()
	levels := []Quality{QualityFast, QualityBalanced, QualityBest}

	t.Log("PSNR in dB, measured on the stored 8-bit codes after the transfer function")
	t.Log("image              boundingBox      fast  balanced      best")
	for _, img := range images {
		bbox := baseConfig(t, img.space, QualityBalanced)
		bbox.seed = seedBoundingBox
		bbox.rounds = 0
		bbox.partitions = 1
		psnr, _ := measure(img.src, img.space, bbox)
		line := fmt.Sprintf("%-18s%12s", img.name, psnrText(psnr))
		for _, q := range levels {
			p, _ := measure(img.src, img.space, baseConfig(t, img.space, q))
			line += fmt.Sprintf("%10s", psnrText(p))
		}
		t.Log(line)
	}

	t.Log("")
	t.Log("blocks per mode at QualityBest")
	t.Log("image              mode0 mode1 mode2 mode3 mode4 mode5 mode6 mode7")
	for _, img := range images {
		_, stats := measure(img.src, img.space, baseConfig(t, img.space, QualityBest))
		line := fmt.Sprintf("%-18s", img.name)
		for _, c := range stats.ModeCounts {
			line += fmt.Sprintf("%6d", c)
		}
		t.Log(line)
	}
}

// TestReportEncoderSteps prints what each quality step buys.
//
// Every row degrades exactly one step away from QualityBest and reports the loss.
// A row that shows no loss is a step that is not paying, which is worth knowing.
func TestReportEncoderSteps(t *testing.T) {
	images := buildTestImages()

	type variant struct {
		name  string
		apply func(*config)
	}
	variants := []variant{
		{"best (reference)", func(c *config) {}},
		{"bounding box seed", func(c *config) { c.seed = seedBoundingBox }},
		{"no refinement", func(c *config) { c.rounds = 0 }},
		{"one refinement", func(c *config) { c.rounds = 1 }},
		{"1 partition", func(c *config) { c.partitions = 1 }},
		{"2 partitions", func(c *config) { c.partitions = 2 }},
		{"4 partitions", func(c *config) { c.partitions = 4 }},
		{"8 partitions", func(c *config) { c.partitions = 8 }},
		{"64 partitions", func(c *config) { c.partitions = 64 }},
		{"no rotation", func(c *config) { c.rotations = 1 }},
		{"cheap parity", func(c *config) { c.exhaustiveParity = false }},
		{"projected indices", func(c *config) { c.exactIndices = false }},
	}

	t.Log("PSNR in dB, one step degraded from QualityBest")
	header := fmt.Sprintf("%-20s", "variant")
	for _, img := range images {
		header += fmt.Sprintf("%12s", img.name)
	}
	t.Log(header)
	for _, v := range variants {
		line := fmt.Sprintf("%-20s", v.name)
		for _, img := range images {
			cfg := baseConfig(t, img.space, QualityBest)
			v.apply(&cfg)
			p, _ := measure(img.src, img.space, cfg)
			line += fmt.Sprintf("%12s", psnrText(p))
		}
		t.Log(line)
	}
}

// TestReportModeValue prints what dropping one mode costs.
//
// The column is the loss against the whole mode set. A mode with no loss anywhere
// is a mode the encoder could skip for free on this image set.
func TestReportModeValue(t *testing.T) {
	images := buildTestImages()

	t.Log("PSNR in dB at QualityBest with one mode removed")
	header := fmt.Sprintf("%-20s", "modes")
	for _, img := range images {
		header += fmt.Sprintf("%12s", img.name)
	}
	t.Log(header)

	rows := []struct {
		name  string
		modes ModeMask
	}{
		{"all", ModesAll},
		{"mode 6 alone", Mode6},
		{"modes 1+6", Mode1 | Mode6},
		{"modes 1+5+6", Mode1 | Mode5 | Mode6},
		{"modes 1+4+5+6", Mode1 | Mode4 | Mode5 | Mode6},
		{"all but 0", ModesAll &^ Mode0},
		{"all but 2", ModesAll &^ Mode2},
		{"all but 0 and 2", ModesAll &^ (Mode0 | Mode2)},
		{"all but 3", ModesAll &^ Mode3},
		{"all but 7", ModesAll &^ Mode7},
	}
	for _, row := range rows {
		line := fmt.Sprintf("%-20s", row.name)
		for _, img := range images {
			cfg := baseConfig(t, img.space, QualityBest)
			cfg.modes = row.modes
			p, _ := measure(img.src, img.space, cfg)
			line += fmt.Sprintf("%12s", psnrText(p))
		}
		t.Log(line)
	}
}

// TestReportGPUBytes prints the headline size number: a full mip chain as BC7
// against the same chain as rgba8unorm.
func TestReportGPUBytes(t *testing.T) {
	t.Log("mip chain GPU bytes, BC7 against rgba8unorm")
	t.Log("  base   rgba8unorm         bc7   ratio")
	for _, size := range []int{256, 512, 1024, 2048, 4096} {
		raw, bc := 0, 0
		for w, h := size, size; w >= 1 && h >= 1; w, h = w/2, h/2 {
			raw += w * h * 4
			bc += EncodedSize(w, h)
			if w == 1 && h == 1 {
				break
			}
		}
		t.Log(fmt.Sprintf("%6d %12d %11d %7.3f", size, raw, bc, float64(raw)/float64(bc)))
	}
}

// TestReportThroughput prints the throughput table with a wall clock.
func TestReportThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("throughput needs several full encode passes")
	}
	src := photoLike(512, 512)
	pixels := float64(src.Width * src.Height)
	t.Log("quality      workers   seconds   Mpixel/s")
	for _, q := range []Quality{QualityFast, QualityBalanced, QualityBest} {
		for _, workers := range []int{1, 0} {
			opts := Options{Space: SRGB, Quality: q, Parallel: workers}
			// One warm-up pass removes first-touch page faults from the timing.
			if _, err := Encode(src, opts); err != nil {
				t.Fatal(err)
			}
			passes := 3
			if q == QualityBest && workers == 1 {
				passes = 1
			}
			start := time.Now()
			for i := 0; i < passes; i++ {
				if _, err := Encode(src, opts); err != nil {
					t.Fatal(err)
				}
			}
			elapsed := time.Since(start).Seconds()
			label := "parallel"
			if workers == 1 {
				label = "single"
			}
			t.Log(fmt.Sprintf("%-12s %-8s %8.3f %10.2f",
				q, label, elapsed/float64(passes), pixels*float64(passes)/1e6/elapsed))
		}
	}
}
