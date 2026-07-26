package bc7

import "testing"

// benchImage is the throughput subject. 512 by 512 is large enough that the
// per-call overhead disappears and small enough to build quickly.
var benchImage = photoLike(512, 512)

func benchEncode(b *testing.B, q Quality, parallel int) {
	b.Helper()
	opts := Options{Space: SRGB, Quality: q, Parallel: parallel}
	pixels := float64(benchImage.Width * benchImage.Height)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Encode(benchImage, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	seconds := b.Elapsed().Seconds()
	if seconds > 0 {
		mpixels := pixels * float64(b.N) / 1e6 / seconds
		b.ReportMetric(mpixels, "Mpixel/s")
	}
}

func BenchmarkEncodeFastSingle(b *testing.B)     { benchEncode(b, QualityFast, 1) }
func BenchmarkEncodeBalancedSingle(b *testing.B) { benchEncode(b, QualityBalanced, 1) }
func BenchmarkEncodeBestSingle(b *testing.B)     { benchEncode(b, QualityBest, 1) }
func BenchmarkEncodeFastParallel(b *testing.B)   { benchEncode(b, QualityFast, 0) }
func BenchmarkEncodeBalancedPar(b *testing.B)    { benchEncode(b, QualityBalanced, 0) }
func BenchmarkEncodeBestParallel(b *testing.B)   { benchEncode(b, QualityBest, 0) }

func BenchmarkDecode(b *testing.B) {
	data, err := Encode(benchImage, Options{Space: SRGB, Quality: QualityFast})
	if err != nil {
		b.Fatal(err)
	}
	pixels := float64(benchImage.Width * benchImage.Height)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decode(data, benchImage.Width, benchImage.Height, SRGB); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if s := b.Elapsed().Seconds(); s > 0 {
		b.ReportMetric(pixels*float64(b.N)/1e6/s, "Mpixel/s")
	}
}
