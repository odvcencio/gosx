//go:build !gosx_tiny_islands_only

package bridge

// islandsOnlyBuild is false in the full build (scene3d + canvas2d available).
// Paired with build_tag_islands.go for the islands-only build flavor. Used
// by tests that need to skip when a surface is unavailable instead of
// failing.
const islandsOnlyBuild = false
