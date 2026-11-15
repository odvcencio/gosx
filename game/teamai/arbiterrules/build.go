//go:build !js || !wasm

package arbiterrules

import (
	"fmt"

	"m31labs.dev/arbiter/gameai/build"
)

// This file links Arbiter's .arb parser, so it is left out of js/wasm builds.
// Compile bundles at build time (or on a development server) and ship the
// bundle; the browser loads it with Load.

// Compile compiles .arb source into Rules, keeping rule names.
func Compile(src []byte) (*Rules, error) {
	res, err := build.Compile(src, build.Options{KeepNames: true})
	if err != nil {
		return nil, fmt.Errorf("arbiterrules: %w", err)
	}
	return New(res.Program)
}

// CompileFile compiles an .arb file (and its imports) into Rules, keeping rule
// names.
func CompileFile(path string) (*Rules, error) {
	res, err := build.CompileFile(path, build.Options{KeepNames: true})
	if err != nil {
		return nil, fmt.Errorf("arbiterrules: %w", err)
	}
	return New(res.Program)
}

// Bundle compiles .arb source into the bundle bytes Load reads, keeping rule
// names. The same source always gives the same bytes.
func Bundle(src []byte) ([]byte, error) {
	res, err := build.Compile(src, build.Options{KeepNames: true})
	if err != nil {
		return nil, fmt.Errorf("arbiterrules: %w", err)
	}
	return res.Bundle, nil
}
