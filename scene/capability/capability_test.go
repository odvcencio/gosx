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
		{"skinning forces webgl", []Feature{FeatureSkinning},
			[]Backend{BackendWebGL}, nil},
		{"ibl droppable: webgpu+canvas2d stay, ibl degraded", []Feature{FeatureIBL},
			[]Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D},
			map[Backend][]Feature{BackendWebGPU: {FeatureIBL}, BackendCanvas2D: {FeatureIBL}}},
		{"skinning+ibl: webgl (skinning required wins)", []Feature{FeatureSkinning, FeatureIBL},
			[]Backend{BackendWebGL}, nil},
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
