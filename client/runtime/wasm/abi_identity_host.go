//go:build !js || !wasm

package wasm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

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
