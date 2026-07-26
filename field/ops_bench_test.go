package field

import (
	"fmt"
	"testing"
)

// benchField builds a deterministic vec3 field for the operator benchmarks.
func benchField(res int, components int) *Field {
	f := New([3]int{res, res, res}, components, AABB{
		Min: [3]float32{-1, -1, -1},
		Max: [3]float32{1, 1, 1},
	})
	for i := range f.Data {
		f.Data[i] = float32(i%97) * 0.01
	}
	return f
}

func BenchmarkBlur(b *testing.B) {
	for _, res := range []int{32, 64} {
		f := benchField(res, 3)
		b.Run(fmt.Sprintf("res%d/vec3", res), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = Blur(f, 1)
			}
		})
	}
}

func BenchmarkBlurInto(b *testing.B) {
	for _, res := range []int{32, 64} {
		f := benchField(res, 3)
		dst := New(f.Resolution, f.Components, f.Bounds)
		scratch := NewScratch(f.Resolution, f.Components)
		b.Run(fmt.Sprintf("res%d/vec3", res), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = BlurInto(dst, f, 1, scratch)
			}
		})
	}
}

func BenchmarkGradient(b *testing.B) {
	f := benchField(48, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Gradient(f)
	}
}

func BenchmarkGradientInto(b *testing.B) {
	f := benchField(48, 1)
	dst := New(f.Resolution, 3, f.Bounds)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GradientInto(dst, f)
	}
}

func BenchmarkDivergence(b *testing.B) {
	f := benchField(48, 3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Divergence(f)
	}
}

func BenchmarkDivergenceInto(b *testing.B) {
	f := benchField(48, 3)
	dst := New(f.Resolution, 1, f.Bounds)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DivergenceInto(dst, f)
	}
}

func BenchmarkCurl(b *testing.B) {
	f := benchField(48, 3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Curl(f)
	}
}

func BenchmarkCurlInto(b *testing.B) {
	f := benchField(48, 3)
	dst := New(f.Resolution, 3, f.Bounds)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CurlInto(dst, f)
	}
}

func BenchmarkResample(b *testing.B) {
	f := benchField(48, 3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Resample(f, [3]int{32, 32, 32})
	}
}

func BenchmarkResampleInto(b *testing.B) {
	f := benchField(48, 3)
	dst := New([3]int{32, 32, 32}, f.Components, f.Bounds)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ResampleInto(dst, f)
	}
}

// BenchmarkFluidStep models one frame of a fluid loop: curl, blur, advect.
// The allocating form allocates the whole working set every frame.
func BenchmarkFluidStep(b *testing.B) {
	f := benchField(32, 3)
	particles := make([]float32, 3*1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := Curl(f)
		blurred := Blur(c, 1)
		_ = Advect(blurred, particles, 0.016)
	}
}

// BenchmarkFluidStepInto is BenchmarkFluidStep with caller-supplied buffers.
func BenchmarkFluidStepInto(b *testing.B) {
	f := benchField(32, 3)
	particles := make([]float32, 3*1024)
	curl := New(f.Resolution, 3, f.Bounds)
	blurred := New(f.Resolution, 3, f.Bounds)
	scratch := NewScratch(f.Resolution, 3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CurlInto(curl, f)
		_ = BlurInto(blurred, curl, 1, scratch)
		_ = Advect(blurred, particles, 0.016)
	}
}
