//go:build !js

// Bytecode lowering for engine surfaces (Slice X.D — AST-compiler
// initiative). Wraps ir/golower so engine/surface/discover.go can route
// annotation-free surface declarations to the shared-VM bytecode path
// per ADR 0003.
//
// Host-only (like discover.go, its sole caller): LowerToBytecode compiles
// .gsx source into bytecode at build time via ir/golower, which pulls in
// ir (and, transitively, gotreesitter for CST parsing — see ir/lower.go).
// The WASM client only ever hydrates the already-compiled bytecode this
// produces (client/bridge/bridge_engine_surface.go's decodeEngineSurfaceProgram
// documents the JSON this emits, but never calls LowerToBytecode itself); it
// has no legitimate reason to link the .gsx compiler or gotreesitter at all
// — compile_stub_tinygo.go states exactly this intent for gosx.Compile, but
// this file (lacking any exclusion) was the leak: TinyGo links gotreesitter
// transitively through ir -> ir/lower.go, and something in that linked-in
// grammar-loading path (encoding/gob's type-info construction against a
// non-empty interface) trips TinyGo's internal/reflectlite gap
// ("AssignableTo with interface" is unimplemented for interfaces with
// methods — see /usr/local/lib/tinygo/src/internal/reflectlite/type.go)
// during WASM boot, before any hydrate call — an unrecoverable panic that,
// combined with this build's -panic=trap, silently traps as a bare
// `unreachable` on every /admin/editor load. Excluding this file from js
// builds (matching discover.go's existing `!js` tag) removes the entire
// gotreesitter/gob chain from the WASM client, since neither of the other
// engine/surface files the client imports (canvas_host_impl.go, vm_host.go,
// context_host.go, surface.go, wrap.go, registry.go, head_assets.go,
// annotation.go) reference ir or gotreesitter.
//
// JS-side bootstrap wiring note: the bytecode dispatcher reads
// data-gosx-engine-bytecode from the rendered <canvas> placeholder
// and forwards it through client/bridge.HydrateReconciler with
// surfaceKind="canvas2d" or "scene3d". canvas2d hydration is gated
// behind Phase 2's <CanvasBoard> adapter (HydrateReconciler returns
// an error today). The JS bootstrap-feature-engines.js update that
// reads the new attribute is a separate follow-up — the Go side
// (this file + discover.go) is the contract this slice delivers.
//
// Output shape:
//   - One *program.Program per surface component, written as JSON to
//     .gosx/cache/surfaces/<hash>.json (alongside the existing per-component
//     WASM artifacts that surface=wasm escape-hatch surfaces still use).
//   - Hash is computed from the source-file fingerprint plus the lowerer
//     version, so a lowerer change invalidates the cache automatically.
//   - The Program's Surface field is set based on the component's
//     declared kind (Canvas2D or Scene3D); Surface is runtime-only and
//     never serialized, so the cached JSON stays clean.
//
// Surface kind is derived from the component's runtime context: today
// the engine-surface authoring contract only ships Canvas2D (Slice X.E
// targets the hyphae graph dogfood). Scene3D wiring stays the door open
// for the meta-plan's future 3D handlers.

package surface

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/ir/golower"
	"m31labs.dev/gosx/island/program"
)

// loweringSchemaVersion bumps when the lowered bytecode format changes
// incompatibly. Caches keyed on (sourceFingerprint, schemaVersion) so a
// version bump invalidates all prior artifacts without manual cleanup.
const loweringSchemaVersion = 1

// LoweringResult describes a successfully lowered surface program ready
// to be served to the client bootstrap.
type LoweringResult struct {
	// JSONPath is the absolute path to the cached Program JSON on disk.
	JSONPath string

	// JSONURL is the path the client bootstrap fetches the bytecode
	// from (e.g. /gosx/engines/<name>.<hash>.json). Mirrors the
	// per-component WASM URL shape so the surface registry can serve
	// both transports uniformly.
	JSONURL string

	// Hash is the cache key — sha256 prefix of the source fingerprint
	// plus the lowerer's schema version.
	Hash string

	// SurfaceKind tells the bootstrap which reconciler hydrates this
	// program. Carried on the manifest entry, then forwarded to the
	// client as data-gosx-engine-surface-kind="canvas2d" or "scene3d".
	SurfaceKind program.SurfaceKind
}

// LowerToBytecode lowers a discovered surface component's handler
// source into a Program JSON cached on disk. It is the bytecode-side
// peer of internal/buildsurface.Build.
//
// Returns a LoweringResult on success. On lowering errors the result
// is zero and a typed error explains the failure (callers can fall
// back to the legacy WASM path or report the issue to the author).
func LowerToBytecode(sp *ir.SurfaceProgram, cacheDir string) (LoweringResult, error) {
	if sp == nil {
		return LoweringResult{}, fmt.Errorf("surface.LowerToBytecode: nil SurfaceProgram")
	}
	if len(sp.SourceFiles) == 0 {
		return LoweringResult{}, fmt.Errorf("surface.LowerToBytecode: %s has no source files (Dir was empty during LowerEngineSurface)", sp.Name)
	}

	// Lower each .go file in the package, then merge their Programs.
	// Engine-surface packages today are single-file, but multi-file
	// packages are valid Go so the merge keeps that door open.
	var merged *program.Program
	for _, sf := range sp.SourceFiles {
		prog, err := golower.LowerFile(sf.Content)
		if err != nil {
			return LoweringResult{}, fmt.Errorf("lower %s: %w", sf.Path, err)
		}
		if merged == nil {
			merged = prog
			continue
		}
		mergePrograms(merged, prog)
	}

	// Surface kind: canvas-rooted surfaces are Canvas2D. The IR will
	// gain a Scene3D root variant when 3D engine surfaces ship; for now
	// every engine surface is Canvas2D since the runtime only knows
	// about canvas roots.
	merged.Name = sp.Name
	merged.Surface = program.SurfaceCanvas2D

	// Encode + hash.
	raw, err := json.Marshal(merged)
	if err != nil {
		return LoweringResult{}, fmt.Errorf("marshal program: %w", err)
	}
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("v%d:", loweringSchemaVersion)), raw...))
	hash := hex.EncodeToString(sum[:])[:16]

	jsonPath := filepath.Join(cacheDir, fmt.Sprintf("%s.%s.json", sp.Name, hash))
	if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
		return LoweringResult{}, fmt.Errorf("write bytecode: %w", err)
	}

	return LoweringResult{
		JSONPath:    jsonPath,
		JSONURL:     fmt.Sprintf("/gosx/engines/%s.%s.json", sp.Name, hash),
		Hash:        hash,
		SurfaceKind: merged.Surface,
	}, nil
}

// mergePrograms appends b's handlers/signals/exprs to a, rewriting b's
// expression IDs so they don't collide with a's. The Surface and Name
// fields stay on a.
//
// Because Handler.Body and SignalDef.Init reference ExprIDs, the
// rewrite walks both lists and adds a fixed offset.
func mergePrograms(a, b *program.Program) {
	offset := program.ExprID(len(a.Exprs))

	// Rewrite b's expression operand references.
	for i := range b.Exprs {
		for j := range b.Exprs[i].Operands {
			b.Exprs[i].Operands[j] += offset
		}
	}
	a.Exprs = append(a.Exprs, b.Exprs...)

	for _, sig := range b.Signals {
		sig.Init += offset
		a.Signals = append(a.Signals, sig)
	}
	for _, h := range b.Handlers {
		for i := range h.Body {
			h.Body[i] += offset
		}
		a.Handlers = append(a.Handlers, h)
	}
}
