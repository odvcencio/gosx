package worklet

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
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

// DataURL returns the processor as a data: URL, for a page that has no server
// to mount Handler on: a static export (gosx export), served from any folder
// or opened from disk as its single HTML file. The URL carries the same source
// as Path. A page whose Content-Security-Policy allows no data: scripts must
// serve Handler instead.
func DataURL() string {
	return "data:text/javascript;base64," + base64.StdEncoding.EncodeToString(source)
}

func processorDigest() string { return fmt.Sprintf("%x", sha256.Sum256(source)) }
