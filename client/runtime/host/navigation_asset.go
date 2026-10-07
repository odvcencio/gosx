package host

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
)

// NavigationRuntime is the generated standalone navigation host. Its readable
// authorities are compatibility.ts, disclosure.ts and navigation.ts. Regenerate
// the runtime and compressed sidecars with go generate ./client/runtime/host;
// make test-js checks their freshness and behavior.
//
//go:generate go run -C ../../../cmd/buildbootstrap -tags "grammar_subset grammar_subset_typescript" .
//go:embed navigation-runtime.min.js
var NavigationRuntime string

// NavigationRuntimeGzip is the build-time gzip representation.
//
//go:embed navigation-runtime.min.js.gz
var NavigationRuntimeGzip []byte

// NavigationRuntimeBrotli is the build-time Brotli representation.
//
//go:embed navigation-runtime.min.js.br
var NavigationRuntimeBrotli []byte

var navigationRuntimeDigest = sha256.Sum256([]byte(NavigationRuntime))

// NavigationRuntimePath identifies the runtime by the SHA-256 of its raw bytes.
var NavigationRuntimePath = "/gosx/assets/runtime/navigation." + hex.EncodeToString(navigationRuntimeDigest[:]) + ".js"

// NavigationRuntimeIntegrity binds the external script to the embedded bytes.
var NavigationRuntimeIntegrity = "sha256-" + base64.StdEncoding.EncodeToString(navigationRuntimeDigest[:])
