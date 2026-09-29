package capability

import "testing"

func TestOceanIsNotSupportedYet(t *testing.T) {
	for _, backend := range []Backend{BackendCanvas2D, BackendWebGPU, BackendWebGL} {
		if Supports(backend, FeatureOcean) {
			t.Errorf("%s unexpectedly supports %s", backend, FeatureOcean)
		}
	}
	if DefaultPolicy().Required[FeatureOcean] {
		t.Fatalf("%s must not be required by DefaultPolicy", FeatureOcean)
	}
}
