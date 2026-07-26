package texture

import (
	"errors"
	"math"
	"testing"
)

// TestPackMetallicRoughnessFollowsTheGLTFLayout checks the channel order glTF
// already expects: occlusion in red, roughness in green, metalness in blue.
//
// Getting the order wrong is invisible to every later stage. A renderer reads
// green for roughness whatever the file holds, so a swapped pack produces a
// material that is smooth where it should be rough, and no test downstream of
// this one can tell.
func TestPackMetallicRoughnessFollowsTheGLTFLayout(t *testing.T) {
	occlusion := constantImage(2, 2, 0.25)
	roughness := constantImage(2, 2, 0.5)
	metalness := constantImage(2, 2, 0.75)

	packed, err := PackMetallicRoughness(occlusion, roughness, metalness)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := packed.At(1, 1)
	if math.Abs(float64(r)-0.25) > 1e-6 {
		t.Errorf("red holds %g, want the occlusion 0.25", r)
	}
	if math.Abs(float64(g)-0.5) > 1e-6 {
		t.Errorf("green holds %g, want the roughness 0.5", g)
	}
	if math.Abs(float64(b)-0.75) > 1e-6 {
		t.Errorf("blue holds %g, want the metalness 0.75", b)
	}
	if a != 1 {
		t.Errorf("alpha holds %g, want 1", a)
	}
}

// TestPackMetallicRoughnessFillsGLTFDefaults checks the missing-input case.
// glTF defaults roughnessFactor and metallicFactor to 1, so an absent map must
// contribute 1 and not 0.
func TestPackMetallicRoughnessFillsGLTFDefaults(t *testing.T) {
	roughness := constantImage(2, 2, 0.3)
	packed, err := PackMetallicRoughness(nil, roughness, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := packed.At(0, 0)
	if r != 1 {
		t.Errorf("absent occlusion gave %g, want 1", r)
	}
	if math.Abs(float64(g)-0.3) > 1e-6 {
		t.Errorf("roughness gave %g, want 0.3", g)
	}
	if b != 1 {
		t.Errorf("absent metalness gave %g, want 1", b)
	}
}

func TestPackRejectsMismatchedSizes(t *testing.T) {
	if _, err := PackMetallicRoughness(constantImage(2, 2, 1), constantImage(4, 4, 1), nil); !errors.Is(err, ErrShape) {
		t.Fatal("mismatched sizes must fail with ErrShape")
	}
	if _, err := PackMetallicRoughness(nil, nil, nil); !errors.Is(err, ErrShape) {
		t.Fatal("no input at all must fail with ErrShape")
	}
	if _, err := Pack(2, 2, nil); !errors.Is(err, ErrShape) {
		t.Fatal("no sources must fail with ErrShape")
	}
}

// TestPackedPairEncodesToTwoBytes measures the byte saving of the packing.
//
// Two separate grayscale maps cost two r8unorm textures. Packed into one
// rg8unorm texture they cost the same GPU bytes but half the draw-time sampler
// slots and one texture binding instead of two. Packed into the glTF
// three-channel layout they cost three bytes per texel against the four a
// naive rgba8 export would use.
func TestPackedPairEncodesToTwoBytes(t *testing.T) {
	roughness := constantImage(8, 8, 0.5)
	metalness := constantImage(8, 8, 0.25)
	packed, err := Pack(8, 8, []Source{
		{Image: roughness, Channel: ChannelRed},
		{Image: metalness, Channel: ChannelRed},
	})
	if err != nil {
		t.Fatal(err)
	}
	two, err := EncodeBytes(packed, Linear, 2)
	if err != nil {
		t.Fatal(err)
	}
	four, err := EncodeBytes(packed, Linear, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(two) != 8*8*2 || len(four) != 8*8*4 {
		t.Fatalf("packed sizes are %d and %d bytes", len(two), len(four))
	}
	if two[0] != 128 || two[1] != 64 {
		t.Fatalf("rg8 pack holds %d and %d, want 128 and 64", two[0], two[1])
	}
}
