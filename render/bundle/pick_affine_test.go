package bundle

import (
	"m31labs.dev/gosx/engine"
	"math"
	"testing"
)

func TestPrimitivePickOrientationMatchesScaleAndSingularPolicy(t *testing.T) {
	for _, scale := range []float32{1e-30, 1, 1e30} {
		m := mat4Identity()
		m[0] = -2 * scale
		m[5] = 3 * scale
		m[10] = 4 * scale
		if primitivePickOrientation(m) != -1 {
			t.Errorf("scale %g lost mirror", scale)
		}
	}
	for _, m := range []mat4{{}, {1, 0, 0, 0, 0, 1, 0, 0, 0, 0, -1e-13, 0, 0, 0, 0, 1}} {
		if primitivePickOrientation(m) != 1 {
			t.Errorf("singular fallback=%v", m)
		}
	}
}

func TestPrimitivePickFacingGatePreservesSkinnedEnrichment(t *testing.T) {
	for _, skin := range []string{"", "unchanged-skin"} {
		b := engine.RenderBundle{Camera: engine.RenderCamera{Z: 2, FOV: math.Pi / 2, Near: .1, Far: 100}, InstancedMeshes: []engine.RenderInstancedMesh{{ID: "ring", Kind: "torus", Radius: 2, Tube: .5, RadialSegments: 64, TubularSegments: 32, InstanceCount: 1, Transforms: identityTransform(), SkinID: skin}}}
		r := &Renderer{}
		r.updatePickSpans(b)
		hit := r.pickTargetsForRequest(b, 32, 32, 65, 65).resultForID(1)
		want := 3.5
		if skin != "" {
			want = .5
		}
		if math.Abs(float64(hit.Depth)-want) > .01 {
			t.Errorf("skin=%q depth=%g want=%g", skin, hit.Depth, want)
		}
	}
}
