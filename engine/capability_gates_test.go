package engine

import "testing"

func TestValidateCapabilitiesAcceptsEnvironmentGates(t *testing.T) {
	if err := ValidateCapabilities([]Capability{CapHardwareGPU, CapFullMotion, CapFullData}); err != nil {
		t.Fatalf("environment gates must validate: %v", err)
	}
	if err := ValidateCapabilities([]Capability{"full-gpu"}); err == nil {
		t.Fatal("an unknown gate name must still fail")
	}
}
