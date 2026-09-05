package host

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
)

//go:embed server-slots-runtime.min.js
var serverSlotRuntime string

var serverSlotDigest = sha256.Sum256([]byte(serverSlotRuntime))

// ServerSlotRuntime is the separately generated, immutable ownership host.
func ServerSlotRuntime() string     { return serverSlotRuntime }
func ServerSlotRuntimeHash() string { return hex.EncodeToString(serverSlotDigest[:8]) }
func ServerSlotRuntimeIntegrity() string {
	return "sha256-" + base64.StdEncoding.EncodeToString(serverSlotDigest[:])
}
