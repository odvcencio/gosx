// Package imagepipe probes, resizes, and encodes the responsive image
// variants gosx build writes to buildmanifest.Manifest.Images. It is a
// build-only package: cmd/gosx imports it to generate same-format variants
// for every image under a project's public/ directory.
//
// WebP output uses m31labs.dev/tqwebp, a pure-Go VP8 encoder. It brings no
// foreign wasm runtime (tetratelabs/wazero) or FFI shim (ebitengine/purego)
// back into the module graph; TestModuleGraphExcludesForeignRuntimes at the
// repo root enforces that boundary. RegisterEncoder can still override WebP
// for compatibility, or add another output format. WebP input continues to
// decode through the pure-Go golang.org/x/image/webp decoder.
//
// server must never import this package directly. Every gosx app imports
// package server, and package imagepipe is a build-time-only feature. The
// runtime optimizer imports tqwebp directly, but it does not pull this
// pipeline or any custom registered Encoder into deployed applications.
// TestServerPackageTreeNeverImportsImagepipe (repo root,
// imagepipe_isolation_test.go) enforces the boundary with a `go list -json`
// check over package server's own direct imports.
//
// The package has three stages, each independently usable:
//
//   - Probe/Decode read a source image's intrinsic dimensions (and, for
//     Decode, its full pixel data) via the standard image.DecodeConfig /
//     image.Decode entry points. Blank-importing golang.org/x/image/webp
//     alongside the standard library's image/jpeg, image/png, and
//     image/gif registers WebP sources too, so a project that already
//     ships WebP originals probes and resizes them like any other format.
//   - Resize scales a decoded image down to a target width with
//     golang.org/x/image/draw's Catmull-Rom resampler -- the same
//     resampler server/image.go's request-time optimizer uses -- and
//     refuses to upscale.
//   - Encode writes a resized image out as WebP, JPEG, or PNG (all built in),
//     or any other registered Encoder's format -- see RegisterEncoder.
//
// Ladder computes the responsive width rungs gosx build generates for one
// source image (AutoImageWidths values, capped at the source's own
// intrinsic width -- see server.AutoImageWidths, which cmd/gosx's build
// stage supplies as Ladder's candidate list). Process ties every stage
// together for one source path: probe once, then resize and encode at
// every requested width and format.
//
// This package performs no disk writes of the images it produces. The
// caller (cmd/gosx) content-hashes and writes each Variant.Data itself,
// through the same writeHashedWithoutCompressedSidecars helper it already
// uses for every other build output, so a variant's on-disk name only ever
// depends on its own bytes.
package imagepipe
