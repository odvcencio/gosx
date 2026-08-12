// Package wasm contains the versioned browser/WASM wire contract.
//
// The package deliberately has no syscall/js dependency.  The same constants
// and codecs are used by the host-side tests, the TinyGo entry point, and the
// generated TypeScript contract, so a browser can reject an incompatible
// runtime before it starts sending hot-path messages.
package wasm

import (
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

// Handshake is returned by the direct runtime export before the host selects a
// mailbox path.  ManifestHash is optional for development builds and required
// by canonical production evidence.
type Handshake struct {
	ABIVersion     uint32      `json:"abiVersion"`
	FeatureMask    FeatureMask `json:"featureMask"`
	Variant        Variant     `json:"variant"`
	MailboxVersion uint16      `json:"mailboxVersion"`
	ManifestHash   string      `json:"manifestHash,omitempty"`
}

// NewHandshake creates the runtime declaration for a compiled variant.
func NewHandshake(variant Variant, manifestHash string) Handshake {
	return Handshake{
		ABIVersion:     ABIVersion,
		FeatureMask:    FeatureMaskForVariant(variant),
		Variant:        variant,
		MailboxVersion: MailboxVersion,
		ManifestHash:   strings.TrimSpace(manifestHash),
	}
}

// FeatureMaskForVariant returns the closed capability set for a variant.
// Later variants may add bits, but they must never repurpose an existing bit.
func FeatureMaskForVariant(variant Variant) FeatureMask {
	switch variant {
	case VariantCore:
		return FeatureCore
	case VariantEngine:
		return FeatureCore | FeatureEngine | FeatureScene3D
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
	if h.FeatureMask&required != required {
		return fmt.Errorf("runtime %q is missing feature mask 0x%x", h.Variant, uint32(required&^h.FeatureMask))
	}
	return nil
}

// Supports reports whether the handshake exposes every requested feature.
func (h Handshake) Supports(required FeatureMask) bool {
	return h.Validate(required) == nil
}
