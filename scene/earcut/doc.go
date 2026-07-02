// Package earcut is a faithful Go port of mapbox/earcut, the canonical
// ear-clipping polygon triangulation library (https://github.com/mapbox/earcut).
//
// The port tracks the algorithm structure of upstream tag v2.2.4: a circular
// doubly linked list of ring vertices, ear clipping with an optional z-order
// curve hash for large inputs, hole elimination via bridging (David Eberly's
// algorithm), and fallback passes (collinear-point filtering, local
// self-intersection curing, and polygon splitting) for degenerate inputs.
// Internal function names mirror the JS source in unexported Go camelCase
// (linkedList, filterPoints, earcutLinked, isEar/isEarHashed,
// cureLocalIntersections, splitEarcut, eliminateHoles/eliminateHole/
// findHoleBridge, indexCurve/sortLinked/zOrder, and so on) so the two
// implementations can be diffed function-by-function.
//
// The package has zero dependencies beyond the Go standard library (math and
// sort) and no cgo, so it is WASM-portable like the rest of GoSX's scene
// package.
//
// # Third-party attribution
//
// Ported from mapbox/earcut, tag v2.2.4 (src/earcut.js), which is
// distributed under the ISC License:
//
//	ISC License
//
//	Copyright (c) 2016, Mapbox
//
//	Permission to use, copy, modify, and/or distribute this software for any
//	purpose with or without fee is hereby granted, provided that the above
//	copyright notice and this permission notice appear in all copies.
//
//	THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
//	WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
//	MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
//	ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
//	WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
//	ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF OR
//	IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
package earcut
