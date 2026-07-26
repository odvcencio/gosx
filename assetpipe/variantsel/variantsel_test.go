package variantsel

import (
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

// TestTokensDeriveFromTheCapabilityConstants pins the shared vocabulary.
//
// The whole design rests on one property: a variant's capability string and the
// server's verdict spell a backend the same way. If someone renames
// capability.BackendWebGL to "webgl2", this test tells them which token moved.
func TestTokensDeriveFromTheCapabilityConstants(t *testing.T) {
	cases := []struct {
		backend capability.Backend
		want    Token
	}{
		{capability.BackendWebGPU, "backend:webgpu"},
		{capability.BackendWebGL, "backend:webgl"},
		{capability.BackendCanvas2D, "backend:canvas2d"},
	}
	for _, tc := range cases {
		if got := BackendToken(tc.backend); got != tc.want {
			t.Errorf("BackendToken(%q) = %q, want %q", tc.backend, got, tc.want)
		}
	}
	if got := FeatureToken(capability.FeatureIBL); got != "feature:ibl" {
		t.Errorf("FeatureToken(ibl) = %q, want feature:ibl", got)
	}
	// Every backend in the capability package must have a format row, even an
	// empty one, so a new backend cannot silently inherit WebGPU's formats.
	for _, backend := range []capability.Backend{
		capability.BackendWebGPU, capability.BackendWebGL, capability.BackendCanvas2D,
	} {
		if _, ok := backendFormats[backend]; !ok {
			t.Errorf("backend %q has no texture capability row", backend)
		}
	}
}

// TestFromBackendCapsIntersectsGPUFormats is the safety property.
//
// A verdict that keeps both GPU backends means the browser may choose either
// one. The capability set must therefore contain only what BOTH can upload.
// rgb8unorm is a WebGL2 format with no WebGPU equivalent, so it must drop out.
func TestFromBackendCapsIntersectsGPUFormats(t *testing.T) {
	both := FromBackendCaps(capability.BackendCaps{
		Capable: []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL, capability.BackendCanvas2D},
	})
	if !both.Has(FormatRGBA8UnormSRGB) {
		t.Error("both backends upload rgba8unorm-srgb; the token must survive the intersection")
	}
	if both.Has(FormatRGB8Unorm) || both.Has(FormatRGB8UnormSRGB) {
		t.Error("WebGPU has no rgb8unorm; the intersection must drop it")
	}
	if !both.Has(BackendToken(capability.BackendCanvas2D)) {
		t.Error("a capable backend must still contribute its own backend token")
	}

	// A verdict pinned to WebGL2 alone keeps the three-channel format.
	webglOnly := FromBackendCaps(capability.BackendCaps{
		Capable: []capability.Backend{capability.BackendWebGL},
	})
	if !webglOnly.Has(FormatRGB8UnormSRGB) {
		t.Error("a WebGL2-only verdict must keep rgb8unorm-srgb")
	}
}

// TestFromBackendCapsGivesCanvas2DNoTextureFormats records the honest answer for
// a page that uploads no GPU texture.
func TestFromBackendCapsGivesCanvas2DNoTextureFormats(t *testing.T) {
	set := FromBackendCaps(capability.BackendCaps{
		Capable: []capability.Backend{capability.BackendCanvas2D},
	})
	if set.Has(ContainerKTX2) || set.Has(FormatRGBA8Unorm) {
		t.Fatalf("a Canvas2D-only verdict must carry no texture capability, got %v", set.Sorted())
	}
	if !set.Has(BackendToken(capability.BackendCanvas2D)) {
		t.Fatal("the backend token itself must be present")
	}
}

// TestFromBackendCapsCarriesDegradedFeatures checks the feature tokens.
func TestFromBackendCapsCarriesDegradedFeatures(t *testing.T) {
	caps := capability.Verdict([]capability.Feature{capability.FeatureIBL}, nil, capability.DefaultPolicy())
	set := FromBackendCaps(caps)
	// The IBL cell is false for WebGPU today, so the verdict degrades WebGPU
	// and the set must carry the feature token.
	if !set.Has(FeatureToken(capability.FeatureIBL)) {
		t.Fatalf("the verdict degraded ibl but the set has %v", set.Sorted())
	}
}

// TestFromBackendCapsHandlesAnEmptyVerdict checks the no-capable-backend case.
func TestFromBackendCapsHandlesAnEmptyVerdict(t *testing.T) {
	set := FromBackendCaps(capability.BackendCaps{})
	if len(set) != 0 {
		t.Fatalf("an empty verdict gave %v, want nothing", set.Sorted())
	}
}

func TestBudgetFromHints(t *testing.T) {
	cases := []struct {
		saveData bool
		memory   float64
		want     Token
	}{
		{true, 8, BudgetLow},       // Save-Data is a user instruction and wins.
		{false, 0, BudgetStandard}, // No hint at all.
		{false, 0.5, BudgetLow},
		{false, 1, BudgetLow},
		{false, 2, BudgetStandard},
		{false, 4, BudgetHigh},
		{false, 8, BudgetHigh},
	}
	for _, tc := range cases {
		if got := BudgetFromHints(tc.saveData, tc.memory); got != tc.want {
			t.Errorf("BudgetFromHints(%v, %g) = %q, want %q", tc.saveData, tc.memory, got, tc.want)
		}
	}
}

// TestTierTokensGateOnlyTheHighTier states the ladder rule. A low budget must
// still be able to take the standard and the low variant, otherwise a
// data-saver page gets no texture at all.
func TestTierTokensGateOnlyTheHighTier(t *testing.T) {
	if tokens := TierTokens("high"); len(tokens) != 1 || tokens[0] != BudgetHigh {
		t.Fatalf("TierTokens(high) = %v, want [budget:high]", tokens)
	}
	for _, tier := range []string{"standard", "low", "", "medium"} {
		if tokens := TierTokens(tier); len(tokens) != 0 {
			t.Fatalf("TierTokens(%q) = %v, want nothing", tier, tokens)
		}
	}
}

func TestSetHasAllNormalizesCase(t *testing.T) {
	set := NewSet(ContainerKTX2, FormatR8Unorm)
	if !set.HasAll([]string{"CONTAINER:KTX2", " texture-format:r8unorm "}) {
		t.Fatal("HasAll must normalize case and trim space")
	}
	if set.HasAll([]string{"container:ktx2", "texture-format:bc7-rgba-unorm-srgb"}) {
		t.Fatal("HasAll must fail on a missing token")
	}
	if !set.HasAll(nil) {
		t.Fatal("an empty requirement list is satisfied by any set")
	}
}

func TestStringsSortsAndDeduplicates(t *testing.T) {
	got := Strings(FormatR8Unorm, ContainerKTX2, FormatR8Unorm, "")
	want := []string{"container:ktx2", "texture-format:r8unorm"}
	if len(got) != len(want) {
		t.Fatalf("Strings gave %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Strings gave %v, want %v", got, want)
		}
	}
}
