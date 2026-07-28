package ibl

import (
	"math"
	"testing"
)

type constantSource struct {
	width, height int
	r, g, b       float32
}

func (s constantSource) Size() (int, int)                         { return s.width, s.height }
func (s constantSource) RGB(int, int) (float32, float32, float32) { return s.r, s.g, s.b }

// gradientSource varies only with latitude, so it stays smooth and easy to
// reason about after projection.
type gradientSource struct{ width, height int }

func (s gradientSource) Size() (int, int) { return s.width, s.height }

func (s gradientSource) RGB(x, y int) (float32, float32, float32) {
	t := (float64(y) + 0.5) / float64(s.height)
	value := float32(1 + 0.5*math.Cos(t*math.Pi))
	return value, value * 0.5, value * 0.25
}

func TestDirectionToFaceInvertsFaceDirection(t *testing.T) {
	const size = 8
	for face := 0; face < 6; face++ {
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dir := FaceDirection(face, x, y, size)
				gotFace, s, tCoord := DirectionToFace(dir)
				if gotFace != face {
					t.Fatalf("face %d texel (%d,%d) mapped to face %d", face, x, y, gotFace)
				}
				gotX := int(math.Floor(s * size))
				gotY := int(math.Floor(tCoord * size))
				if gotX != x || gotY != y {
					t.Fatalf("face %d texel (%d,%d) mapped to (%d,%d)", face, x, y, gotX, gotY)
				}
			}
		}
	}
}

func TestSampleReturnsTexelCentreExactly(t *testing.T) {
	const size = 4
	cube := NewCube(size)
	for face := 0; face < 6; face++ {
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				cube.Set(face, x, y, Vec3{float64(face), float64(x), float64(y)})
			}
		}
	}
	for face := 0; face < 6; face++ {
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				got := cube.Sample(FaceDirection(face, x, y, size))
				want := Vec3{float64(face), float64(x), float64(y)}
				if math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 || math.Abs(got.Z-want.Z) > 1e-6 {
					t.Fatalf("face %d texel (%d,%d) sampled %+v, want %+v", face, x, y, got, want)
				}
			}
		}
	}
}

func TestEquirectToCubeKeepsConstantEnergy(t *testing.T) {
	src := constantSource{width: 64, height: 32, r: 3, g: 2, b: 1}
	cube, err := EquirectToCube(src, 16, 2)
	if err != nil {
		t.Fatal(err)
	}
	for face := 0; face < 6; face++ {
		for y := 0; y < cube.Size; y++ {
			for x := 0; x < cube.Size; x++ {
				got := cube.Get(face, x, y)
				if math.Abs(got.X-3) > 1e-5 || math.Abs(got.Y-2) > 1e-5 || math.Abs(got.Z-1) > 1e-5 {
					t.Fatalf("face %d texel (%d,%d) = %+v, want (3,2,1)", face, x, y, got)
				}
			}
		}
	}
}

func TestEquirectToCubePlacesPoles(t *testing.T) {
	// The top row of the source maps to y = +1, so the +Y face must pick it up.
	src := gradientSource{width: 64, height: 32}
	cube, err := EquirectToCube(src, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	top := cube.Get(FacePosY, 4, 4)
	bottom := cube.Get(FaceNegY, 4, 4)
	if top.X <= bottom.X {
		t.Fatalf("+Y face %.4f should stay brighter than -Y face %.4f", top.X, bottom.X)
	}
	if math.Abs(top.X-1.5) > 0.1 {
		t.Fatalf("+Y face = %.4f, want about 1.5", top.X)
	}
	if math.Abs(bottom.X-0.5) > 0.1 {
		t.Fatalf("-Y face = %.4f, want about 0.5", bottom.X)
	}
}

func TestBuildChainHalvesEachLevel(t *testing.T) {
	cube := NewCube(8)
	for face := 0; face < 6; face++ {
		for i := range cube.Faces[face] {
			cube.Faces[face][i] = 2
		}
	}
	chain := BuildChain(cube)
	if len(chain) != 4 {
		t.Fatalf("chain has %d levels, want 4", len(chain))
	}
	for level, mip := range chain {
		if mip.Size != 8>>level {
			t.Fatalf("level %d size %d, want %d", level, mip.Size, 8>>level)
		}
		if got := mip.Get(0, 0, 0); math.Abs(got.X-2) > 1e-6 {
			t.Fatalf("level %d lost energy: %+v", level, got)
		}
	}
}
