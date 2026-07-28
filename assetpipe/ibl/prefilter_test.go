package ibl

import (
	"math"
	"testing"
)

func filledCube(size int, colour Vec3) *Cube {
	cube := NewCube(size)
	for face := 0; face < 6; face++ {
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				cube.Set(face, x, y, colour)
			}
		}
	}
	return cube
}

func patternCube(size int) *Cube {
	cube := NewCube(size)
	for face := 0; face < 6; face++ {
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dir := FaceDirection(face, x, y, size)
				cube.Set(face, x, y, Vec3{1 + dir.X, 1 + dir.Y, 1 + dir.Z})
			}
		}
	}
	return cube
}

func TestPrefilterLevelZeroEqualsSource(t *testing.T) {
	source := patternCube(16)
	chain := Prefilter(source, PrefilterOptions{Samples: 8, MipSelect: true})
	if len(chain) != 5 {
		t.Fatalf("chain has %d levels, want 5", len(chain))
	}
	for face := 0; face < 6; face++ {
		for i := range source.Faces[face] {
			if chain[0].Faces[face][i] != source.Faces[face][i] {
				t.Fatalf("face %d component %d changed at roughness 0", face, i)
			}
		}
	}
}

func TestPrefilterAtZeroRoughnessIsIdentity(t *testing.T) {
	// Drive the integrator itself, not the level 0 copy path.
	source := patternCube(8)
	chain := BuildChain(source)
	saTexel := 4 * math.Pi / (6 * 64)
	for face := 0; face < 6; face++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				normal := FaceDirection(face, x, y, 8)
				got := prefilterTexel(chain, normal, 0, 16, saTexel, true)
				want := source.Get(face, x, y)
				if math.Abs(got.X-want.X) > 1e-5 || math.Abs(got.Y-want.Y) > 1e-5 || math.Abs(got.Z-want.Z) > 1e-5 {
					t.Fatalf("face %d texel (%d,%d) = %+v, want %+v", face, x, y, got, want)
				}
			}
		}
	}
}

func TestPrefilterKeepsConstantEnvironment(t *testing.T) {
	// The GGX convolution normalizes its weights, so a constant environment
	// must survive at every roughness.
	source := filledCube(16, Vec3{1.5, 2.5, 0.5})
	chain := Prefilter(source, PrefilterOptions{Samples: 64, MipSelect: true})
	for level, mip := range chain {
		for face := 0; face < 6; face++ {
			for y := 0; y < mip.Size; y++ {
				for x := 0; x < mip.Size; x++ {
					got := mip.Get(face, x, y)
					if math.Abs(got.X-1.5) > 1e-4 || math.Abs(got.Y-2.5) > 1e-4 || math.Abs(got.Z-0.5) > 1e-4 {
						t.Fatalf("level %d face %d texel (%d,%d) = %+v, want (1.5, 2.5, 0.5)", level, face, x, y, got)
					}
				}
			}
		}
	}
}

func TestPrefilterWidensWithRoughness(t *testing.T) {
	// A single bright face must spread as roughness grows, so the variance of
	// the level must fall monotonically.
	source := NewCube(32)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			source.Set(FacePosY, x, y, Vec3{10, 10, 10})
		}
	}
	chain := BuildChain(source)
	saTexel := 4 * math.Pi / (6 * 32 * 32)
	up := Vec3{0, 1, 0}
	previous := math.Inf(1)
	for step := 0; step <= 10; step++ {
		roughness := float64(step) / 10
		value := prefilterTexel(chain, up, roughness*roughness, 512, saTexel, true).X
		if value > previous+1e-3 {
			t.Fatalf("roughness %.1f reads %.5f at the bright pole, above the previous %.5f", roughness, value, previous)
		}
		previous = value
	}
	// A wide lobe must pull the pole away from the source radiance of 10 and
	// toward the average over the sphere, which is 10/6 here.
	if previous > 6 {
		t.Fatalf("roughness 1 still reads %.5f at the pole, expected a much wider average", previous)
	}
}

func TestRoughnessForLevel(t *testing.T) {
	if got := RoughnessForLevel(0, 5); got != 0 {
		t.Fatalf("level 0 roughness = %v, want 0", got)
	}
	if got := RoughnessForLevel(4, 5); got != 1 {
		t.Fatalf("last level roughness = %v, want 1", got)
	}
	if got := RoughnessForLevel(2, 5); math.Abs(got-0.5) > 1e-12 {
		t.Fatalf("middle level roughness = %v, want 0.5", got)
	}
}
