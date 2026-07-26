package bcn

import (
	"runtime"
	"testing"
)

// benchImage holds one image large enough to hide the setup cost.
func benchImage(size int) *Surface {
	return srgbSurface(size, size, func(x, y int) RGBA8 {
		return RGBA8{
			R: uint8((x * 3) ^ (y * 5)),
			G: uint8(x + y*2),
			B: uint8((x*x + y) % 251),
			A: uint8(x % 256),
		}
	})
}

// reportMegapixels turns the benchmark timing into megapixels per second, which
// is the unit an asset pipeline budgets in.
func reportMegapixels(b *testing.B, pixels int) {
	b.ReportMetric(float64(pixels)*float64(b.N)/b.Elapsed().Seconds()/1e6, "MP/s")
}

func BenchmarkEncodeBC1(b *testing.B) {
	const size = 512
	image := benchImage(size)
	for _, quality := range []Quality{QualityFast, QualityHigh} {
		for _, workers := range []int{1, -1} {
			name := quality.String() + "/single"
			if workers != 1 {
				name = quality.String() + "/parallel"
			}
			b.Run(name, func(b *testing.B) {
				opts := BC1Options{Transfer: TransferSRGB, Quality: quality, Workers: workers}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := EncodeBC1(image, opts); err != nil {
						b.Fatal(err)
					}
				}
				reportMegapixels(b, size*size)
			})
		}
	}
}

func BenchmarkEncodeBC3(b *testing.B) {
	const size = 512
	image := benchImage(size)
	for _, quality := range []Quality{QualityFast, QualityHigh} {
		for _, workers := range []int{1, -1} {
			name := quality.String() + "/single"
			if workers != 1 {
				name = quality.String() + "/parallel"
			}
			b.Run(name, func(b *testing.B) {
				opts := BC3Options{Transfer: TransferSRGB, Quality: quality, Workers: workers}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := EncodeBC3(image, opts); err != nil {
						b.Fatal(err)
					}
				}
				reportMegapixels(b, size*size)
			})
		}
	}
}

func BenchmarkEncodeBC4(b *testing.B) {
	const size = 512
	image := benchImage(size)
	for _, quality := range []Quality{QualityFast, QualityHigh} {
		for _, workers := range []int{1, -1} {
			name := quality.String() + "/single"
			if workers != 1 {
				name = quality.String() + "/parallel"
			}
			b.Run(name, func(b *testing.B) {
				opts := BC4Options{Transfer: TransferUnorm, Quality: quality, Workers: workers}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := EncodeBC4(image, opts); err != nil {
						b.Fatal(err)
					}
				}
				reportMegapixels(b, size*size)
			})
		}
	}
}

func BenchmarkEncodeBC5Normal(b *testing.B) {
	const size = 512
	image := normalImage(size)
	for _, quality := range []Quality{QualityFast, QualityHigh} {
		for _, workers := range []int{1, -1} {
			name := quality.String() + "/single"
			if workers != 1 {
				name = quality.String() + "/parallel"
			}
			b.Run(name, func(b *testing.B) {
				opts := BC5Options{Transfer: TransferUnorm, Quality: quality, Workers: workers}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := EncodeBC5Normal(image, opts); err != nil {
						b.Fatal(err)
					}
				}
				reportMegapixels(b, size*size)
			})
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	const size = 512
	image := benchImage(size)
	payload, err := EncodeBC1(image, BC1Options{Transfer: TransferSRGB})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decode(FormatBC1RGB, payload, size, size); err != nil {
			b.Fatal(err)
		}
	}
	reportMegapixels(b, size*size)
}

// TestBenchmarkEnvironment records the processor count next to the throughput
// numbers, because a parallel figure means nothing without it.
func TestBenchmarkEnvironment(t *testing.T) {
	t.Logf("runtime.NumCPU reports %d processors", runtime.NumCPU())
}
