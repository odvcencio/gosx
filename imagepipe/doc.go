// Package imagepipe probes, resizes, and encodes the responsive image
// variants gosx build writes to buildmanifest.Manifest.Images. It is a
// build-only package: cmd/gosx imports it to generate WebP (and same-format
// fallback) variants for every image under a project's public/ directory.
//
// server must never import this package, or its encoder dependency
// (github.com/gen2brain/webp), directly. Every gosx app imports package
// server, and the WebP encoder adds roughly 5 MB to a linked binary (see
// the CHANGELOG entry for issue #200 for the measured delta) -- paying that
// cost in every deployed app for a build-time-only feature would be a
// regression the other direction. TestServerPackageTreeNeverImportsImagepipe
// (repo root, imagepipe_isolation_test.go) enforces the boundary with a `go
// list -json` check over package server's own direct imports.
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
//   - Encode writes a resized image out as WebP (github.com/gen2brain/webp,
//     pinned to v0.5.5 -- see the CHANGELOG entry for why v0.6.x is not
//     safe to build with today), JPEG, or PNG.
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
