package capability

import (
	"sort"
	"testing"
)

func TestVerdict(t *testing.T) {
	cases := []struct {
		name     string
		features []Feature
		want     []Backend
		degraded map[Backend][]Feature
	}{
		{"plain scene", nil,
			[]Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D}, nil},
		{"skinning supports webgpu and webgl", []Feature{FeatureSkinning},
			[]Backend{BackendWebGPU, BackendWebGL}, nil},
		// ibl is droppable. WebGPU consumes the products unconditionally, so it
		// stays capable with no gap; WebGL2 and Canvas2D stay capable with the
		// gap listed. See TestIBLTruthMatchesRuntimeConsumers in
		// water_shadow_test.go and ibl_test.go.
		{"ibl droppable: every backend stays, webgpu clean, webgl+canvas2d degraded", []Feature{FeatureIBL},
			[]Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D},
			map[Backend][]Feature{
				BackendWebGL:    {FeatureIBL},
				BackendCanvas2D: {FeatureIBL},
			}},
		{"skinning+ibl: canvas2d excluded by skinning, webgl degraded by ibl, webgpu clean",
			[]Feature{FeatureSkinning, FeatureIBL},
			[]Backend{BackendWebGPU, BackendWebGL},
			map[Backend][]Feature{
				BackendWebGL: {FeatureIBL},
			}},
		{"water simulation supports webgpu and webgl", []Feature{FeatureWaterSim},
			[]Backend{BackendWebGPU, BackendWebGL}, nil},
		// The mesh-shadow pass is WebGPU-only and DROPPABLE, so WebGL2 stays
		// capable with the gap listed. canvas2d also stays capable here only
		// because this case names the feature alone; a real water scene also
		// carries water-simulation and water-object-texture-pass, and both of
		// those are required and exclude canvas2d. The next case shows that.
		{"water object mesh shadow pass degrades webgl and canvas2d, excludes nobody",
			[]Feature{FeatureWaterObjectMeshShadowPass},
			[]Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D},
			map[Backend][]Feature{
				BackendWebGL:    {FeatureWaterObjectMeshShadowPass},
				BackendCanvas2D: {FeatureWaterObjectMeshShadowPass},
			}},
		// A realistic water scene. canvas2d falls out on the two required water
		// features; WebGL2 survives and reports only the shadow-shape gap.
		{"full water scene: canvas2d excluded, webgl degraded by the mesh shadow alone",
			[]Feature{FeatureWaterSim, FeatureWaterObjectTexturePass, FeatureWaterObjectMeshShadowPass},
			[]Backend{BackendWebGPU, BackendWebGL},
			map[Backend][]Feature{
				BackendWebGL: {FeatureWaterObjectMeshShadowPass},
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Verdict(tc.features, nil, DefaultPolicy())
			if !backendsEqual(got.Capable, tc.want) {
				t.Fatalf("Capable = %v, want %v", got.Capable, tc.want)
			}
			if tc.degraded != nil && !degradedEqual(got.Degraded, tc.degraded) {
				t.Fatalf("Degraded = %v, want %v", got.Degraded, tc.degraded)
			}
		})
	}
}

// TestVerdictReportsEveryRequiredUnsupportedFeature checks that an excluded
// backend gets a named reason for EACH required feature it fails, not just the
// first one. An author fixing one gap must see the rest.
//
// Only the two REQUIRED water features exclude canvas2d. The mesh-shadow pass
// is droppable, so it can never exclude anybody; TestVerdict covers its degrade
// path and water_shadow_test.go explains why it is droppable.
func TestVerdictReportsEveryRequiredUnsupportedFeature(t *testing.T) {
	required := []Feature{FeatureWaterObjectTexturePass, FeatureWaterSim}
	features := append([]Feature{FeatureWaterObjectMeshShadowPass}, required...)
	got := Verdict(features, nil, DefaultPolicy())
	// Both GPU backends implement the two required water features, so only
	// canvas2d falls out.
	if !backendsEqual(got.Capable, []Backend{BackendWebGPU, BackendWebGL}) {
		t.Fatalf("Capable = %v, want [webgpu webgl]", got.Capable)
	}
	for _, feature := range required {
		if !hasExcludeReason(got.Reasons, feature, BackendCanvas2D) {
			t.Fatalf("expected exclude reason for %s/%s, got %+v", feature, BackendCanvas2D, got.Reasons)
		}
		if hasExcludeReason(got.Reasons, feature, BackendWebGL) {
			t.Fatalf("did not expect webgl exclusion for %s now that WebGL implements it, got %+v", feature, got.Reasons)
		}
	}
	// A droppable feature must never produce an exclude reason for any backend.
	for _, b := range []Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D} {
		if hasExcludeReason(got.Reasons, FeatureWaterObjectMeshShadowPass, b) {
			t.Fatalf("water-object-mesh-shadow-pass is droppable and must never exclude %s, got %+v", b, got.Reasons)
		}
	}
	// An EXCLUDED backend must not also accumulate degraded features. canvas2d is
	// gone, so its mesh-shadow gap is moot; WebGL2 survives and keeps its gap.
	if _, ok := got.Degraded[BackendCanvas2D]; ok {
		t.Fatalf("an excluded backend must not accumulate degraded features, got %v", got.Degraded)
	}
	if want := []Feature{FeatureWaterObjectMeshShadowPass}; len(got.Degraded[BackendWebGL]) != 1 || got.Degraded[BackendWebGL][0] != want[0] {
		t.Fatalf("WebGL2 must be degraded by the mesh-shadow pass alone, got %v", got.Degraded)
	}
}

func hasExcludeReason(reasons []CapReason, feature Feature, backend Backend) bool {
	for _, reason := range reasons {
		if reason.Feature == feature && reason.Excludes == backend {
			return true
		}
	}
	return false
}

// backendsEqual compares two Backend slices by sorted order (order-insensitive).
func backendsEqual(a, b []Backend) bool {
	if len(a) != len(b) {
		return false
	}
	as := make([]string, len(a))
	bs := make([]string, len(b))
	for i, v := range a {
		as[i] = string(v)
	}
	for i, v := range b {
		bs[i] = string(v)
	}
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// degradedEqual compares two degraded maps order-insensitively per backend.
func degradedEqual(a, b map[Backend][]Feature) bool {
	if len(a) != len(b) {
		return false
	}
	for bk, bfeats := range b {
		afeats, ok := a[bk]
		if !ok {
			return false
		}
		if len(afeats) != len(bfeats) {
			return false
		}
		as := make([]string, len(afeats))
		bs := make([]string, len(bfeats))
		for i, v := range afeats {
			as[i] = string(v)
		}
		for i, v := range bfeats {
			bs[i] = string(v)
		}
		sort.Strings(as)
		sort.Strings(bs)
		for i := range as {
			if as[i] != bs[i] {
				return false
			}
		}
	}
	return true
}
