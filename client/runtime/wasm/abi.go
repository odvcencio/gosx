// Package wasm contains the versioned browser/WASM wire contract.
//
// The package deliberately has no syscall/js dependency.  The same constants
// and codecs are used by the host-side tests, the TinyGo entry point, and the
// generated TypeScript contract, so a browser can reject an incompatible
// runtime before it starts sending hot-path messages.
package wasm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// ABIVersion changes whenever the direct browser/WASM contract changes in a
	// way that cannot be handled by a compatibility reader.
	ABIVersion uint32 = 2

	// MailboxVersion is independent from the export ABI.  It lets the mailbox
	// codec evolve while the direct function surface remains compatible.
	MailboxVersion uint16 = 1
)

// FeatureMask identifies the capabilities compiled into one runtime variant.
type FeatureMask uint32

const (
	FeatureCore FeatureMask = 1 << iota
	FeatureEngine
	FeatureCollab
	FeatureScene3D
	FeatureIslands
)

// Variant is the stable name carried by the handshake and manifest.
type Variant string

const (
	VariantCore    Variant = "core"
	VariantEngine  Variant = "engine"
	VariantCollab  Variant = "collab"
	VariantFull    Variant = "full"
	VariantIslands Variant = "islands"
)

var publishedVariants = [...]Variant{
	VariantCore,
	VariantEngine,
	VariantCollab,
	VariantFull,
}

// PublishedVariants returns the capability-linked variants emitted in new
// build manifests. VariantIslands remains a read-compatible alias for older
// manifests, but is not a fifth advertised profile.
func PublishedVariants() []Variant {
	variants := make([]Variant, len(publishedVariants))
	copy(variants, publishedVariants[:])
	return variants
}

// Handshake is returned by the direct runtime export before the host selects a
// mailbox path. ManifestHash identifies the complete generated runtime
// contract, rather than a page or a particular compiled artifact.
type Handshake struct {
	ABIVersion     uint32      `json:"abiVersion"`
	FeatureMask    FeatureMask `json:"featureMask"`
	Variant        Variant     `json:"variant"`
	MailboxVersion uint16      `json:"mailboxVersion"`
	ManifestHash   string      `json:"manifestHash"`
}

// NewHandshake creates the runtime declaration for a compiled variant.
func NewHandshake(variant Variant) Handshake {
	return Handshake{
		ABIVersion:     ABIVersion,
		FeatureMask:    FeatureMaskForVariant(variant),
		Variant:        variant,
		MailboxVersion: MailboxVersion,
		ManifestHash:   ManifestIdentity(),
	}
}

// ManifestIdentity returns a stable digest of every value that affects the
// browser/WASM boundary. The generated browser contract embeds this value and
// the loader requires the manifest reference and runtime handshake to agree.
func ManifestIdentity() string {
	descriptor := fmt.Sprintf(
		"abi=%d;mailbox=%d;magic=%08x;header=%d;response=%d;status_ok=%d;"+
			"features=%d,%d,%d,%d,%d;variants=core:%d,engine:%d,collab:%d,full:%d;compatibility_variants=islands:%d;"+
			"direct_opcodes=%d,%d;outbound_opcodes=%d",
		ABIVersion, MailboxVersion, MailboxMagic, MailboxHeaderSize, MailboxFlagResponse, MailboxStatusOK,
		FeatureCore, FeatureEngine, FeatureCollab, FeatureScene3D, FeatureIslands,
		FeatureMaskForVariant(VariantCore), FeatureMaskForVariant(VariantEngine),
		FeatureMaskForVariant(VariantCollab), FeatureMaskForVariant(VariantFull),
		FeatureMaskForVariant(VariantIslands), MailboxOpcodeHandshake, MailboxOpcodePing,
		MailboxOpcodePatches,
	)
	digest := sha256.Sum256([]byte(descriptor))
	return hex.EncodeToString(digest[:])
}

// FeatureMaskForVariant returns the closed capability set for a variant.
// Later variants may add bits, but they must never repurpose an existing bit.
func FeatureMaskForVariant(variant Variant) FeatureMask {
	switch variant {
	case VariantCore:
		return FeatureCore | FeatureIslands
	case VariantEngine:
		return FeatureCore | FeatureEngine | FeatureScene3D | FeatureIslands
	case VariantCollab:
		return FeatureCore | FeatureCollab | FeatureIslands
	case VariantIslands:
		return FeatureCore | FeatureIslands
	case VariantFull:
		return FeatureCore | FeatureEngine | FeatureCollab | FeatureScene3D | FeatureIslands
	default:
		return 0
	}
}

// SelectVariant returns the smallest published runtime that satisfies the
// requested capabilities. The ordering is intentional: core is the narrow
// DOM/island bridge, engine adds shared engine and Scene3D support, collab adds
// collaboration, and full combines the two independent capability families.
// The legacy islands alias is accepted by handshakes but never selected.
func SelectVariant(required FeatureMask) (Variant, bool) {
	if required == 0 {
		return VariantCore, true
	}
	for _, variant := range publishedVariants {
		features := FeatureMaskForVariant(variant)
		if features&required == required {
			return variant, true
		}
	}
	return "", false
}

// RequiredFeaturesForVariant returns the capability declaration carried by a
// published variant. Keeping this helper beside SelectVariant makes route
// metadata independent from artifact filenames.
func RequiredFeaturesForVariant(variant Variant) FeatureMask {
	return FeatureMaskForVariant(variant)
}

// Validate checks the compatibility boundary before any runtime operation.
func (h Handshake) Validate(required FeatureMask) error {
	if h.ABIVersion != ABIVersion {
		return fmt.Errorf("runtime ABI mismatch: got %d, want %d", h.ABIVersion, ABIVersion)
	}
	if h.MailboxVersion != MailboxVersion {
		return fmt.Errorf("runtime mailbox mismatch: got %d, want %d", h.MailboxVersion, MailboxVersion)
	}
	if h.Variant == "" || FeatureMaskForVariant(h.Variant) == 0 {
		return fmt.Errorf("runtime handshake has unknown variant %q", h.Variant)
	}
	if expected := FeatureMaskForVariant(h.Variant); h.FeatureMask != expected {
		return fmt.Errorf("runtime %q feature mask mismatch: got 0x%x, want 0x%x", h.Variant, uint32(h.FeatureMask), uint32(expected))
	}
	if strings.TrimSpace(h.ManifestHash) != ManifestIdentity() {
		return fmt.Errorf("runtime manifest identity mismatch")
	}
	if h.FeatureMask&required != required {
		return fmt.Errorf("runtime %q is missing feature mask 0x%x", h.Variant, uint32(required&^h.FeatureMask))
	}
	return nil
}

// Supports reports whether the handshake exposes every requested feature.
func (h Handshake) Supports(required FeatureMask) bool {
	return h.Validate(required) == nil
}
