package worklet

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
)

//go:embed processor.js
var source []byte

// Path is content-addressed so a cached processor always matches the Go host.
// The source is hashed only when an asset API is called; importing the browser
// ABI does not initialize or retain an unused processor asset.
func Path() string { return "worklet/wasm-processor." + processorDigest()[:16] + ".js" }

// Source returns an independent copy of the framework-owned processor.
func Source() []byte { return bytes.Clone(source) }

func processorDigest() string { return fmt.Sprintf("%x", sha256.Sum256(source)) }
