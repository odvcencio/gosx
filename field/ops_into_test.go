package field

import "testing"

// testField builds a deterministic field for the equivalence tests.
func testField(res int, components int) *Field {
	f := New([3]int{res, res, res}, components, AABB{
		Min: [3]float32{-1, -1, -1},
		Max: [3]float32{2, 3, 4},
	})
	for i := range f.Data {
		f.Data[i] = float32((i*7)%211)*0.013 - 1.2
	}
	return f
}

func sameData(t *testing.T, name string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: length %d, want %d", name, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: value %d = %v, want %v", name, i, got[i], want[i])
		}
	}
}

// TestIntoVariantsMatchAllocatingForms proves that the output-parameter forms
// produce the same numbers as the allocating forms, bit for bit.
func TestIntoVariantsMatchAllocatingForms(t *testing.T) {
	scalar := testField(12, 1)
	vec3 := testField(12, 3)

	t.Run("Gradient", func(t *testing.T) {
		want := Gradient(scalar)
		got := New(scalar.Resolution, 3, scalar.Bounds)
		if err := GradientInto(got, scalar); err != nil {
			t.Fatal(err)
		}
		sameData(t, "GradientInto", got.Data, want.Data)
	})

	t.Run("Divergence", func(t *testing.T) {
		want := Divergence(vec3)
		got := New(vec3.Resolution, 1, vec3.Bounds)
		if err := DivergenceInto(got, vec3); err != nil {
			t.Fatal(err)
		}
		sameData(t, "DivergenceInto", got.Data, want.Data)
	})

	t.Run("Curl", func(t *testing.T) {
		want := Curl(vec3)
		got := New(vec3.Resolution, 3, vec3.Bounds)
		if err := CurlInto(got, vec3); err != nil {
			t.Fatal(err)
		}
		sameData(t, "CurlInto", got.Data, want.Data)
	})

	t.Run("Resample", func(t *testing.T) {
		want := Resample(vec3, [3]int{7, 9, 5})
		got := New([3]int{7, 9, 5}, 3, AABB{})
		if err := ResampleInto(got, vec3); err != nil {
			t.Fatal(err)
		}
		sameData(t, "ResampleInto", got.Data, want.Data)
		if got.Bounds != vec3.Bounds {
			t.Fatalf("ResampleInto bounds = %v, want %v", got.Bounds, vec3.Bounds)
		}
	})

	t.Run("Blur", func(t *testing.T) {
		for _, radius := range []float32{0, 0.7, 1.5} {
			want := Blur(vec3, radius)
			got := New(vec3.Resolution, 3, vec3.Bounds)
			scratch := NewScratch(vec3.Resolution, 3)
			if err := BlurInto(got, vec3, radius, scratch); err != nil {
				t.Fatal(err)
			}
			sameData(t, "BlurInto", got.Data, want.Data)
		}
	})
}

// TestBlurIntoInPlaceMatchesCopy proves that an aliased destination gives the
// same result as a separate destination.
func TestBlurIntoInPlaceMatchesCopy(t *testing.T) {
	src := testField(10, 3)
	want := Blur(src, 1.2)

	inPlace := New(src.Resolution, src.Components, src.Bounds)
	copy(inPlace.Data, src.Data)
	scratch := NewScratch(src.Resolution, src.Components)
	if err := BlurInto(inPlace, inPlace, 1.2, scratch); err != nil {
		t.Fatal(err)
	}
	sameData(t, "BlurInto in place", inPlace.Data, want.Data)
}

// TestBlurIntoNilScratchWorks proves that a nil Scratch is legal.
func TestBlurIntoNilScratchWorks(t *testing.T) {
	src := testField(6, 2)
	want := Blur(src, 1)
	got := New(src.Resolution, src.Components, src.Bounds)
	if err := BlurInto(got, src, 1, nil); err != nil {
		t.Fatal(err)
	}
	sameData(t, "BlurInto nil scratch", got.Data, want.Data)
}

// TestIntoVariantsRejectBadOutput proves that the output-parameter forms report
// a shape mismatch, an alias, or a nil buffer instead of panicking.
func TestIntoVariantsRejectBadOutput(t *testing.T) {
	scalar := testField(6, 1)
	vec3 := testField(6, 3)
	wrong := New([3]int{5, 5, 5}, 3, vec3.Bounds)

	if err := GradientInto(nil, scalar); err == nil {
		t.Fatal("GradientInto accepted a nil output")
	}
	if err := GradientInto(wrong, scalar); err == nil {
		t.Fatal("GradientInto accepted a mismatched output resolution")
	}
	if err := CurlInto(vec3, vec3); err == nil {
		t.Fatal("CurlInto accepted an aliased output")
	}
	if err := DivergenceInto(vec3, vec3); err == nil {
		t.Fatal("DivergenceInto accepted a vec3 output")
	}
	if err := ResampleInto(vec3, vec3); err == nil {
		t.Fatal("ResampleInto accepted an aliased output")
	}
	if err := BlurInto(New(vec3.Resolution, 1, vec3.Bounds), vec3, 1, nil); err == nil {
		t.Fatal("BlurInto accepted a mismatched component count")
	}
}

// TestScratchReachesSteadyState proves that a loop with a reused Scratch stops
// growing its buffer after the first call.
func TestScratchReachesSteadyState(t *testing.T) {
	src := testField(8, 3)
	dst := New(src.Resolution, src.Components, src.Bounds)
	scratch := NewScratch(src.Resolution, src.Components)
	first := scratch.Cap()
	for i := 0; i < 8; i++ {
		if err := BlurInto(dst, src, 1, scratch); err != nil {
			t.Fatal(err)
		}
	}
	if scratch.Cap() != first {
		t.Fatalf("scratch capacity grew from %d to %d", first, scratch.Cap())
	}
}

// TestIntoVariantsAllocateNothing locks the allocation-free contract.
func TestIntoVariantsAllocateNothing(t *testing.T) {
	scalar := testField(8, 1)
	vec3 := testField(8, 3)
	gradOut := New(vec3.Resolution, 3, vec3.Bounds)
	divOut := New(vec3.Resolution, 1, vec3.Bounds)
	curlOut := New(vec3.Resolution, 3, vec3.Bounds)
	blurOut := New(vec3.Resolution, 3, vec3.Bounds)
	small := New([3]int{4, 4, 4}, 3, vec3.Bounds)
	scratch := NewScratch(vec3.Resolution, 3)
	particles := make([]float32, 30)

	// Warm the scratch buffer so the measured runs reuse it.
	if err := BlurInto(blurOut, vec3, 1, scratch); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		run  func() error
	}{
		{"GradientInto", func() error { return GradientInto(gradOut, scalar) }},
		{"DivergenceInto", func() error { return DivergenceInto(divOut, vec3) }},
		{"CurlInto", func() error { return CurlInto(curlOut, vec3) }},
		{"ResampleInto", func() error { return ResampleInto(small, vec3) }},
		{"Advect", func() error { return Advect(vec3, particles, 0.01) }},
	}
	for _, tc := range cases {
		var failed error
		allocs := testing.AllocsPerRun(20, func() {
			if err := tc.run(); err != nil {
				failed = err
			}
		})
		if failed != nil {
			t.Fatalf("%s: %v", tc.name, failed)
		}
		if allocs != 0 {
			t.Errorf("%s allocated %.1f times per run, want 0", tc.name, allocs)
		}
	}

	// BlurInto caches the Gaussian kernel in the Scratch, so a fixed radius
	// allocates nothing after the warm-up call above.
	blurAllocs := testing.AllocsPerRun(20, func() {
		if err := BlurInto(blurOut, vec3, 1, scratch); err != nil {
			t.Fatal(err)
		}
	})
	if blurAllocs != 0 {
		t.Errorf("BlurInto allocated %.1f times per run, want 0", blurAllocs)
	}
}
