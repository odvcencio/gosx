package bundle

import (
	"encoding/binary"
	"math"
	"strconv"

	"m31labs.dev/gosx/engine"
)

// meshFrameState caches every value Frame needs for one InstancedMesh draw
// slot. The Renderer keeps one entry per slot and reuses it across frames.
// A slot whose geometry parameters do not change keeps its cache key, its
// primitive buffers, its material, and its cull buffers. A static scene
// therefore formats no strings and allocates nothing per frame.
//
// Frame fills the slice once through prepareMeshStates, then the cull pass,
// the shadow passes, and the main pass all read from it. That replaces the
// four to five key rebuilds per mesh per frame the renderer used to do.
type meshFrameState struct {
	// params holds the normalized primitive parameters. Comparing it against
	// the incoming parameters tells prepareMeshStates whether the cached key
	// and the cached resources still apply.
	params  primitiveParams
	key     string
	haveKey bool

	prim *primitiveResources
	cull *cullResources
	mat  *materialResources

	fp     materialFingerprint
	haveFP bool

	// radius is the unscaled bounding-sphere radius of the primitive. The cull
	// shader multiplies it by each instance's conservative Frobenius bound.
	radius        float32
	pickBase      uint32
	instanceCount int
	vertexCount   int
	skinned       bool
	castShadow    bool
	// drawable is true when the slot has instances, transforms, and geometry.
	drawable bool
}

// prepareMeshStates refreshes the per-slot cache for every InstancedMesh in
// the bundle. It creates primitive buffers, material bind groups, and cull
// buffers on first sight and reuses them afterwards. Call it before the first
// render pass: ensureMaterial writes a uniform buffer, and a queue write
// inside an open pass is illegal.
func (r *Renderer) prepareMeshStates(b engine.RenderBundle) error {
	meshes := b.InstancedMeshes
	if cap(r.meshStates) < len(meshes) {
		grown := make([]meshFrameState, len(meshes))
		copy(grown, r.meshStates)
		r.meshStates = grown
	}
	r.meshStates = r.meshStates[:len(meshes)]
	r.materialFPs.reset(len(b.Materials))
	for i := range meshes {
		im := &meshes[i]
		st := &r.meshStates[i]
		st.instanceCount = im.InstanceCount
		st.skinned = isSkinnedMesh(*im)
		st.castShadow = im.CastShadow
		st.drawable = false
		st.pickBase = r.pickBaseForMesh(i)
		if im.InstanceCount <= 0 || len(im.Transforms) == 0 {
			continue
		}
		params := normalizePrimitiveParams(primitiveParamsForInstancedMesh(*im))
		if !st.haveKey || st.params != params {
			st.params = params
			st.key = instancedMeshKeyForParams(i, params)
			st.radius = primitiveCullRadius(params)
			st.prim = nil
			st.cull = nil
			st.haveKey = true
		}
		if st.key == "" {
			continue
		}
		if st.prim == nil {
			prim, err := r.ensurePrimitive(params)
			if err != nil {
				return err
			}
			st.prim = prim
		}
		if st.prim == nil || st.prim.vertexCount == 0 {
			continue
		}
		st.vertexCount = st.prim.vertexCount
		fp := r.materialFPs.lookup(b.Materials, im.MaterialIndex)
		if !st.haveFP || st.mat == nil || st.fp != fp {
			mat, err := r.ensureMaterial(fp)
			if err != nil {
				return err
			}
			st.fp = fp
			st.mat = mat
			st.haveFP = true
		}
		if st.cull == nil || st.cull.capacity < im.InstanceCount ||
			(st.castShadow && st.cull.casters[0] == nil) {
			cull, err := r.ensureCullResources(st.key, im.InstanceCount, st.castShadow)
			if err != nil {
				return err
			}
			st.cull = cull
		}
		st.drawable = true
	}
	return nil
}

// instancedMeshKeyForParams composes the cull/skin cache key for one draw slot
// from its already-normalized primitive parameters. It avoids fmt.Sprintf so
// the key costs one string concat instead of a reflection-driven format.
func instancedMeshKeyForParams(idx int, params primitiveParams) string {
	key := primitiveCacheKey(params)
	if key == "" {
		return ""
	}
	return strconv.Itoa(idx) + ":" + key
}

// instanceTransformHash fingerprints the instance payload of one mesh.
// recordCullPass compares the fingerprint against the value it uploaded last
// and skips both the re-encode and the buffer upload when they match. The hash
// reads the source float64 values directly, so it costs less than the encode
// plus upload it replaces. Static instances therefore cost no upload at all.
//
// The mix is FNV-1a over 64-bit words. Word-at-a-time mixing is weaker than
// byte-at-a-time, but change detection only needs the chance of a collision to
// be negligible, and it is.
//
// Four accumulators run in parallel. One accumulator serialises on the multiply
// latency and made the hash the hottest function in the frame; four independent
// chains let the processor overlap them and cut the cost roughly fourfold.
func instanceTransformHash(transforms []float64, instanceCount int, pickBase uint32) uint64 {
	const (
		fnvOffset64 = uint64(14695981039346656037)
		fnvPrime64  = uint64(1099511628211)
	)
	h0 := (fnvOffset64 ^ uint64(instanceCount)) * fnvPrime64
	h1 := (fnvOffset64 ^ uint64(pickBase)) * fnvPrime64
	h2 := (fnvOffset64 ^ uint64(len(transforms))) * fnvPrime64
	h3 := fnvOffset64
	need := instanceCount * 16
	if need > len(transforms) {
		need = len(transforms)
	}
	src := transforms[:need]
	i := 0
	for ; i+4 <= len(src); i += 4 {
		lane := src[i : i+4 : i+4]
		h0 = (h0 ^ math.Float64bits(lane[0])) * fnvPrime64
		h1 = (h1 ^ math.Float64bits(lane[1])) * fnvPrime64
		h2 = (h2 ^ math.Float64bits(lane[2])) * fnvPrime64
		h3 = (h3 ^ math.Float64bits(lane[3])) * fnvPrime64
	}
	for ; i < len(src); i++ {
		h0 = (h0 ^ math.Float64bits(src[i])) * fnvPrime64
	}
	return (((h0*fnvPrime64^h1)*fnvPrime64^h2)*fnvPrime64 ^ h3) * fnvPrime64
}

// instanceScratch returns a Renderer-owned byte buffer of at least size bytes.
// Frame encodes instance records into it and hands it straight to WriteBuffer,
// which copies. The buffer grows one way, so steady-state frames allocate
// nothing.
func (r *Renderer) instanceScratch(size int) []byte {
	if cap(r.instanceEncodeScratch) < size {
		r.instanceEncodeScratch = make([]byte, size+size/4)
	}
	return r.instanceEncodeScratch[:size]
}

// instanceRecordInto packs one instance record per transform into dst. The
// first 64 bytes of each record are the column-major model matrix consumed by
// the render and culling pipelines; the trailing vec4<u32> carries the stable
// pick ID. dst must hold instanceCount*instanceRecordStride bytes.
func instanceRecordInto(dst []byte, transforms []float64, instanceCount int, pickBase uint32) {
	for inst := 0; inst < instanceCount; inst++ {
		record := dst[inst*instanceRecordStride : (inst+1)*instanceRecordStride]
		src := inst * 16
		if src+16 <= len(transforms) {
			for j, v := range transforms[src : src+16] {
				binary.LittleEndian.PutUint32(record[j*4:], math.Float32bits(float32(v)))
			}
		} else {
			for j := 0; j < 16; j++ {
				value := float64(0)
				if idx := src + j; idx < len(transforms) {
					value = transforms[idx]
				}
				binary.LittleEndian.PutUint32(record[j*4:], math.Float32bits(float32(value)))
			}
		}
		if pickBase != 0 {
			binary.LittleEndian.PutUint32(record[64:68], pickBase+uint32(inst))
		} else {
			binary.LittleEndian.PutUint32(record[64:68], 0)
		}
		binary.LittleEndian.PutUint32(record[68:72], 0)
		binary.LittleEndian.PutUint32(record[72:76], 0)
		binary.LittleEndian.PutUint32(record[76:80], 0)
	}
}

// putFloat32s writes src as little-endian float32 words into dst. dst must
// hold len(src)*4 bytes.
func putFloat32s(dst []byte, src []float32) {
	for i, f := range src {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(f))
	}
}
