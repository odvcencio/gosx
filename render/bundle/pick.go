package bundle

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu"
)

// PickResult is the renderer-facing structured result for a queued pick.
// ID 0 means background. Native GPU readback resolves object/instance identity
// from the ID buffer; primitive meshes also get CPU-side ray reconstruction for
// triangle, UV, world position, local position, and depth.
type PickResult struct {
	ID             uint32
	ObjectID       string
	ObjectIndex    int
	InstanceIndex  int
	PrimitiveIndex int
	TriangleIndex  int
	LocalPosition  [3]float32
	WorldPosition  [3]float32
	UV             [2]float32
	Depth          float32
	// RayOrigin and RayDirection are the world-space click ray computed from
	// the live bundle camera at pick time. They are set for hits AND
	// background clicks so editors can run exact CPU confirmation against
	// the same ray the GPU picker saw.
	RayOrigin    [3]float32
	RayDirection [3]float32
}

// PickCallback receives the numeric object ID under the requested pixel. ID 0
// means the cursor was over background (no pickable surface).
type PickCallback func(id uint32)

// PickResultCallback receives a structured pick result.
type PickResultCallback func(result PickResult)

// pickRowAlignment is WebGPU's minimum bytesPerRow alignment for
// copyTextureToBuffer. The spec guarantees 256.
const pickRowAlignment = 256

// pickRequest tracks one queued pick from QueuePick until its staging
// buffer has been copied to + read back.
type pickRequest struct {
	x, y        int
	cb          PickResultCallback
	targets     *pickTargets
	staging     gpu.Buffer
	inFlight    bool
	submitFrame bool // flagged on the frame we enqueued the copy
}

// QueuePick schedules a one-pixel readback from the id buffer at the given
// window coordinates. The callback runs on the frame AFTER the read-back
// buffer is available — typically 1–2 frames of latency. Only one pick may
// be in flight at a time; subsequent calls replace the pending request.
func (r *Renderer) QueuePick(x, y int, cb PickCallback) {
	if cb == nil {
		return
	}
	r.QueuePickResult(x, y, func(result PickResult) {
		cb(result.ID)
	})
}

// QueuePickResult schedules a one-pixel readback and returns structured
// target metadata when the result is available.
func (r *Renderer) QueuePickResult(x, y int, cb PickResultCallback) {
	if cb == nil {
		return
	}
	r.pickMu.Lock()
	defer r.pickMu.Unlock()
	if r.pendingPick != nil && r.pendingPick.staging != nil {
		// Drop the existing request — its callback never fires. The caller
		// should only keep the most recent pick anyway (mouse hover etc.).
		// If the copy has already been submitted, keep the request around for
		// cleanup readback so the staging buffer is not destroyed while the GPU
		// or a map/read goroutine still owns it.
		if r.pendingPick.inFlight {
			// The readback goroutine owns cleanup from here.
		} else if r.pendingPick.submitFrame {
			r.retiredPicks = append(r.retiredPicks, r.pendingPick)
		} else {
			r.pendingPick.staging.Destroy()
		}
	}
	r.pendingPick = &pickRequest{x: x, y: y, cb: cb}
}

// recordPickCopy, if a pick is queued and hasn't been submitted yet,
// allocates a staging buffer and records a 1×1 texture→buffer copy from
// the id buffer at the pick coordinates. Called between the main pass and
// the present pass so the id buffer has just been written.
func (r *Renderer) recordPickCopy(enc gpu.CommandEncoder, b engine.RenderBundle, surfaceWidth, surfaceHeight int) {
	r.pickMu.Lock()
	req := r.pendingPick
	if req == nil || req.submitFrame {
		r.pickMu.Unlock()
		return
	}
	r.pickMu.Unlock()
	if req.x < 0 || req.x >= surfaceWidth || req.y < 0 || req.y >= surfaceHeight {
		// Out-of-bounds coordinates — synthesize a background hit.
		r.pickMu.Lock()
		current := r.pendingPick == req
		if current {
			r.pendingPick = nil
		}
		r.pickMu.Unlock()
		if current {
			req.cb(backgroundPickResult())
		}
		return
	}

	staging, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  pickRowAlignment, // 256 bytes — smallest valid copy target
		Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst,
		Label: "bundle.pick.staging",
	})
	if err != nil {
		// Staging allocation failed. Treat as background and drop.
		r.pickMu.Lock()
		current := r.pendingPick == req
		if current {
			r.pendingPick = nil
		}
		r.pickMu.Unlock()
		if current {
			req.cb(backgroundPickResult())
		}
		return
	}
	r.pickMu.Lock()
	if r.pendingPick != req || req.submitFrame {
		r.pickMu.Unlock()
		staging.Destroy()
		return
	}
	req.staging = staging
	req.targets = r.pickTargetsForRequest(b, req.x, req.y, surfaceWidth, surfaceHeight)
	req.submitFrame = true
	r.pickMu.Unlock()

	enc.CopyTextureToBuffer(
		gpu.TextureCopyInfo{Texture: r.idBufferTex, Origin: [3]int{req.x, req.y, 0}},
		gpu.BufferCopyInfo{Buffer: staging, BytesPerRow: pickRowAlignment, RowsPerImage: 1},
		1, 1, 1,
	)
}

// finishPickReadback, if the queued pick has been submitted to the GPU,
// kicks off an async readback in a dedicated goroutine. The goroutine
// blocks on ReadAsync (WebGPU mapAsync), decodes the u32, fires the
// callback, and disposes the staging buffer.
func (r *Renderer) finishPickReadback() {
	r.pickMu.Lock()
	var starts []pickReadbackStart
	if start, ok := markPickReadbackLocked(r.pendingPick, true); ok {
		starts = append(starts, start)
	}
	if len(r.retiredPicks) > 0 {
		retired := r.retiredPicks
		r.retiredPicks = nil
		for _, req := range retired {
			if start, ok := markPickReadbackLocked(req, false); ok {
				starts = append(starts, start)
			} else if req != nil && req.staging != nil && !req.inFlight {
				req.staging.Destroy()
			}
		}
	}
	r.pickMu.Unlock()

	for _, start := range starts {
		r.finishPickReadbackAsync(start)
	}
}

type pickReadbackStart struct {
	req      *pickRequest
	staging  gpu.Buffer
	cb       PickResultCallback
	targets  *pickTargets
	callback bool
}

func markPickReadbackLocked(req *pickRequest, callback bool) (pickReadbackStart, bool) {
	if req == nil || !req.submitFrame || req.inFlight || req.staging == nil {
		return pickReadbackStart{}, false
	}
	req.inFlight = true
	return pickReadbackStart{req: req, staging: req.staging, cb: req.cb, targets: req.targets, callback: callback}, true
}

func (r *Renderer) finishPickReadbackAsync(start pickReadbackStart) {
	go func() {
		data, err := start.staging.ReadAsync(4) // 4 bytes = one u32
		defer start.staging.Destroy()
		id := uint32(0)
		if err == nil {
			id = binary.LittleEndian.Uint32(data)
		}
		if !start.callback {
			return
		}
		r.pickMu.Lock()
		current := r.pendingPick == start.req
		if current {
			r.pendingPick = nil
		}
		r.pickMu.Unlock()
		if !current {
			return
		}
		start.cb(start.targets.resultForID(id))
	}()
}

func backgroundPickResult() PickResult {
	return PickResult{
		ObjectIndex:    -1,
		InstanceIndex:  -1,
		PrimitiveIndex: -1,
		TriangleIndex:  -1,
	}
}

// pickSpanKind names the bundle collection that owns a run of pick IDs.
type pickSpanKind uint8

const (
	pickSpanInstanced pickSpanKind = iota
	pickSpanObject
	pickSpanSurface
)

// pickSpan maps a contiguous run of pick IDs back to the bundle entry that
// owns them. One span covers every instance of an InstancedMesh; object and
// surface spans cover a single ID.
//
// Spans replace the per-instance map the renderer used to rebuild on every
// frame. A scene with 100k instances needs 1 span per mesh instead of 100k map
// entries, and Frame builds nothing at all until a pick is queued.
type pickSpan struct {
	base     uint32
	count    uint32
	objectID string
	index    int
	kind     pickSpanKind
}

// contains reports whether id falls inside the span.
func (s pickSpan) contains(id uint32) bool {
	return s.base != 0 && id >= s.base && id-s.base < s.count
}

// result returns the identity-only PickResult for an ID inside the span.
func (s pickSpan) result(id uint32) PickResult {
	out := PickResult{
		ID:             id,
		ObjectID:       s.objectID,
		ObjectIndex:    s.index,
		InstanceIndex:  -1,
		PrimitiveIndex: -1,
		TriangleIndex:  -1,
	}
	if s.kind == pickSpanInstanced {
		out.InstanceIndex = int(id - s.base)
	}
	return out
}

// pickTargets is the snapshot one queued pick needs to turn the ID the GPU
// returns into a PickResult. spans resolve identity for any ID. hits hold the
// extra geometry — triangle, UV, local and world position, depth — for the few
// instances the click ray actually crosses.
type pickTargets struct {
	spans []pickSpan
	hits  map[uint32]PickResult
	ray   pickRay
}

// resultForID resolves one GPU-returned ID against the snapshot.
func (t *pickTargets) resultForID(id uint32) PickResult {
	if t == nil {
		return pickResultForID(nil, id)
	}
	if id == 0 {
		background := backgroundPickResult()
		background.RayOrigin = t.ray.origin
		background.RayDirection = t.ray.dir
		return background
	}
	if hit, ok := t.hits[id]; ok {
		return hit
	}
	for _, span := range t.spans {
		if !span.contains(id) {
			continue
		}
		out := span.result(id)
		out.RayOrigin = t.ray.origin
		out.RayDirection = t.ray.dir
		return out
	}
	out := pickResultForID(nil, id)
	out.RayOrigin = t.ray.origin
	out.RayDirection = t.ray.dir
	return out
}

// pickResultForID resolves an ID with no snapshot available. It reports the
// numeric fallback identity so a pick still returns something usable when the
// bundle changed under the readback.
func pickResultForID(hits map[uint32]PickResult, id uint32) PickResult {
	if id == 0 {
		if background, ok := hits[0]; ok {
			return background
		}
		return backgroundPickResult()
	}
	if result, ok := hits[id]; ok {
		return result
	}
	result := backgroundPickResult()
	result.ID = id
	result.ObjectIndex = int(id) - 1
	return result
}

func renderObjectPickable(object engine.RenderObject) bool {
	if object.Pickable != nil && !*object.Pickable {
		return false
	}
	return true
}

// updatePickSpans recomputes the pick ID assignment for the current bundle.
// It writes into Renderer-owned slices that grow one way, so a steady-state
// frame allocates nothing. It deliberately builds no per-instance results —
// pickTargetsForRequest does that, and only when a pick is queued.
func (r *Renderer) updatePickSpans(b engine.RenderBundle) {
	r.pickSpans = r.pickSpans[:0]
	r.pickBases = resetUint32s(r.pickBases, len(b.InstancedMeshes))
	r.objectPickBases = resetUint32s(r.objectPickBases, len(b.Objects))
	r.surfacePickBases = resetUint32s(r.surfacePickBases, len(b.Surfaces))

	nextID := uint32(1)
	for i := range b.InstancedMeshes {
		mesh := &b.InstancedMeshes[i]
		if mesh.InstanceCount <= 0 {
			continue
		}
		r.pickBases[i] = nextID
		r.pickSpans = append(r.pickSpans, pickSpan{
			base: nextID, count: uint32(mesh.InstanceCount),
			objectID: mesh.ID, index: i, kind: pickSpanInstanced,
		})
		nextID += uint32(mesh.InstanceCount)
		if nextID == 0 {
			return
		}
	}
	for i := range b.Objects {
		object := &b.Objects[i]
		if object.VertexCount <= 0 || !renderObjectPickable(*object) {
			continue
		}
		r.objectPickBases[i] = nextID
		r.pickSpans = append(r.pickSpans, pickSpan{
			base: nextID, count: 1,
			objectID: object.ID, index: i, kind: pickSpanObject,
		})
		nextID++
		if nextID == 0 {
			return
		}
	}
	for i := range b.Surfaces {
		if !surfaceDrawable(b.Surfaces[i]) {
			continue
		}
		r.surfacePickBases[i] = nextID
		r.pickSpans = append(r.pickSpans, pickSpan{
			base: nextID, count: 1,
			objectID: b.Surfaces[i].ID, index: i, kind: pickSpanSurface,
		})
		nextID++
		if nextID == 0 {
			return
		}
	}
}

// resetUint32s returns a zeroed slice of length n, reusing dst when it fits.
func resetUint32s(dst []uint32, n int) []uint32 {
	if cap(dst) < n {
		return make([]uint32, n)
	}
	dst = dst[:n]
	for i := range dst {
		dst[i] = 0
	}
	return dst
}

// pickTargetsForRequest snapshots what the readback needs for one queued pick.
// It copies the span table, computes the click ray, and raycasts the scene once
// to collect geometry for the instances the ray crosses. Only crossed instances
// enter the hits map, so the map stays small no matter how many instances the
// scene draws.
func (r *Renderer) pickTargetsForRequest(b engine.RenderBundle, x, y, width, height int) *pickTargets {
	targets := &pickTargets{ray: pickRayForCamera(b.Camera, x, y, width, height)}
	if len(r.pickSpans) > 0 {
		targets.spans = append(make([]pickSpan, 0, len(r.pickSpans)), r.pickSpans...)
	}
	if width <= 0 || height <= 0 {
		return targets
	}
	collectPickHits(targets, b, r.pickBases, r.objectPickBases, r.surfacePickBases)
	return targets
}

func (r *Renderer) pickBaseForMesh(index int) uint32 {
	if index < 0 || index >= len(r.pickBases) {
		return 0
	}
	return r.pickBases[index]
}

func (r *Renderer) pickBaseForObject(index int) uint32 {
	if index < 0 || index >= len(r.objectPickBases) {
		return 0
	}
	return r.objectPickBases[index]
}

func (r *Renderer) pickBaseForSurface(index int) uint32 {
	if index < 0 || index >= len(r.surfacePickBases) {
		return 0
	}
	return r.surfacePickBases[index]
}

type pickRay struct {
	origin [3]float32
	dir    [3]float32
}

type primitiveHit struct {
	triangleIndex int
	localPosition [3]float32
	worldPosition [3]float32
	uv            [2]float32
	depth         float32
}

// collectPickHits raycasts the scene along the click ray and records geometry
// for every entry the ray crosses. Entries the ray misses stay out of the map:
// resultForID resolves their identity from the span table instead.
//
// Each instance first gets a bounding-sphere test that respects the scale baked
// into its transform. That rejects most instances with 4 multiplies instead of
// a full triangle walk.
func collectPickHits(targets *pickTargets, b engine.RenderBundle, bases, objectBases, surfaceBases []uint32) {
	ray := targets.ray
	addHit := func(span pickSpan, id uint32, hit primitiveHit) {
		out := span.result(id)
		out.PrimitiveIndex = hit.triangleIndex
		out.TriangleIndex = hit.triangleIndex
		out.LocalPosition = hit.localPosition
		out.WorldPosition = hit.worldPosition
		out.UV = hit.uv
		out.Depth = hit.depth
		out.RayOrigin = ray.origin
		out.RayDirection = ray.dir
		if targets.hits == nil {
			targets.hits = make(map[uint32]PickResult)
		}
		targets.hits[id] = out
	}
	for objectIndex := range b.InstancedMeshes {
		mesh := &b.InstancedMeshes[objectIndex]
		if objectIndex >= len(bases) || bases[objectIndex] == 0 || mesh.InstanceCount <= 0 {
			continue
		}
		params := normalizePrimitiveParams(primitiveParamsForInstancedMesh(*mesh))
		geo := primitiveForParams(params)
		if geo == nil || geo.vertexCount <= 0 {
			continue
		}
		baseRadius := primitiveCullRadius(params)
		span := pickSpan{
			base: bases[objectIndex], count: uint32(mesh.InstanceCount),
			objectID: mesh.ID, index: objectIndex, kind: pickSpanInstanced,
		}
		for instanceIndex := 0; instanceIndex < mesh.InstanceCount; instanceIndex++ {
			model := matrixForInstance(mesh.Transforms, instanceIndex)
			if !raySphereIntersects(ray, model, instanceCullRadius(baseRadius, model)) {
				continue
			}
			if hit, ok := raycastPrimitiveInstance(ray, geo, model); ok {
				addHit(span, span.base+uint32(instanceIndex), hit)
			}
		}
	}
	for objectIndex := range b.Objects {
		object := &b.Objects[objectIndex]
		if objectIndex >= len(objectBases) || objectBases[objectIndex] == 0 {
			continue
		}
		if !nativeObjectDrawable(b, *object) || !renderObjectPickable(*object) {
			continue
		}
		if hit, ok := raycastWorldObject(ray, b, *object); ok {
			addHit(pickSpan{
				base: objectBases[objectIndex], count: 1,
				objectID: object.ID, index: objectIndex, kind: pickSpanObject,
			}, objectBases[objectIndex], hit)
		}
	}
	for surfaceIndex := range b.Surfaces {
		surface := &b.Surfaces[surfaceIndex]
		if surfaceIndex >= len(surfaceBases) || surfaceBases[surfaceIndex] == 0 || !surfaceDrawable(*surface) {
			continue
		}
		if hit, ok := raycastSurface(ray, *surface); ok {
			addHit(pickSpan{
				base: surfaceBases[surfaceIndex], count: 1,
				objectID: surface.ID, index: surfaceIndex, kind: pickSpanSurface,
			}, surfaceBases[surfaceIndex], hit)
		}
	}
}

// raySphereIntersects reports whether the ray crosses the bounding sphere
// centred on the transform's translation column with the given radius. It is
// the cheap reject in front of the triangle walk.
func raySphereIntersects(ray pickRay, model mat4, radius float32) bool {
	cx, cy, cz := model[12], model[13], model[14]
	ox := cx - ray.origin[0]
	oy := cy - ray.origin[1]
	oz := cz - ray.origin[2]
	// Project the centre onto the ray, then measure the perpendicular distance.
	t := ox*ray.dir[0] + oy*ray.dir[1] + oz*ray.dir[2]
	if t < -radius {
		return false
	}
	px := ox - ray.dir[0]*t
	py := oy - ray.dir[1]*t
	pz := oz - ray.dir[2]*t
	return px*px+py*py+pz*pz <= radius*radius
}

func pickRayForCamera(cam engine.RenderCamera, x, y, width, height int) pickRay {
	nxNorm := (float32(x)+0.5)/float32(max(1, width))*2 - 1
	nyNorm := 1 - (float32(y)+0.5)/float32(max(1, height))*2
	if cam.Mode == "orthographic" {
		zoom := float32(cam.Zoom)
		if zoom <= 0 {
			zoom = 1
		}
		halfWidth := float32(cam.Right-cam.Left) / 2 / zoom
		halfHeight := float32(cam.Top-cam.Bottom) / 2 / zoom
		if halfWidth <= 0 {
			halfWidth = 1
		}
		if halfHeight <= 0 {
			halfHeight = 1
		}
		invRot := mat4Mul(mat4RotateY(-float32(cam.RotationY)), mat4RotateX(-float32(cam.RotationX)))
		offset := transformVector(invRot, [3]float32{nxNorm * halfWidth, nyNorm * halfHeight, 0})
		forward := transformVector(invRot, [3]float32{0, 0, -1})
		forward = normalize3(forward[0], forward[1], forward[2])
		return pickRay{
			origin: [3]float32{float32(cam.X) + offset[0], float32(cam.Y) + offset[1], float32(cam.Z) + offset[2]},
			dir:    forward,
		}
	}
	fov := float32(cam.FOV)
	if fov <= 0 {
		fov = float32(math.Pi / 3)
	}
	aspect := float32(1)
	if height > 0 {
		aspect = float32(width) / float32(height)
	}
	tanHalf := float32(math.Tan(float64(fov) / 2))
	dir := [3]float32{nxNorm * aspect * tanHalf, nyNorm * tanHalf, -1}
	invRot := mat4Mul(mat4RotateY(-float32(cam.RotationY)), mat4RotateX(-float32(cam.RotationX)))
	dir = transformVector(invRot, dir)
	dir = normalize3(dir[0], dir[1], dir[2])
	return pickRay{
		origin: [3]float32{float32(cam.X), float32(cam.Y), float32(cam.Z)},
		dir:    dir,
	}
}

func matrixForInstance(transforms []float64, instanceIndex int) mat4 {
	if instanceIndex < 0 || len(transforms) < (instanceIndex+1)*16 {
		return mat4Identity()
	}
	var m mat4
	offset := instanceIndex * 16
	for i := 0; i < 16; i++ {
		m[i] = float32(transforms[offset+i])
	}
	return m
}

func raycastPrimitiveInstance(ray pickRay, geo *primitiveGeometry, model mat4) (primitiveHit, bool) {
	var best primitiveHit
	best.depth = float32(math.Inf(1))
	found := false
	for tri := 0; tri+2 < geo.vertexCount; tri += 3 {
		lp0 := primitivePositionAt(geo, tri)
		lp1 := primitivePositionAt(geo, tri+1)
		lp2 := primitivePositionAt(geo, tri+2)
		wp0 := transformPoint(model, lp0)
		wp1 := transformPoint(model, lp1)
		wp2 := transformPoint(model, lp2)
		dist, u, v, ok := rayIntersectsTriangle(ray.origin, ray.dir, wp0, wp1, wp2)
		if !ok || dist >= best.depth {
			continue
		}
		w := 1 - u - v
		best = primitiveHit{
			triangleIndex: tri / 3,
			localPosition: barycentric3(lp0, lp1, lp2, w, u, v),
			worldPosition: [3]float32{
				ray.origin[0] + ray.dir[0]*dist,
				ray.origin[1] + ray.dir[1]*dist,
				ray.origin[2] + ray.dir[2]*dist,
			},
			uv:    barycentric2(primitiveUVAt(geo, tri), primitiveUVAt(geo, tri+1), primitiveUVAt(geo, tri+2), w, u, v),
			depth: dist,
		}
		found = true
	}
	return best, found
}

func rayIntersectsTriangle(origin, dir, v0, v1, v2 [3]float32) (float32, float32, float32, bool) {
	const eps = float32(1e-7)
	edge1 := sub3(v1, v0)
	edge2 := sub3(v2, v0)
	h := cross3(dir, edge2)
	a := dot3(edge1, h)
	if a > -eps && a < eps {
		return 0, 0, 0, false
	}
	f := 1 / a
	s := sub3(origin, v0)
	u := f * dot3(s, h)
	if u < 0 || u > 1 {
		return 0, 0, 0, false
	}
	q := cross3(s, edge1)
	v := f * dot3(dir, q)
	if v < 0 || u+v > 1 {
		return 0, 0, 0, false
	}
	t := f * dot3(edge2, q)
	if t <= eps {
		return 0, 0, 0, false
	}
	return t, u, v, true
}

func primitivePositionAt(geo *primitiveGeometry, index int) [3]float32 {
	offset := index * 3
	if geo == nil || offset+2 >= len(geo.positions) {
		return [3]float32{}
	}
	return [3]float32{geo.positions[offset], geo.positions[offset+1], geo.positions[offset+2]}
}

func primitiveUVAt(geo *primitiveGeometry, index int) [2]float32 {
	offset := index * 2
	if geo == nil || offset+1 >= len(geo.uvs) {
		return [2]float32{}
	}
	return [2]float32{geo.uvs[offset], geo.uvs[offset+1]}
}

func transformPoint(m mat4, p [3]float32) [3]float32 {
	return [3]float32{
		m[0]*p[0] + m[4]*p[1] + m[8]*p[2] + m[12],
		m[1]*p[0] + m[5]*p[1] + m[9]*p[2] + m[13],
		m[2]*p[0] + m[6]*p[1] + m[10]*p[2] + m[14],
	}
}

func transformVector(m mat4, p [3]float32) [3]float32 {
	return [3]float32{
		m[0]*p[0] + m[4]*p[1] + m[8]*p[2],
		m[1]*p[0] + m[5]*p[1] + m[9]*p[2],
		m[2]*p[0] + m[6]*p[1] + m[10]*p[2],
	}
}

func barycentric3(a, b, c [3]float32, wa, wb, wc float32) [3]float32 {
	return [3]float32{
		a[0]*wa + b[0]*wb + c[0]*wc,
		a[1]*wa + b[1]*wb + c[1]*wc,
		a[2]*wa + b[2]*wb + c[2]*wc,
	}
}

func barycentric2(a, b, c [2]float32, wa, wb, wc float32) [2]float32 {
	return [2]float32{
		a[0]*wa + b[0]*wb + c[0]*wc,
		a[1]*wa + b[1]*wb + c[1]*wc,
	}
}

func sub3(a, b [3]float32) [3]float32 {
	return [3]float32{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func dot3(a, b [3]float32) float32 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func cross3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

// pickState holds synchronous access to the single in-flight pick request.
type pickState struct {
	mu          sync.Mutex
	pendingPick *pickRequest
}

// describePick is a helper for diagnostics; kept public to the package so
// test harnesses can inspect pending state without reaching into the mutex.
func (r *Renderer) describePick() string {
	r.pickMu.Lock()
	defer r.pickMu.Unlock()
	if r.pendingPick == nil {
		return "none"
	}
	return fmt.Sprintf("pending@(%d,%d) submitted=%v inFlight=%v",
		r.pendingPick.x, r.pendingPick.y,
		r.pendingPick.submitFrame, r.pendingPick.inFlight)
}
