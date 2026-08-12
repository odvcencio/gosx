package wasm

import "testing"

func TestFeatureMasksAreClosedAndComposable(t *testing.T) {
	if FeatureMaskForVariant(VariantCore) != FeatureCore {
		t.Fatalf("core mask = 0x%x", FeatureMaskForVariant(VariantCore))
	}
	full := FeatureMaskForVariant(VariantFull)
	if full != FeatureCore|FeatureEngine|FeatureCollab|FeatureScene3D|FeatureIslands {
		t.Fatalf("full mask = 0x%x", full)
	}
	if FeatureMaskForVariant("unknown") != 0 {
		t.Fatal("unknown variant unexpectedly exposed features")
	}
}

func TestHandshakeValidatesABIAndRequiredFeatures(t *testing.T) {
	h := NewHandshake(VariantFull, "manifest-sha")
	if err := h.Validate(FeatureEngine | FeatureCollab); err != nil {
		t.Fatalf("full handshake rejected: %v", err)
	}
	h.ABIVersion++
	if err := h.Validate(FeatureCore); err == nil {
		t.Fatal("ABI mismatch was accepted")
	}
	h = NewHandshake(VariantCore, "")
	if err := h.Validate(FeatureEngine); err == nil {
		t.Fatal("missing feature was accepted")
	}
}
