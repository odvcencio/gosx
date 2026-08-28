package wasm

import "testing"

func TestFeatureMasksAreClosedAndComposable(t *testing.T) {
	if FeatureMaskForVariant(VariantCore) != FeatureCore|FeatureIslands {
		t.Fatalf("core mask = 0x%x", FeatureMaskForVariant(VariantCore))
	}
	full := FeatureMaskForVariant(VariantFull)
	if full != FeatureCore|FeatureEngine|FeatureCollab|FeatureScene3D|FeatureIslands {
		t.Fatalf("full mask = 0x%x", full)
	}
	if FeatureMaskForVariant("unknown") != 0 {
		t.Fatal("unknown variant unexpectedly exposed features")
	}
	if got := FeatureMaskForVariant(VariantEngine); got != FeatureCore|FeatureEngine|FeatureScene3D|FeatureIslands {
		t.Fatalf("engine mask = 0x%x", got)
	}
	wantPublished := []Variant{VariantCore, VariantEngine, VariantCollab, VariantFull}
	if got := PublishedVariants(); len(got) != len(wantPublished) {
		t.Fatalf("published variants = %v, want %v", got, wantPublished)
	} else {
		for index := range got {
			if got[index] != wantPublished[index] {
				t.Fatalf("published variants = %v, want %v", got, wantPublished)
			}
		}
	}
}

func TestHandshakeValidatesABIAndRequiredFeatures(t *testing.T) {
	h := NewHandshake(VariantFull)
	if err := h.Validate(FeatureEngine | FeatureCollab); err != nil {
		t.Fatalf("full handshake rejected: %v", err)
	}
	h.ABIVersion++
	if err := h.Validate(FeatureCore); err == nil {
		t.Fatal("ABI mismatch was accepted")
	}
	h = NewHandshake(VariantCore)
	if err := h.Validate(FeatureEngine); err == nil {
		t.Fatal("missing feature was accepted")
	}
}

func TestSelectVariantChoosesSmallestCapabilitySet(t *testing.T) {
	cases := []struct {
		name     string
		required FeatureMask
		want     Variant
	}{
		{name: "core", required: FeatureCore, want: VariantCore},
		{name: "islands", required: FeatureCore | FeatureIslands, want: VariantCore},
		{name: "engine", required: FeatureCore | FeatureEngine, want: VariantEngine},
		{name: "island plus engine", required: FeatureCore | FeatureIslands | FeatureEngine, want: VariantEngine},
		{name: "scene", required: FeatureCore | FeatureScene3D, want: VariantEngine},
		{name: "collab", required: FeatureCore | FeatureCollab, want: VariantCollab},
		{name: "full", required: FeatureCore | FeatureEngine | FeatureCollab, want: VariantFull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SelectVariant(tc.required)
			if !ok || got != tc.want {
				t.Fatalf("SelectVariant(0x%x) = %q, %v; want %q, true", uint32(tc.required), got, ok, tc.want)
			}
			if err := NewHandshake(got).Validate(tc.required); err != nil {
				t.Fatalf("selected variant does not validate: %v", err)
			}
		})
	}
	if got, ok := SelectVariant(FeatureMask(1 << 20)); ok || got != "" {
		t.Fatalf("unknown feature selected variant %q, %v", got, ok)
	}
}

func TestHandshakeRejectsContractAndVariantDrift(t *testing.T) {
	h := NewHandshake(VariantIslands)
	if h.ManifestHash == "" {
		t.Fatal("manifest identity is empty")
	}
	h.FeatureMask = FeatureCore
	if err := h.Validate(FeatureCore); err == nil {
		t.Fatal("variant feature-mask drift was accepted")
	}
	h = NewHandshake(VariantIslands)
	h.ManifestHash = "stale"
	if err := h.Validate(FeatureCore); err == nil {
		t.Fatal("stale manifest identity was accepted")
	}
}
