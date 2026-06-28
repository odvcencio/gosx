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
		{"ibl droppable: webgpu+canvas2d stay, ibl degraded", []Feature{FeatureIBL},
			[]Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D},
			map[Backend][]Feature{BackendWebGPU: {FeatureIBL}, BackendCanvas2D: {FeatureIBL}}},
		{"skinning+ibl: webgpu degraded by ibl, webgl full", []Feature{FeatureSkinning, FeatureIBL},
			[]Backend{BackendWebGPU, BackendWebGL},
			map[Backend][]Feature{BackendWebGPU: {FeatureIBL}}},
		{"water simulation requires webgpu", []Feature{FeatureWaterSim},
			[]Backend{BackendWebGPU}, nil},
		{"water object mesh shadow pass requires webgpu", []Feature{FeatureWaterObjectMeshShadowPass},
			[]Backend{BackendWebGPU}, nil},
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

func TestVerdictReportsEveryRequiredUnsupportedFeature(t *testing.T) {
	features := []Feature{FeatureWaterObjectMeshShadowPass, FeatureWaterObjectTexturePass, FeatureWaterSim}
	got := Verdict(features, nil, DefaultPolicy())
	if !backendsEqual(got.Capable, []Backend{BackendWebGPU}) {
		t.Fatalf("Capable = %v, want [webgpu]", got.Capable)
	}
	for _, backend := range []Backend{BackendWebGL, BackendCanvas2D} {
		for _, feature := range features {
			if !hasExcludeReason(got.Reasons, feature, backend) {
				t.Fatalf("expected exclude reason for %s/%s, got %+v", feature, backend, got.Reasons)
			}
		}
	}
	if len(got.Degraded) != 0 {
		t.Fatalf("excluded backends should not accumulate degraded features, got %v", got.Degraded)
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
