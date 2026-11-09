// Package meshopt encodes glTF buffer views for EXT_meshopt_compression.
//
// It writes the two codecs the scene loader decodes: the vertex codec
// (version 0, mode ATTRIBUTES) and the triangle index codec (version 1, mode
// TRIANGLES). The formats are those of meshoptimizer. The encoders favor
// plain, predictable choices over its exhaustive search, so the output is
// somewhat larger than gltfpack's and always decodes with any conforming
// decoder. Decode functions are included to verify output in tests.
//
// Both encoders keep their input order: the vertex codec does not reorder
// vertices, and the index codec may rotate a triangle but never reorders
// triangles. Reorder for the GPU cache before encoding; assetpipe's
// optimize-mesh stage does.
package meshopt
