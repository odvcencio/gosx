//go:build gosx_tiny_islands_only

package bridge

// islandsOnlyBuild is true in the islands-only build (scene3d + canvas2d
// stripped from the WASM artifact). Paired with build_tag_full.go.
const islandsOnlyBuild = true
