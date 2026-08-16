package server

import (
	"log"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
)

// warnStaleIslands logs one warning per island whose dist/ program is stale
// relative to its current .gsx source (issue #166: editing an island and
// rebuilding only the Go binary — not `gosx build` — leaves dist/ serving an
// old program while SSR markup reflects the new source).
//
// Runs at most once per App (guarded by staleIslandsOnce) and costs nothing
// beyond the sync.Once check when no build manifest is loaded.
//
// Two things have to line up for this check to fire on real staleness and
// stay silent everywhere else: finding build.json at all, and resolving
// IslandAsset.SourceFile against the right directory. See
// loadStaleCheckManifest and resolveIslandSourceRoot.
//
// Never fails startup: every branch here only logs or returns.
func (a *App) warnStaleIslands() {
	if a == nil {
		return
	}
	a.staleIslandsOnce.Do(func() {
		runtimeRoot := a.effectiveRuntimeRoot()
		if runtimeRoot == "" {
			return
		}
		manifest, manifestDir, ok := a.loadStaleCheckManifest(runtimeRoot)
		if !ok || manifest == nil {
			return
		}
		sourceRoot := resolveIslandSourceRoot(manifest, manifestDir, runtimeRoot)
		if sourceRoot == "" {
			return
		}
		for _, stale := range manifest.StaleIslands(sourceRoot) {
			log.Printf("gosx islands: %s's source file changed since gosx build (%s); run gosx build", stale.Name, stale.SourceFile)
		}
	})
}

// loadStaleCheckManifest finds build.json under runtimeRoot, trying the two
// locations a GoSX deployment may load it from — the same two candidates
// island.loadDefaultBuildManifest tries for the runtime asset renderer:
//
//  1. runtimeRoot/build.json — production, where SetRuntimeRoot (or its
//     ResolveAppRoot("") fallback via GOSX_APP_ROOT) points straight at the
//     dist/ output that run.sh stages GOSX_APP_ROOT to.
//  2. runtimeRoot/dist/build.json — runtimeRoot is a project root (a
//     dev/example server's SetRuntimeRoot(root), or `go build` executed and
//     run from the project root after `gosx build` populated dist/ —
//     issue #166's exact repro).
//
// Returns the manifest and the directory it was actually found in, so the
// caller can tell which of the two layouts matched.
func (a *App) loadStaleCheckManifest(runtimeRoot string) (*buildmanifest.Manifest, string, bool) {
	if manifest, ok := a.runtimeBuildManifest(runtimeRoot); ok {
		return manifest, runtimeRoot, true
	}
	distRoot := filepath.Join(runtimeRoot, "dist")
	if manifest, ok := a.runtimeBuildManifest(distRoot); ok {
		return manifest, distRoot, true
	}
	return nil, "", false
}

// resolveIslandSourceRoot picks the directory to resolve IslandAsset.
// SourceFile paths against, in precedence order:
//
//  1. manifest.SourceRoot — the project root `gosx build` recorded at build
//     time — if that directory still exists on this host. Most direct: a
//     dev machine (or CI job) running the server from the same checkout
//     that produced the build doesn't need any layout guessing.
//  2. manifestDir's parent ("dist/.."), only when build.json was actually
//     found nested under runtimeRoot/dist (manifestDir != runtimeRoot).
//     That parent is the project root — issue #166's project-root-as-
//     server-root layout, where live .gsx sources sit beside dist/, not
//     inside it. When build.json was found directly at runtimeRoot instead,
//     there is no "dist" nesting to walk up out of, so this tier does not
//     apply — using it anyway would walk one directory too far.
//  3. runtimeRoot itself. The production fallback: dist/app is only ever
//     the build-time snapshot `gosx build` staged there (stageDeployment
//     Bundle copies app/ into dist/app), so hashing that snapshot against
//     itself always matches and the check stays silent — correctly, since
//     a production image commonly ships dist/ without the app's live
//     sources to compare against.
//
// A candidate that turns out to be wrong for a given island (its SourceFile
// doesn't resolve to a readable file there) still resolves safely: Manifest.
// StaleIslands skips any island whose SourceFile can't be read under the
// chosen root instead of reporting it stale.
func resolveIslandSourceRoot(manifest *buildmanifest.Manifest, manifestDir, runtimeRoot string) string {
	if manifest != nil {
		if root := strings.TrimSpace(manifest.SourceRoot); root != "" && isDir(root) {
			return root
		}
	}
	if manifestDir != runtimeRoot {
		if parent := filepath.Dir(manifestDir); parent != "" && parent != manifestDir && isDir(parent) {
			return parent
		}
	}
	return runtimeRoot
}
