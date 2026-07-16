// Package signal aliases per ADR 0007 (surface event signal namespace).
//
// Pick events used to write into $scene.event.<field>. Phase 2 generalizes
// this to $surface.event.<field> so the same name set works for both
// Canvas2D and Scene3D surfaces. The migration is in-place: every existing
// $scene.event.<field> is registered here as a READ-ONLY alias for the
// corresponding $surface.event.<field>. Renderer writes go to
// $surface.event.* only.
//
// Consumers that have not migrated keep working transparently: a name lookup
// for $scene.event.X is rewritten to $surface.event.X before any storage
// access. The alias is sunsetted (file deleted, lookups removed) when no
// consumer references $scene.event.* across tracked repos — see the ADR's
// grep gate.

package signal

import (
	"strings"
	"sync"
)

// aliasTable is the canonical map: legacy name → target name. Populated at
// init time with the ADR 0007 field list. Public callers go through
// ResolveAlias and AliasedFrom; do not mutate the table at runtime.
var (
	aliasMu      sync.RWMutex
	aliasTable   = map[string]string{}
	reverseMu    sync.RWMutex
	reverseAlias = map[string][]string{}
)

// surfaceEventFields enumerates the pick-event fields that originally lived
// under $scene.event.* and now live under $surface.event.*. The list mirrors
// pushPickToSignals in client/wasm/render_full.go — keep it in sync when
// fields are added or removed.
//
// New fields go DIRECTLY into $surface.event.* with no $scene.event.* alias
// (per ADR 0007 "New surfaces use $surface.event.* unconditionally").
var surfaceEventFields = []string{
	"pointerX",
	"pointerY",
	"type",
	"targetIndex",
	"targetID",
	"targetInstanceIndex",
	"targetPrimitiveIndex",
	"targetTriangleIndex",
	"worldX",
	"worldY",
	"worldZ",
	"localX",
	"localY",
	"localZ",
	"uvX",
	"uvY",
	"depth",
	"rayOriginX",
	"rayOriginY",
	"rayOriginZ",
	"rayDirX",
	"rayDirY",
	"rayDirZ",
	"revision",
	"hovered",
	"hoverIndex",
	"hoverID",
	"down",
	"downIndex",
	"downID",
	"selected",
	"selectedIndex",
	"selectedID",
	"clickCount",
	// Phase 2 additions — Canvas2D-specific. These have no $scene.event.* alias.
	"dropTargetID",
	"marqueeStartX",
	"marqueeStartY",
	"marqueeEndX",
	"marqueeEndY",
	// Slice 3 — multi-select. selectedIDs is the comma-joined set a marquee
	// produces; selectedID stays the primary (first). No $scene.event.* alias.
	"selectedIDs",
}

func init() {
	for _, field := range surfaceEventFields {
		target := "$surface.event." + field
		legacy := "$scene.event." + field
		// Some fields are Canvas2D-only (no Scene3D legacy). Track the alias
		// for every field that DID exist under $scene.event.* per ADR 0007.
		if isLegacySceneEventField(field) {
			aliasTable[legacy] = target
			reverseAlias[target] = append(reverseAlias[target], legacy)
		}
	}
}

// legacySceneEventFields enumerates the subset of surfaceEventFields that
// existed under $scene.event.* in Phase 1 and therefore need a legacy alias.
// The post-Phase-2 fields (dropTargetID, marquee*) are absent here — they
// have no legacy callers and skip the alias entirely.
var legacySceneEventFields = map[string]bool{
	"pointerX":             true,
	"pointerY":             true,
	"type":                 true,
	"targetIndex":          true,
	"targetID":             true,
	"targetInstanceIndex":  true,
	"targetPrimitiveIndex": true,
	"targetTriangleIndex":  true,
	"worldX":               true,
	"worldY":               true,
	"worldZ":               true,
	"localX":               true,
	"localY":               true,
	"localZ":               true,
	"uvX":                  true,
	"uvY":                  true,
	"depth":                true,
	"revision":             true,
	"hovered":              true,
	"hoverIndex":           true,
	"hoverID":              true,
	"down":                 true,
	"downIndex":            true,
	"downID":               true,
	"selected":             true,
	"selectedIndex":        true,
	"selectedID":           true,
	"clickCount":           true,
}

func isLegacySceneEventField(field string) bool {
	return legacySceneEventFields[field]
}

// ResolveAlias returns the canonical signal name for name. If name is a known
// alias (e.g. "$scene.event.selectedID"), it returns the target
// ("$surface.event.selectedID"). For non-aliased names, returns name unchanged.
//
// Use this at every Store.Get / Store.Set / Store.Signal boundary so reads
// and writes converge on the target name. Writers should not invoke this
// for new code paths — new code writes directly to $surface.event.*.
func ResolveAlias(name string) string {
	aliasMu.RLock()
	defer aliasMu.RUnlock()
	if target, ok := aliasTable[name]; ok {
		return target
	}
	return name
}

// IsAlias reports whether name is registered as a legacy alias for some
// target. Used by tests and the alias-sunset grep gate.
func IsAlias(name string) bool {
	aliasMu.RLock()
	defer aliasMu.RUnlock()
	_, ok := aliasTable[name]
	return ok
}

// AliasesOf returns every legacy alias that resolves to target, or nil if
// target has no aliases. Useful for callers that need to broadcast a change
// notification to legacy subscribers.
//
// The returned slice is a copy — safe to retain.
func AliasesOf(target string) []string {
	reverseMu.RLock()
	defer reverseMu.RUnlock()
	if aliases := reverseAlias[target]; len(aliases) > 0 {
		out := make([]string, len(aliases))
		copy(out, aliases)
		return out
	}
	return nil
}

// LegacySceneEventNames returns the full list of $scene.event.* names that
// have $surface.event.* targets. Used by tests and by the bridge's store
// initialization to pre-register aliases on first use.
func LegacySceneEventNames() []string {
	out := make([]string, 0, len(legacySceneEventFields))
	for field := range legacySceneEventFields {
		out = append(out, "$scene.event."+field)
	}
	return out
}

// SurfaceEventNames returns the full list of $surface.event.* signals the
// renderer writes. Useful for tests asserting field coverage.
func SurfaceEventNames() []string {
	out := make([]string, 0, len(surfaceEventFields))
	for _, field := range surfaceEventFields {
		out = append(out, "$surface.event."+field)
	}
	return out
}

// IsSurfaceEventName reports whether name belongs to the $surface.event.*
// namespace. Used by callers that need to special-case event signals.
func IsSurfaceEventName(name string) bool {
	return strings.HasPrefix(name, "$surface.event.")
}

// IsSceneEventName reports whether name belongs to the legacy $scene.event.*
// namespace. Use sparingly — new code should call IsSurfaceEventName.
func IsSceneEventName(name string) bool {
	return strings.HasPrefix(name, "$scene.event.")
}
