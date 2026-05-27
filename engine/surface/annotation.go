// Detection of the legacy `//gosx:engine surface=wasm` escape-hatch
// annotation. The annotation referred to the per-component WASM backend
// (internal/buildsurface) which was deleted per ADR 0005. Surfaces still
// carrying the annotation must be flagged so authors know to drop it —
// the bytecode pipeline now handles every surface unconditionally.

package surface

import (
	"bufio"
	"bytes"
	"strings"
)

// hasWASMEscapeHatch reports whether gsxSrc declares the obsolete
// `//gosx:engine surface=wasm` (or `surface wasm`) annotation. The
// scan is intentionally simple: a single pass over lines, looking for
// the `//gosx:engine` prefix, then checking the rest for `wasm`.
func hasWASMEscapeHatch(gsxSrc []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(gsxSrc))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(trimmed, "//gosx:engine") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "//gosx:engine"))
		if containsWASMToken(rest) {
			return true
		}
	}
	return false
}

// containsWASMToken reports whether s mentions "wasm" as a backend
// selector. Accepted shapes: `surface=wasm`, `surface wasm`, just
// `wasm`. Case-insensitive.
func containsWASMToken(s string) bool {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "surface=wasm") {
		return true
	}
	for _, f := range strings.Fields(lower) {
		if f == "wasm" {
			return true
		}
	}
	return false
}
