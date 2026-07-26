package ibl

import (
	"math"
	"testing"
)

func TestSolidAnglesSumToSphere(t *testing.T) {
	const size = 16
	total := 0.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			total += texelSolidAngle(x, y, size)
		}
	}
	total *= 6
	if math.Abs(total-4*math.Pi) > 1e-9 {
		t.Fatalf("cube solid angle = %.12f, want %.12f", total, 4*math.Pi)
	}
}

func TestIrradianceOfConstantEnvironmentIsConstant(t *testing.T) {
	// A Lambert surface under uniform radiance L reflects exactly L when its
	// albedo is 1, because the irradiance is pi*L and the map stores E/pi.
	source := filledCube(16, Vec3{2, 3, 4})
	sh := ProjectSH(source)
	cube := IrradianceCube(sh, 8)
	for face := 0; face < 6; face++ {
		for y := 0; y < cube.Size; y++ {
			for x := 0; x < cube.Size; x++ {
				got := cube.Get(face, x, y)
				if math.Abs(got.X-2) > 1e-4 || math.Abs(got.Y-3) > 1e-4 || math.Abs(got.Z-4) > 1e-4 {
					t.Fatalf("face %d texel (%d,%d) = %+v, want (2,3,4)", face, x, y, got)
				}
			}
		}
	}
}

func TestIrradianceMatchesBruteForceOnBandLimitedEnvironment(t *testing.T) {
	// L(d) = 1 + 0.5*d.Y lives entirely in bands 0 and 1, so the harmonic
	// path must agree with a direct cosine convolution.
	const size = 32
	source := NewCube(size)
	for face := 0; face < 6; face++ {
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dir := FaceDirection(face, x, y, size)
				value := 1 + 0.5*dir.Y
				source.Set(face, x, y, Vec3{value, value, value})
			}
		}
	}
	sh := ProjectSH(source)
	fromSH := IrradianceCube(sh, 8)
	reference := IrradianceCubeBruteForce(source, 8)
	worst := 0.0
	for face := 0; face < 6; face++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				got := fromSH.Get(face, x, y).X
				want := reference.Get(face, x, y).X
				if err := math.Abs(got-want) / math.Abs(want); err > worst {
					worst = err
				}
			}
		}
	}
	if worst > 0.01 {
		t.Fatalf("worst relative error %.4f, want at most 0.01", worst)
	}
}

func TestProjectSHIsResolutionIndependent(t *testing.T) {
	build := func(size int) SH9 {
		cube := NewCube(size)
		for face := 0; face < 6; face++ {
			for y := 0; y < size; y++ {
				for x := 0; x < size; x++ {
					dir := FaceDirection(face, x, y, size)
					cube.Set(face, x, y, Vec3{1 + dir.X, 1, 1})
				}
			}
		}
		return ProjectSH(cube)
	}
	small := build(16)
	large := build(32)
	for i := 0; i < 9; i++ {
		if math.Abs(small[i].X-large[i].X) > 0.02 {
			t.Fatalf("coefficient %d: %.5f at size 16, %.5f at size 32", i, small[i].X, large[i].X)
		}
	}
}
