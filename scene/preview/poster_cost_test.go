package preview_test

import (
	"fmt"
	"testing"
	"time"

	"m31labs.dev/gosx/scene/preview"
)

// This file measures what a poster costs to make and to ship. Benchmarks do not
// run under a plain `go test`, so the suite stays fast and the numbers stay
// available on demand:
//
//	go test ./scene/preview/ -run XXX -bench PosterCost -benchtime 1x
//
// Every number these benchmarks print describes the machine that ran them. Do
// not publish one as a property of GoSX, and do not read a render time here as
// a browser frame time.

var posterCostSizes = []struct {
	name          string
	width, height int
}{
	{"320x180", 320, 180},
	{"640x360", 640, 360},
	{"1280x720", 1280, 720},
}

// BenchmarkPosterCost reports the encoded byte size and the split between CPU
// rasterization and PNG encoding at each poster size.
func BenchmarkPosterCost(b *testing.B) {
	for _, size := range posterCostSizes {
		b.Run(size.name, func(b *testing.B) {
			opts := preview.NewPosterOptions(posterRenderOptions(size.width, size.height))
			var render, encode time.Duration
			var bytesOut int
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				poster, err := preview.PosterFromJSON([]byte(posterScene), opts)
				if err != nil {
					b.Fatal(err)
				}
				if !poster.Fidelity.OK {
					b.Fatalf("poster failed its gate: %v", poster.Fidelity.Failures)
				}
				render += poster.RenderDuration
				encode += poster.EncodeDuration
				bytesOut = poster.ByteSize()
			}
			b.StopTimer()
			runs := float64(b.N)
			b.ReportMetric(float64(bytesOut), "bytes")
			b.ReportMetric(float64(bytesOut)/1024, "KiB")
			b.ReportMetric(float64(render.Microseconds())/runs/1000, "render-ms")
			b.ReportMetric(float64(encode.Microseconds())/runs/1000, "encode-ms")
			b.Log(fmt.Sprintf("%s: %d bytes (%.1f KiB), render %.2f ms, encode %.2f ms",
				size.name, bytesOut, float64(bytesOut)/1024,
				float64(render.Microseconds())/runs/1000,
				float64(encode.Microseconds())/runs/1000))
		})
	}
}

// BenchmarkPosterCostPage models the build-time cost of a realistic page count.
// It renders one poster per page at 640x360, which is the size a hero surface
// needs before the browser upscales it.
func BenchmarkPosterCostPage(b *testing.B) {
	const pages = 50
	opts := preview.NewPosterOptions(posterRenderOptions(640, 360))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		total := 0
		for page := 0; page < pages; page++ {
			poster, err := preview.PosterFromJSON([]byte(posterScene), opts)
			if err != nil {
				b.Fatal(err)
			}
			total += poster.ByteSize()
		}
		b.ReportMetric(float64(total)/1024, "total-KiB")
	}
	b.ReportMetric(pages, "pages")
}
