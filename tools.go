//go:build tools

// Package gosx tool-dependency anchor.
//
// Blank-imports here keep sibling Orchard modules pinned in go.mod even
// before their consumer code lands in-tree. Each of these will grow a
// real import path when the corresponding migration ships:
//
//   - turboquant: already live via quant/vecdb/scene. Pinned here so a
//     deliberate version bump stays explicit.
//   - corkscrewdb: slated to replace vecdb/ + workspace/semantic/ flat
//     search with persistent versioned collections.
//   - manta/runtime: slated to replace embed/ with .mll-backed tokenize
//     and embed entry points.
//
// The `tools` build tag keeps these imports out of every normal build
// but visible to `go mod tidy`, so the dependencies survive cleanup.
package gosx

import (
	_ "github.com/odvcencio/corkscrewdb"
	_ "github.com/odvcencio/manta/runtime"
	_ "github.com/odvcencio/turboquant"
)
