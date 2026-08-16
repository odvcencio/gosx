package server

import "log"

// warnStaleIslands logs one warning per island whose dist/ program is stale
// relative to its current .gsx source (issue #166: editing an island and
// rebuilding only the Go binary — not `gosx build` — leaves dist/ serving an
// old program while SSR markup reflects the new source).
//
// Runs at most once per App (guarded by staleIslandsOnce) and costs nothing
// beyond the sync.Once check when no build manifest is loaded: it reuses the
// same cached manifest read (runtimeBuildManifest) the runtime asset server
// already performs, and buildmanifest.Manifest.StaleIslands skips any island
// whose manifest entry predates the SourceFile/SourceHash fields or whose
// source file cannot be read (a production image may ship dist/ without the
// app's .gsx sources — that is not an error).
//
// Never fails startup: every branch here only logs or returns.
func (a *App) warnStaleIslands() {
	if a == nil {
		return
	}
	a.staleIslandsOnce.Do(func() {
		root := a.effectiveRuntimeRoot()
		if root == "" {
			return
		}
		manifest, ok := a.runtimeBuildManifest(root)
		if !ok || manifest == nil {
			return
		}
		for _, name := range manifest.StaleIslands(root) {
			log.Printf("gosx islands: %s program in dist is stale (source changed since gosx build); run gosx build", name)
		}
	})
}
