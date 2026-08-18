package host

import _ "embed"

// NavigationRuntime is the browser navigation host embedded by
// server.NavigationScript. The compatibility adapter precedes the JSDoc-only
// TypeScript authority so navigation publishes legacy names through the same
// facade-owned boundary as the bootstrap bundles.
//
// NavigationRuntime ships minified (gosx#221). compatibility.ts and
// navigation.ts stay in this directory unminified as the sources of truth —
// for a human reader, for the JS test suite (client/js/runtime-test-
// harness.js reads both directly), and for cmd/buildbootstrap's TypeScript
// validation and chunk-closure check. navigation-runtime.min.js is the
// generated, committed artifact cmd/buildbootstrap builds from those two
// sources (see its inlineAssets table in cmd/buildbootstrap/main.go); only
// the generated artifact reaches the browser. Regenerate it with
// `go generate ./client/runtime/host` or `make build-bootstrap`, and verify
// it is current with `go run ./cmd/buildbootstrap --check` (run from
// cmd/buildbootstrap; see make test-js), which fails the build when this
// file drifts from compatibility.ts or navigation.ts.
//
//go:generate go run -C ../../../cmd/buildbootstrap -tags "grammar_subset grammar_subset_typescript" .
//go:embed navigation-runtime.min.js
var NavigationRuntime string
