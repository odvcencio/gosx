// Slice X.D — engine-surface annotation parsing.
//
// Per ADR 0006 the `//gosx:engine surface=wasm` annotation opts a
// surface out of the shared-VM bytecode lowering and back into the
// per-component WASM build path. The default (annotation absent or
// just `//gosx:engine surface`) routes through ir/golower to bytecode.
//
// We re-scan the .gsx source for the annotation here rather than
// adding a backend field to the IR's Component struct. The IR doesn't
// need to know about backend choice — the build pipeline does — and
// keeping the surface here avoids a cross-cut change that would touch
// every test that constructs a Component fixture.

package surface

import (
	"bufio"
	"bytes"
	"strings"
)

// surfaceBackend names the lowering backend a component opted into.
type surfaceBackend int

const (
	// backendBytecode is the default: the surface lowers to shared-VM
	// bytecode via ir/golower (Slice X.D).
	backendBytecode surfaceBackend = iota
	// backendWASM is the ADR 0006 escape hatch: the surface compiles
	// to a per-component WASM artifact via internal/buildsurface.
	backendWASM
)

// detectedAnnotation pairs the chosen backend with the source line the
// `//gosx:engine` comment appeared on, for the escape-hatch policy to
// match against gap-file entries.
type detectedAnnotation struct {
	backend surfaceBackend
	line    int // 1-based; 0 means "no annotation found"
}

// scanSurfaceAnnotation reads gsxSrc looking for the //gosx:engine
// directive. Returns the backend choice and the line number. Surfaces
// with no engine annotation default to bytecode; an annotation of
// `surface=wasm` (or `surface wasm` or `surface wasm-only` etc.) opts
// into WASM.
//
// The scan is intentionally simple: a single pass over lines, looking
// for the `//gosx:engine ` prefix, then checking the rest for
// `wasm`. Tree-sitter or proper parsing would be over-engineered for
// what amounts to a one-line directive.
func scanSurfaceAnnotation(gsxSrc []byte) detectedAnnotation {
	scanner := bufio.NewScanner(bytes.NewReader(gsxSrc))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		trimmed := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(trimmed, "//gosx:engine") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "//gosx:engine"))
		// Bare `//gosx:engine` or `//gosx:engine surface` → bytecode.
		// `//gosx:engine surface=wasm` or any `wasm` token → WASM.
		if containsWASMToken(rest) {
			return detectedAnnotation{backend: backendWASM, line: line}
		}
		return detectedAnnotation{backend: backendBytecode, line: line}
	}
	return detectedAnnotation{backend: backendBytecode, line: 0}
}

// containsWASMToken reports whether s mentions "wasm" as a backend
// selector. Accepted shapes: `surface=wasm`, `surface wasm`, just
// `wasm`. The check is case-insensitive and ignores trailing
// whitespace/comments.
func containsWASMToken(s string) bool {
	lower := strings.ToLower(s)
	// `surface=wasm` is the canonical spelling from ADR 0006.
	if strings.Contains(lower, "surface=wasm") {
		return true
	}
	// Tolerate `surface wasm` (space-separated) for typing convenience.
	fields := strings.Fields(lower)
	for _, f := range fields {
		if f == "wasm" {
			return true
		}
	}
	return false
}
