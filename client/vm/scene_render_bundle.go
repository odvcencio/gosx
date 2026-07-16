package vm

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
)

func buildRenderBundle(props map[string]any, nodes []resolvedNode, width, height int, timeSeconds float64, spinSc *spinScratch) rootengine.RenderBundle {
	return buildRenderBundleCached(props, nodes, width, height, timeSeconds, spinSc, nil)
}

// bakedSegment is the camera-INDEPENDENT result of transforming one local-space
// edge into WORLD space: world endpoints, world normals, and the lit RGBA at each
// endpoint. None of these depend on the camera (the camera is applied later in the
// clip/project step), so for a static object whose props + lighting are unchanged
// they are identical frame-to-frame and can be served from the world-bake cache.
type bakedSegment struct {
	worldFrom point3
	worldTo   point3
	fromRGBA  [4]float64
	toRGBA    [4]float64
}

// objectWorldBake is a per-node cache entry holding the fully baked WORLD geometry
// for a static object. It is reused across frames (including across an orbiting
// camera) until the node's reconcile generation or the lighting/material signature
// changes. The segments slice and bounds are treated as READ-ONLY by every reader
// (the emit step only reads them to clip/project + append copies into the bundle),
// so sharing the cached slice across frames is safe.
type objectWorldBake struct {
	generation uint64
	litSig     uint64
	segments   []bakedSegment
	bounds     rootengine.RenderBounds
	hasBounds  bool
}

// worldBakeStore threads the SceneAdapter's per-node world-bake cache into
// buildRenderBundle without coupling the bundle builder to the adapter type. It
// holds the node-index keyed cache, the per-node generation slice (the
// invalidation signal), and hit/miss counters for test assertions.
type worldBakeStore struct {
	cache  map[int]*objectWorldBake
	gen    []uint64
	hits   *uint64
	misses *uint64
}

func (s *worldBakeStore) generationFor(nodeIndex int) (uint64, bool) {
	if s == nil || nodeIndex < 0 || nodeIndex >= len(s.gen) {
		return 0, false
	}
	return s.gen[nodeIndex], true
}

func (s *worldBakeStore) load(nodeIndex int) (*objectWorldBake, bool) {
	if s == nil || s.cache == nil {
		return nil, false
	}
	entry, ok := s.cache[nodeIndex]
	return entry, ok
}

func (s *worldBakeStore) store(nodeIndex int, entry *objectWorldBake) {
	if s == nil || s.cache == nil {
		return
	}
	s.cache[nodeIndex] = entry
}

func (s *worldBakeStore) recordHit() {
	if s != nil && s.hits != nil {
		*s.hits++
	}
}

func (s *worldBakeStore) recordMiss() {
	if s != nil && s.misses != nil {
		*s.misses++
	}
}

func buildRenderBundleCached(props map[string]any, nodes []resolvedNode, width, height int, timeSeconds float64, spinSc *spinScratch, bakeStore *worldBakeStore) rootengine.RenderBundle {
	if width <= 0 {
		width = 720
	}
	if height <= 0 {
		height = 420
	}
	postEffects, diagnostics := nativeRenderPostEffects(props)
	animations := nativeRenderAnimations(props)

	bundle := rootengine.RenderBundle{
		Background:      sceneBackground(props),
		Materials:       []rootengine.RenderMaterial{},
		Objects:         []rootengine.RenderObject{},
		Surfaces:        []rootengine.RenderSurface{},
		Lights:          []rootengine.RenderLight{},
		Lines:           []rootengine.RenderLine{},
		Labels:          []rootengine.RenderLabel{},
		Sprites:         []rootengine.RenderSprite{},
		Positions:       []float64{},
		Colors:          []float64{},
		WorldPositions:  []float64{},
		WorldColors:     []float64{},
		Animations:      animations,
		PostEffects:     postEffects,
		PostFXMaxPixels: int(math.Max(0, math.Floor(numberFromAny(sceneValue(props, "postFXMaxPixels"), numberFromAny(propValue(props, "postFXMaxPixels"), 0))))),
		Diagnostics:     diagnostics,
	}

	camera := sceneCameraFromProps(props)
	lights := sceneLightsFromProps(props)
	sprites := sceneSpritesFromProps(props)
	objects := make([]sceneObject, 0, len(nodes))
	// objectNodeIndex[k] is the stable program-node index of objects[k]; it keys
	// the per-node world-bake cache. Kept parallel (instead of wrapping each large
	// sceneObject in a struct) so the per-object loop copies the sceneObject exactly
	// once, matching the pre-cache hot path's copy cost.
	objectNodeIndex := make([]int, 0, len(nodes))
	labels := make([]sceneLabel, 0, len(nodes))
	for index, node := range nodes {
		switch strings.TrimSpace(strings.ToLower(node.Kind)) {
		case "camera":
			camera = normalizeSceneCameraMap(node.Props, camera)
		case "light":
			if light, ok := sceneLightFromResolvedNode(index, node); ok {
				lights = append(lights, light)
			}
		case "mesh":
			objects = append(objects, sceneObjectFromResolvedNode(index, node))
			objectNodeIndex = append(objectNodeIndex, index)
		case "label":
			if label, ok := sceneLabelFromResolvedNode(index, node); ok {
				labels = append(labels, label)
			}
		case "sprite":
			if sprite, ok := sceneSpriteFromResolvedNode(index, node); ok {
				sprites = append(sprites, sprite)
			}
		}
	}
	environment := resolveSceneEnvironment(props, len(lights) > 0)
	bundle.Camera = rootengine.RenderCamera{
		Mode:      cameraModeForKind(camera.Kind),
		X:         camera.X,
		Y:         camera.Y,
		Z:         camera.Z,
		RotationX: camera.RotationX,
		RotationY: camera.RotationY,
		RotationZ: camera.RotationZ,
		FOV:       camera.FOV,
		Near:      camera.Near,
		Far:       camera.Far,
		Left:      camera.Left,
		Right:     camera.Right,
		Top:       camera.Top,
		Bottom:    camera.Bottom,
		Zoom:      camera.Zoom,
	}
	bundle.Environment = renderSceneEnvironment(environment)
	if len(lights) > 0 {
		bundle.Lights = renderSceneLights(lights)
	}
	appendSceneGrid(&bundle, width, height)
	// Camera rotation is constant for every vertex/corner in the frame; hoist
	// its inverse-rotation trig once and thread it through the per-object loops.
	camTrig := cameraInverseRotTrig(camera)
	// Pre-resolve all light and environment colors once per frame so
	// sceneLitColorRGBAResolved can skip the string→RGBA parse on every vertex.
	litCtx := buildLightingContext(lights, environment)
	// litSig fingerprints the lighting inputs (lights + environment). The cached
	// lit colors are a function of these, so a change must invalidate the bake even
	// when the object's own props are unchanged. The camera is NOT part of this.
	litSig := lightingSignature(litCtx, lights)
	// materialIndexByKey deduplicates materials by their identity Key so the
	// per-object lookup is O(1) instead of an O(materials) DeepEqual scan.
	materialIndexByKey := make(map[string]int, len(objects))
	for objectIdx := range objects {
		object := objects[objectIdx]
		vertexOffset := len(bundle.WorldPositions) / 3
		materialIndex := ensureRenderMaterial(&bundle, materialIndexByKey, object)
		material := bundle.Materials[materialIndex]
		appendResult := appendSceneObjectCached(&bundle, camera, width, height, &object, objectIdx, material, lights, environment, animations, timeSeconds, camTrig, litCtx, bakeStore, objectNodeIndex[objectIdx], litSig, spinSc)
		vertexCount := (len(bundle.WorldPositions) / 3) - vertexOffset
		if vertexCount > 0 || appendResult.HasBounds || appendResult.ViewCulled {
			bounds := appendResult.Bounds
			if !appendResult.HasBounds && vertexCount > 0 {
				bounds = renderObjectBounds(bundle.WorldPositions, vertexOffset, vertexCount)
			}
			depthNear, depthFar, depthCenter := renderBoundsDepthMetricsTrig(bounds, camera, camTrig)
			bundle.Objects = append(bundle.Objects, rootengine.RenderObject{
				ID:            object.ID,
				Kind:          object.Kind,
				Pickable:      object.Pickable,
				MaterialIndex: materialIndex,
				RenderPass:    bundle.Materials[materialIndex].RenderPass,
				VertexOffset:  vertexOffset,
				VertexCount:   vertexCount,
				Static:        object.Static,
				Bounds:        bounds,
				DepthNear:     depthNear,
				DepthFar:      depthFar,
				DepthCenter:   depthCenter,
				ViewCulled:    appendResult.ViewCulled,
			})
			// Pass the spinQ and clip TRS already computed in appendSceneObjectCached — no second evaluation.
			appendSceneSurface(&bundle, camera, width, height, object, materialIndex, material, bounds, appendResult.SpinQ, appendResult.ClipTRS, timeSeconds, camTrig)
		}
	}
	for _, label := range labels {
		appendSceneLabel(&bundle, camera, width, height, label, timeSeconds)
	}
	for _, sprite := range sprites {
		appendSceneSprite(&bundle, camera, width, height, sprite, timeSeconds)
	}
	bundle.ObjectCount = len(bundle.Objects)
	bundle.VertexCount = len(bundle.Positions) / 2
	bundle.WorldVertexCount = len(bundle.WorldPositions) / 3
	bundle.Passes = buildRenderPassBundles(bundle)
	return bundle
}

func appendSceneGrid(bundle *rootengine.RenderBundle, width, height int) {
	for x := 0; x <= width; x += 48 {
		appendSceneLine(bundle, width, height, rootengine.RenderPoint{X: float64(x), Y: 0}, rootengine.RenderPoint{X: float64(x), Y: float64(height)}, "rgba(141, 225, 255, 0.14)", 1)
	}
	for y := 0; y <= height; y += 48 {
		appendSceneLine(bundle, width, height, rootengine.RenderPoint{X: 0, Y: float64(y)}, rootengine.RenderPoint{X: float64(width), Y: float64(y)}, "rgba(141, 225, 255, 0.14)", 1)
	}
}

// appendSceneObject is a thin wrapper for callers that do not have a bake store
// (e.g. tests). It delegates to appendSceneObjectCached with a nil store.
func appendSceneObject(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, object sceneObject, objIndex int, material rootengine.RenderMaterial, lights []sceneLight, environment sceneEnvironment, animations []rootengine.RenderAnimation, timeSeconds float64, camTrig rotTrig, litCtx lightingContext, spinSc *spinScratch) sceneAppendResult {
	return appendSceneObjectCached(bundle, camera, width, height, &object, objIndex, material, lights, environment, animations, timeSeconds, camTrig, litCtx, nil, -1, 0, spinSc)
}

// appendSceneObjectCached bakes (or reuses a cached bake of) the object's WORLD
// geometry, then emits the camera-dependent clip/project for the current frame.
// The bake is camera-INDEPENDENT; when the object is cache-eligible and unchanged
// (same node generation + lighting signature) the bake step is skipped entirely
// and the emit step replays the cached segments. The emitted WorldPositions/
// WorldColors/bounds are bit-identical to a fresh bake — the cache only removes
// the transcendental rotation/normal/lighting math, never alters its result.
//
// object is passed by pointer to avoid copying the large sceneObject struct on the
// per-object hot path; it is treated as READ-ONLY (only deref-copied into the leaf
// transform helpers, exactly as the pre-cache path did).
func appendSceneObjectCached(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, object *sceneObject, objIndex int, material rootengine.RenderMaterial, lights []sceneLight, environment sceneEnvironment, animations []rootengine.RenderAnimation, timeSeconds float64, camTrig rotTrig, litCtx lightingContext, bakeStore *worldBakeStore, nodeIndex int, litSig uint64, spinSc *spinScratch) sceneAppendResult {
	aspect := math.Max(0.0001, float64(width)/math.Max(1, float64(height)))
	result := sceneAppendResult{}
	spinQ := spinQuatWithScratch(*object, timeSeconds, spinSc)
	result.SpinQ = spinQ
	clip := objectClipTRS(*object, objIndex, animations, timeSeconds, spinSc)
	result.ClipTRS = clip
	// Textured plane surfaces emit no world line segments; bake/cache only applies
	// to the line-geometry path, so handle the textured case using motion semantics.
	if !sceneObjectUsesLineGeometry(*object, material) && sceneObjectHasTexturedSurface(*object, material) {
		for _, corner := range scenePlaneSurfaceCorners(*object, spinQ, clip, timeSeconds) {
			result.Bounds, result.HasBounds = expandRenderBounds(result.Bounds, result.HasBounds, corner)
		}
		if result.HasBounds {
			result.ViewCulled = renderBoundsOutsideFrustumTrig(result.Bounds, camera, width, height, camTrig)
		}
		return result
	}

	// Static (cache-eligible) path: perf's trig bake/cache is bit-identical for
	// objects with no spin, no drift, AND no clip.
	if bakeStore != nil && nodeIndex >= 0 && sceneObjectBakeEligible(object) && clipIsEmpty(clip) {
		if gen, ok := bakeStore.generationFor(nodeIndex); ok {
			if entry, found := bakeStore.load(nodeIndex); found && entry.generation == gen && entry.litSig == litSig {
				bakeStore.recordHit()
				emitted := emitBakedSceneObject(bundle, camera, width, height, material, entry, camTrig)
				emitted.SpinQ = spinQ
				emitted.ClipTRS = clip
				return emitted
			}
			entry := bakeSceneObjectWorld(object, material, lights, timeSeconds, litCtx)
			entry.generation = gen
			entry.litSig = litSig
			bakeStore.store(nodeIndex, entry)
			bakeStore.recordMiss()
			emitted := emitBakedSceneObject(bundle, camera, width, height, material, entry, camTrig)
			emitted.SpinQ = spinQ
			emitted.ClipTRS = clip
			return emitted
		}
	}
	// Animated/streaming path: motion geometry + perf color/camera.
	baseRGBA := sceneColorRGBA(material.Color, [4]float64{0.55, 0.88, 1, 1})
	obj := *object
	for _, segment := range sceneObjectSegments(obj) {
		worldFrom := translatePoint(segment[0], obj, spinQ, clip, timeSeconds)
		worldTo := translatePoint(segment[1], obj, spinQ, clip, timeSeconds)
		fromNormal := sceneObjectWorldNormal(obj, segment[0], spinQ, clip)
		toNormal := sceneObjectWorldNormal(obj, segment[1], spinQ, clip)
		fromRGBA := sceneLitColorRGBAResolved(baseRGBA, material, worldFrom, fromNormal, lights, litCtx)
		toRGBA := sceneLitColorRGBAResolved(baseRGBA, material, worldTo, toNormal, lights, litCtx)
		result.Bounds, result.HasBounds = expandRenderBounds(result.Bounds, result.HasBounds, worldFrom)
		result.Bounds, result.HasBounds = expandRenderBounds(result.Bounds, result.HasBounds, worldTo)
		clippedFrom, clippedTo, ok := clipWorldSegmentForCameraTrig(worldFrom, worldTo, camera, aspect, camTrig)
		if !ok {
			continue
		}
		appendWorldSceneLine(bundle, clippedFrom, clippedTo, fromRGBA, toRGBA)
		from := projectPointTrig(clippedFrom, camera, width, height, camTrig)
		to := projectPointTrig(clippedTo, camera, width, height, camTrig)
		if from == nil || to == nil {
			continue
		}
		stroke := mixRGBA(fromRGBA, toRGBA)
		stroke[3] = clamp(stroke[3]*material.Opacity, 0, 1)
		appendSceneLine(bundle, width, height, *from, *to, sceneRGBAString(stroke), 1.8)
	}
	if result.HasBounds {
		result.ViewCulled = renderBoundsOutsideFrustumTrig(result.Bounds, camera, width, height, camTrig)
	}
	return result
}

// clipIsEmpty reports whether the clip has no active T, R, or S channel.
func clipIsEmpty(c clipTRS) bool { return !c.HasT && !c.HasR && !c.HasS }

// sceneObjectBakeEligible reports whether the object's WORLD geometry is constant
// across frames: no spin (rotation*time) and no drift (sinusoidal offset*time).
// Only such objects have camera-independent AND time-independent world positions,
// so only they are safe to cache across frames. Any object that gains spin or
// drift fails this check and is rebaked every frame.
func sceneObjectBakeEligible(object *sceneObject) bool {
	return object.SpinX == 0 && object.SpinY == 0 && object.SpinZ == 0 &&
		object.ShiftX == 0 && object.ShiftY == 0 && object.ShiftZ == 0
}

// bakeSceneObjectWorld computes the camera-INDEPENDENT world geometry for the
// object: per-segment world endpoints, lit RGBA, and the accumulated bounds. This
// is exactly the work the old per-frame loop did before the camera-dependent
// clip/project; the math (and its float ordering) is unchanged, so a cached bake
// reproduced here is bit-identical to a fresh one.
func bakeSceneObjectWorld(object *sceneObject, material rootengine.RenderMaterial, lights []sceneLight, timeSeconds float64, litCtx lightingContext) *objectWorldBake {
	obj := *object
	objTrig := sceneObjectRotTrig(obj, timeSeconds)
	baseRGBA := sceneColorRGBA(material.Color, [4]float64{0.55, 0.88, 1, 1})
	segments := sceneObjectSegments(obj)
	bake := &objectWorldBake{segments: make([]bakedSegment, 0, len(segments))}
	for _, segment := range segments {
		worldFrom := translatePointTrig(segment[0], obj, timeSeconds, objTrig)
		worldTo := translatePointTrig(segment[1], obj, timeSeconds, objTrig)
		fromNormal := sceneObjectWorldNormalTrig(obj, segment[0], objTrig)
		toNormal := sceneObjectWorldNormalTrig(obj, segment[1], objTrig)
		fromRGBA := sceneLitColorRGBAResolved(baseRGBA, material, worldFrom, fromNormal, lights, litCtx)
		toRGBA := sceneLitColorRGBAResolved(baseRGBA, material, worldTo, toNormal, lights, litCtx)
		bake.bounds, bake.hasBounds = expandRenderBounds(bake.bounds, bake.hasBounds, worldFrom)
		bake.bounds, bake.hasBounds = expandRenderBounds(bake.bounds, bake.hasBounds, worldTo)
		bake.segments = append(bake.segments, bakedSegment{
			worldFrom: worldFrom,
			worldTo:   worldTo,
			fromRGBA:  fromRGBA,
			toRGBA:    toRGBA,
		})
	}
	return bake
}

// emitBakedSceneObject applies the camera-dependent clip/project to each baked
// segment and appends the result into the bundle. The arithmetic per segment is
// identical to the original loop's tail, so the emitted bundle is bit-identical.
func emitBakedSceneObject(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, material rootengine.RenderMaterial, bake *objectWorldBake, camTrig rotTrig) sceneAppendResult {
	aspect := math.Max(0.0001, float64(width)/math.Max(1, float64(height)))
	result := sceneAppendResult{Bounds: bake.bounds, HasBounds: bake.hasBounds}
	for i := range bake.segments {
		seg := &bake.segments[i]
		clippedFrom, clippedTo, ok := clipWorldSegmentForCameraTrig(seg.worldFrom, seg.worldTo, camera, aspect, camTrig)
		if !ok {
			continue
		}
		appendWorldSceneLine(bundle, clippedFrom, clippedTo, seg.fromRGBA, seg.toRGBA)
		from := projectPointTrig(clippedFrom, camera, width, height, camTrig)
		to := projectPointTrig(clippedTo, camera, width, height, camTrig)
		if from == nil || to == nil {
			continue
		}
		stroke := mixRGBA(seg.fromRGBA, seg.toRGBA)
		stroke[3] = clamp(stroke[3]*material.Opacity, 0, 1)
		appendSceneLine(bundle, width, height, *from, *to, sceneRGBAString(stroke), 1.8)
	}
	if result.HasBounds {
		result.ViewCulled = renderBoundsOutsideFrustumTrig(result.Bounds, camera, width, height, camTrig)
	}
	return result
}

// lightingSignature fingerprints the per-frame lighting inputs that the baked lit
// colors depend on. If it changes, a cached bake's colors are stale and must be
// recomputed. The camera is deliberately excluded — world bakes are camera-free.
//
// It hashes inline with FNV-1a over a stack-local accumulator (no heap hasher) so
// it adds no per-frame allocation to either the static or the dynamic path.
func lightingSignature(ctx lightingContext, lights []sceneLight) uint64 {
	const (
		fnvOffset uint64 = 14695981039346656037
		fnvPrime  uint64 = 1099511628211
	)
	h := fnvOffset
	writeByte := func(b byte) {
		h ^= uint64(b)
		h *= fnvPrime
	}
	writeF := func(v float64) {
		bits := math.Float64bits(v)
		for i := 0; i < 8; i++ {
			writeByte(byte(bits >> (8 * i)))
		}
	}
	writeStr := func(s string) {
		for i := 0; i < len(s); i++ {
			writeByte(s[i])
		}
	}
	if !ctx.active {
		writeF(math.NaN()) // distinct, stable signature for "lighting inactive"
		return h
	}
	writeF(1)
	writeF(ctx.ambientColor.X)
	writeF(ctx.ambientColor.Y)
	writeF(ctx.ambientColor.Z)
	writeF(ctx.skyColor.X)
	writeF(ctx.skyColor.Y)
	writeF(ctx.skyColor.Z)
	writeF(ctx.groundColor.X)
	writeF(ctx.groundColor.Y)
	writeF(ctx.groundColor.Z)
	writeF(ctx.exposure)
	writeF(ctx.ambientInt)
	writeF(ctx.skyInt)
	writeF(ctx.groundInt)
	for i, light := range lights {
		writeStr(light.Kind)
		writeF(light.Intensity)
		writeF(light.X)
		writeF(light.Y)
		writeF(light.Z)
		writeF(light.DirectionX)
		writeF(light.DirectionY)
		writeF(light.DirectionZ)
		writeF(light.Range)
		writeF(light.Decay)
		c := ctx.lightColors[i]
		writeF(c.X)
		writeF(c.Y)
		writeF(c.Z)
	}
	return h
}

func sceneObjectUsesLineGeometry(object sceneObject, material rootengine.RenderMaterial) bool {
	return !(sceneObjectHasTexturedSurface(object, material) && !material.Wireframe)
}

func appendSceneSurface(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, object sceneObject, materialIndex int, material rootengine.RenderMaterial, bounds rootengine.RenderBounds, spinQ motion.Quat, clip clipTRS, timeSeconds float64, camTrig rotTrig) {
	if !sceneObjectHasTexturedSurface(object, material) {
		return
	}
	// spinQ and clip TRS were already computed in appendSceneObjectCached and threaded
	// here — no second evaluation.
	corners := scenePlaneSurfaceCorners(object, spinQ, clip, timeSeconds)
	if len(corners) != 4 {
		return
	}
	depthNear, depthFar, depthCenter := renderBoundsDepthMetricsTrig(bounds, camera, camTrig)
	bundle.Surfaces = append(bundle.Surfaces, rootengine.RenderSurface{
		ID:            object.ID,
		Kind:          object.Kind,
		MaterialIndex: materialIndex,
		RenderPass:    material.RenderPass,
		Static:        object.Static,
		Positions:     scenePlaneSurfacePositions(corners),
		UV:            scenePlaneSurfaceUVs(),
		VertexCount:   6,
		Bounds:        bounds,
		DepthNear:     depthNear,
		DepthFar:      depthFar,
		DepthCenter:   depthCenter,
		ViewCulled:    renderBoundsOutsideFrustum(bounds, camera, width, height),
	})
}

func sceneObjectHasTexturedSurface(object sceneObject, material rootengine.RenderMaterial) bool {
	return object.Kind == "plane" && strings.TrimSpace(material.Texture) != ""
}

func scenePlaneSurfaceCorners(object sceneObject, spinQ motion.Quat, clip clipTRS, timeSeconds float64) []point3 {
	vertices := cachedBoxVertices(object.Width, 0, object.Depth)
	if len(vertices) < 4 {
		return nil
	}
	return []point3{
		translatePoint(vertices[0], object, spinQ, clip, timeSeconds),
		translatePoint(vertices[1], object, spinQ, clip, timeSeconds),
		translatePoint(vertices[2], object, spinQ, clip, timeSeconds),
		translatePoint(vertices[3], object, spinQ, clip, timeSeconds),
	}
}

func scenePlaneSurfacePositions(corners []point3) []float64 {
	if len(corners) < 4 {
		return nil
	}
	return []float64{
		corners[0].X, corners[0].Y, corners[0].Z,
		corners[1].X, corners[1].Y, corners[1].Z,
		corners[2].X, corners[2].Y, corners[2].Z,
		corners[0].X, corners[0].Y, corners[0].Z,
		corners[2].X, corners[2].Y, corners[2].Z,
		corners[3].X, corners[3].Y, corners[3].Z,
	}
}

func scenePlaneSurfaceUVs() []float64 {
	return []float64{
		0, 1,
		1, 1,
		1, 0,
		0, 1,
		1, 0,
		0, 0,
	}
}

func appendSceneLabel(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, label sceneLabel, timeSeconds float64) {
	world := sceneLabelPoint(label, timeSeconds)
	position := projectPoint(world, camera, width, height)
	if position == nil {
		return
	}
	marginX := math.Max(24, label.MaxWidth)
	marginY := math.Max(24, label.LineHeight*2)
	if position.X < -marginX || position.X > float64(width)+marginX || position.Y < -marginY || position.Y > float64(height)+marginY {
		return
	}
	bundle.Labels = append(bundle.Labels, rootengine.RenderLabel{
		ID:          label.ID,
		Text:        label.Text,
		ClassName:   label.ClassName,
		Position:    *position,
		Depth:       cameraLocalPoint(world, camera).Z,
		Priority:    label.Priority,
		MaxWidth:    label.MaxWidth,
		MaxLines:    label.MaxLines,
		Overflow:    label.Overflow,
		Font:        label.Font,
		LineHeight:  label.LineHeight,
		Color:       label.Color,
		Background:  label.Background,
		BorderColor: label.BorderColor,
		OffsetX:     label.OffsetX,
		OffsetY:     label.OffsetY,
		AnchorX:     label.AnchorX,
		AnchorY:     label.AnchorY,
		Collision:   label.Collision,
		Occlude:     label.Occlude,
		WhiteSpace:  label.WhiteSpace,
		TextAlign:   label.TextAlign,
	})
}

func appendSceneSprite(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, sprite sceneSprite, timeSeconds float64) {
	point := sceneSpritePoint(sprite, timeSeconds)
	position := projectPoint(point, camera, width, height)
	if position == nil {
		return
	}
	depth := cameraLocalPoint(point, camera).Z
	screenWidth, screenHeight := projectedSceneSpriteSize(camera, width, height, sprite, depth)
	if screenWidth <= 0 || screenHeight <= 0 {
		return
	}
	marginX := math.Max(24, screenWidth)
	marginY := math.Max(24, screenHeight)
	if position.X < -marginX || position.X > float64(width)+marginX || position.Y < -marginY || position.Y > float64(height)+marginY {
		return
	}
	bundle.Sprites = append(bundle.Sprites, rootengine.RenderSprite{
		ID:        sprite.ID,
		Src:       sprite.Src,
		ClassName: sprite.ClassName,
		Position:  *position,
		Depth:     depth,
		Priority:  sprite.Priority,
		Width:     screenWidth,
		Height:    screenHeight,
		Opacity:   sprite.Opacity,
		OffsetX:   sprite.OffsetX,
		OffsetY:   sprite.OffsetY,
		AnchorX:   sprite.AnchorX,
		AnchorY:   sprite.AnchorY,
		Occlude:   sprite.Occlude,
		Fit:       normalizeSceneSpriteFit(sprite.Fit),
	})
}

func appendWorldSceneLine(bundle *rootengine.RenderBundle, from, to point3, fromRGBA, toRGBA [4]float64) {
	bundle.WorldPositions = append(bundle.WorldPositions,
		from.X, from.Y, from.Z,
		to.X, to.Y, to.Z,
	)
	bundle.WorldColors = append(bundle.WorldColors,
		fromRGBA[0], fromRGBA[1], fromRGBA[2], fromRGBA[3],
		toRGBA[0], toRGBA[1], toRGBA[2], toRGBA[3],
	)
}

func sceneLabelPoint(label sceneLabel, timeSeconds float64) point3 {
	offset := sceneLabelOffset(label, timeSeconds)
	return point3{
		X: label.X + offset.X,
		Y: label.Y + offset.Y,
		Z: label.Z + offset.Z,
	}
}

func sceneLabelOffset(label sceneLabel, timeSeconds float64) point3 {
	if label.ShiftX == 0 && label.ShiftY == 0 && label.ShiftZ == 0 {
		return point3{}
	}
	angle := label.DriftPhase + timeSeconds*label.DriftSpeed
	return point3{
		X: math.Cos(angle) * label.ShiftX,
		Y: math.Sin(angle*0.82+label.DriftPhase*0.35) * label.ShiftY,
		Z: math.Sin(angle) * label.ShiftZ,
	}
}

func sceneSpritePoint(sprite sceneSprite, timeSeconds float64) point3 {
	offset := sceneSpriteOffset(sprite, timeSeconds)
	return point3{
		X: sprite.X + offset.X,
		Y: sprite.Y + offset.Y,
		Z: sprite.Z + offset.Z,
	}
}

func sceneSpriteOffset(sprite sceneSprite, timeSeconds float64) point3 {
	if sprite.ShiftX == 0 && sprite.ShiftY == 0 && sprite.ShiftZ == 0 {
		return point3{}
	}
	angle := sprite.DriftPhase + timeSeconds*sprite.DriftSpeed
	return point3{
		X: math.Cos(angle) * sprite.ShiftX,
		Y: math.Sin(angle*0.82+sprite.DriftPhase*0.35) * sprite.ShiftY,
		Z: math.Sin(angle) * sprite.ShiftZ,
	}
}

func projectedSceneSpriteSize(camera sceneCamera, width, height int, sprite sceneSprite, depth float64) (float64, float64) {
	if depth <= 0 {
		return 0, 0
	}
	focal := (math.Min(float64(width), float64(height)) / 2) / math.Tan((camera.FOV*math.Pi)/360)
	scale := sprite.Scale
	if scale <= 0 {
		scale = 1
	}
	worldWidth := sprite.Width
	if worldWidth <= 0 {
		worldWidth = 1.25
	}
	worldHeight := sprite.Height
	if worldHeight <= 0 {
		worldHeight = worldWidth
	}
	return math.Max(1, (worldWidth*scale*focal)/depth), math.Max(1, (worldHeight*scale*focal)/depth)
}

// ensureRenderMaterial returns the index of a deduplicated material, appending a
// new one when needed. It keys on the material's identity Key (which already
// encodes every distinguishing field except the derived ShaderData) to avoid the
// per-object O(materials) reflect.DeepEqual scan the linear path used to run.
//
// renderMaterialEqual is retained as a tie-breaker: ShaderData is a pure function
// of fields the Key already encodes (Kind + Emissive, or the kind's registered
// profile), so a Key match implies a full match in practice — but verifying on the
// (rare) key hit and re-scanning on the (theoretical) key collision keeps the
// dedup result bit-identical to the previous linear scan.
func ensureRenderMaterial(bundle *rootengine.RenderBundle, indexByKey map[string]int, object sceneObject) int {
	profile := resolveRenderMaterial(object)
	if idx, ok := indexByKey[profile.Key]; ok {
		if renderMaterialEqual(bundle.Materials[idx], profile) {
			return idx
		}
		// Key collision with a non-equal material (not expected): fall back to a
		// full scan so the result still matches the old linear behavior exactly.
		for index, existing := range bundle.Materials {
			if renderMaterialEqual(existing, profile) {
				return index
			}
		}
	}
	bundle.Materials = append(bundle.Materials, profile)
	index := len(bundle.Materials) - 1
	if _, ok := indexByKey[profile.Key]; !ok {
		indexByKey[profile.Key] = index
	}
	return index
}

func renderMaterialEqual(left, right rootengine.RenderMaterial) bool {
	if left.Key != right.Key ||
		left.Kind != right.Kind ||
		left.Color != right.Color ||
		left.Texture != right.Texture ||
		left.Opacity != right.Opacity ||
		left.Wireframe != right.Wireframe ||
		left.BlendMode != right.BlendMode ||
		left.RenderPass != right.RenderPass ||
		left.Emissive != right.Emissive ||
		left.Roughness != right.Roughness ||
		left.Metalness != right.Metalness ||
		left.Clearcoat != right.Clearcoat ||
		left.Sheen != right.Sheen ||
		left.Transmission != right.Transmission ||
		left.Iridescence != right.Iridescence ||
		left.Anisotropy != right.Anisotropy ||
		left.NormalMap != right.NormalMap ||
		left.RoughnessMap != right.RoughnessMap ||
		left.MetalnessMap != right.MetalnessMap ||
		left.EmissiveMap != right.EmissiveMap ||
		left.CustomVertex != right.CustomVertex ||
		left.CustomFragment != right.CustomFragment ||
		left.CustomVertexWGSL != right.CustomVertexWGSL ||
		left.CustomFragmentWGSL != right.CustomFragmentWGSL ||
		left.ShaderBackend != right.ShaderBackend ||
		!reflect.DeepEqual(left.CustomUniforms, right.CustomUniforms) ||
		!reflect.DeepEqual(left.ShaderLayout, right.ShaderLayout) {
		return false
	}
	if len(left.ShaderData) != len(right.ShaderData) {
		return false
	}
	for i := range left.ShaderData {
		if left.ShaderData[i] != right.ShaderData[i] {
			return false
		}
	}
	return true
}

func resolveRenderMaterial(object sceneObject) rootengine.RenderMaterial {
	profile := rootengine.RenderMaterial{
		Kind:      stringFromAny(object.Material, "flat"),
		Color:     object.Color,
		Texture:   strings.TrimSpace(object.Texture),
		Opacity:   1,
		Wireframe: true,
		BlendMode: "opaque",
		Emissive:  0,
		Roughness: 0.55,
		Metalness: 0,
	}
	profile.CustomVertex = object.CustomVertex
	profile.CustomFragment = object.CustomFragment
	profile.CustomVertexWGSL = object.CustomVertexWGSL
	profile.CustomFragmentWGSL = object.CustomFragmentWGSL
	profile.CustomUniforms = cloneAnyMap(object.CustomUniforms)
	profile.ShaderBackend = object.ShaderBackend
	profile.ShaderLayout = cloneAnyMap(object.ShaderLayout)

	kindKey := strings.ToLower(strings.TrimSpace(profile.Kind))
	customProfile, hasCustomProfile := materialProfileForKind(kindKey)
	if hasCustomProfile {
		profile.Kind = kindKey
		if customProfile.HasOpacity {
			profile.Opacity = customProfile.Opacity
		}
		if customProfile.HasWireframe {
			profile.Wireframe = customProfile.Wireframe
		}
		if customProfile.HasBlendMode && strings.TrimSpace(customProfile.BlendMode) != "" {
			profile.BlendMode = customProfile.BlendMode
		}
		if customProfile.HasEmissive {
			profile.Emissive = customProfile.Emissive
		}
		if customProfile.HasRoughness {
			profile.Roughness = clamp(customProfile.Roughness, 0, 1)
		}
		if customProfile.HasMetalness {
			profile.Metalness = clamp(customProfile.Metalness, 0, 1)
		}
		if customProfile.HasClearcoat {
			profile.Clearcoat = clamp(customProfile.Clearcoat, 0, 1)
		}
		if customProfile.HasSheen {
			profile.Sheen = clamp(customProfile.Sheen, 0, 1)
		}
		if customProfile.HasTransmission {
			profile.Transmission = clamp(customProfile.Transmission, 0, 1)
		}
		if customProfile.HasIridescence {
			profile.Iridescence = clamp(customProfile.Iridescence, 0, 1)
		}
		if customProfile.HasAnisotropy {
			profile.Anisotropy = clamp(customProfile.Anisotropy, -1, 1)
		}
		if customProfile.HasNormalMap {
			profile.NormalMap = strings.TrimSpace(customProfile.NormalMap)
		}
		if customProfile.HasRoughnessMap {
			profile.RoughnessMap = strings.TrimSpace(customProfile.RoughnessMap)
		}
		if customProfile.HasMetalnessMap {
			profile.MetalnessMap = strings.TrimSpace(customProfile.MetalnessMap)
		}
		if customProfile.HasEmissiveMap {
			profile.EmissiveMap = strings.TrimSpace(customProfile.EmissiveMap)
		}
	}

	switch kindKey {
	case "ghost":
		if !hasCustomProfile {
			profile.Opacity = 0.42
			profile.BlendMode = "alpha"
			profile.Emissive = 0.12
		}
	case "glass":
		if !hasCustomProfile {
			profile.Opacity = 0.28
			profile.BlendMode = "alpha"
			profile.Emissive = 0.08
		}
	case "glow":
		if !hasCustomProfile {
			profile.Opacity = 0.92
			profile.BlendMode = "additive"
			profile.Emissive = 0.42
		}
	case "matte":
		if !hasCustomProfile {
			profile.Wireframe = true
		}
	case "flat":
	}

	if object.HasOpacity {
		profile.Opacity = object.Opacity
	}
	if object.HasWireframe {
		profile.Wireframe = object.Wireframe
	}
	if object.HasBlendMode && object.BlendMode != "" {
		profile.BlendMode = object.BlendMode
	}
	if object.HasEmissive {
		profile.Emissive = object.Emissive
	}
	if object.HasRoughness {
		profile.Roughness = object.Roughness
	}
	if object.HasMetalness {
		profile.Metalness = object.Metalness
	}
	if object.HasClearcoat {
		profile.Clearcoat = object.Clearcoat
	}
	if object.HasSheen {
		profile.Sheen = object.Sheen
	}
	if object.HasTransmission {
		profile.Transmission = object.Transmission
	}
	if object.HasIridescence {
		profile.Iridescence = object.Iridescence
	}
	if object.HasAnisotropy {
		profile.Anisotropy = object.Anisotropy
	}
	if object.HasTexture {
		profile.Texture = strings.TrimSpace(object.Texture)
	}
	if object.HasNormalMap {
		profile.NormalMap = strings.TrimSpace(object.NormalMap)
	}
	if object.HasRoughnessMap {
		profile.RoughnessMap = strings.TrimSpace(object.RoughnessMap)
	}
	if object.HasMetalnessMap {
		profile.MetalnessMap = strings.TrimSpace(object.MetalnessMap)
	}
	if object.HasEmissiveMap {
		profile.EmissiveMap = strings.TrimSpace(object.EmissiveMap)
	}
	if profile.Opacity < 0.999 && profile.BlendMode == "opaque" {
		profile.BlendMode = "alpha"
	}
	profile.RenderPass = renderPassFromMaterialProfile(profile)
	profile.Key = renderMaterialKey(profile)
	if hasCustomProfile && len(customProfile.ShaderData) >= 3 {
		profile.ShaderData = cloneMaterialShaderData(customProfile.ShaderData)
	} else {
		profile.ShaderData = renderMaterialShaderData(profile)
	}
	return profile
}

func renderPassFromMaterialProfile(profile rootengine.RenderMaterial) string {
	switch strings.ToLower(strings.TrimSpace(profile.BlendMode)) {
	case "additive":
		return "additive"
	case "alpha":
		return "alpha"
	}
	if profile.Opacity < 0.999 {
		return "alpha"
	}
	return "opaque"
}

func renderMaterialKey(profile rootengine.RenderMaterial) string {
	// Direct Builder writes avoid the fmt.Sprintf boxing/reflection walk
	// that this runs on every material profile (one per scene element).
	kind := strings.ToLower(strings.TrimSpace(profile.Kind))
	color := strings.TrimSpace(profile.Color)
	texture := strings.TrimSpace(profile.Texture)
	normalMap := strings.TrimSpace(profile.NormalMap)
	roughnessMap := strings.TrimSpace(profile.RoughnessMap)
	metalnessMap := strings.TrimSpace(profile.MetalnessMap)
	emissiveMap := strings.TrimSpace(profile.EmissiveMap)
	blendMode := strings.ToLower(strings.TrimSpace(profile.BlendMode))
	var b strings.Builder
	b.Grow(len(kind) + len(color) + len(texture) + len(normalMap) + len(roughnessMap) + len(metalnessMap) + len(emissiveMap) + len(blendMode) + len(profile.RenderPass) + 120)
	b.WriteString(kind)
	b.WriteByte('|')
	b.WriteString(color)
	b.WriteByte('|')
	b.WriteString(texture)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Roughness, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Metalness, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Clearcoat, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Sheen, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Transmission, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Iridescence, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Anisotropy, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(normalMap)
	b.WriteByte('|')
	b.WriteString(roughnessMap)
	b.WriteByte('|')
	b.WriteString(metalnessMap)
	b.WriteByte('|')
	b.WriteString(emissiveMap)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Opacity, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatBool(profile.Wireframe))
	b.WriteByte('|')
	b.WriteString(blendMode)
	b.WriteByte('|')
	b.WriteString(profile.RenderPass)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Emissive, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(profile.CustomVertex))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(profile.CustomFragment))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(profile.CustomVertexWGSL))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(profile.CustomFragmentWGSL))
	if len(profile.CustomUniforms) > 0 {
		b.WriteByte('|')
		b.WriteString(renderMaterialCustomUniformKey(profile.CustomUniforms))
	}
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(profile.ShaderBackend))
	if len(profile.ShaderLayout) > 0 {
		b.WriteByte('|')
		b.WriteString(renderMaterialCustomUniformKey(profile.ShaderLayout))
	}
	return b.String()
}

func renderMaterialCustomUniformKey(values map[string]any) string {
	if len(values) == 0 {
		return ""
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func renderMaterialShaderData(profile rootengine.RenderMaterial) []float64 {
	kind := strings.ToLower(strings.TrimSpace(profile.Kind))
	emissive := profile.Emissive
	switch kind {
	case "ghost":
		return []float64{1, emissive, 0.3}
	case "glass":
		return []float64{2, emissive, 0.7}
	case "glow":
		return []float64{3, emissive, 1}
	case "matte":
		return []float64{4, emissive, 0.2}
	default:
		return []float64{0, emissive, 1}
	}
}

func buildRenderPassBundles(bundle rootengine.RenderBundle) []rootengine.RenderPassBundle {
	passes := map[string]*rootengine.RenderPassBundle{
		"staticOpaque": {
			Name:      "staticOpaque",
			Blend:     "opaque",
			Depth:     "opaque",
			Static:    true,
			CacheKey:  renderStaticPassKey(bundle),
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
		"dynamicOpaque": {
			Name:      "dynamicOpaque",
			Blend:     "opaque",
			Depth:     "opaque",
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
		"alpha": {
			Name:      "alpha",
			Blend:     "alpha",
			Depth:     "translucent",
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
		"additive": {
			Name:      "additive",
			Blend:     "additive",
			Depth:     "translucent",
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
	}
	opaqueObjects := []rootengine.RenderObject{}
	alphaObjects := []rootengine.RenderObject{}
	additiveObjects := []rootengine.RenderObject{}

	for _, object := range bundle.Objects {
		switch renderPassBucketName(object) {
		case "alpha":
			alphaObjects = append(alphaObjects, object)
		case "additive":
			additiveObjects = append(additiveObjects, object)
		default:
			opaqueObjects = append(opaqueObjects, object)
		}
	}

	sort.SliceStable(alphaObjects, func(i, j int) bool {
		if alphaObjects[i].DepthCenter != alphaObjects[j].DepthCenter {
			return alphaObjects[i].DepthCenter > alphaObjects[j].DepthCenter
		}
		return alphaObjects[i].VertexOffset < alphaObjects[j].VertexOffset
	})
	sort.SliceStable(additiveObjects, func(i, j int) bool {
		if additiveObjects[i].DepthCenter != additiveObjects[j].DepthCenter {
			return additiveObjects[i].DepthCenter > additiveObjects[j].DepthCenter
		}
		return additiveObjects[i].VertexOffset < additiveObjects[j].VertexOffset
	})

	for _, object := range opaqueObjects {
		appendRenderPassObject(passes, bundle, object)
	}
	for _, object := range alphaObjects {
		appendRenderPassObject(passes, bundle, object)
	}
	for _, object := range additiveObjects {
		appendRenderPassObject(passes, bundle, object)
	}

	ordered := []rootengine.RenderPassBundle{
		finalizeRenderPassBundle(passes["staticOpaque"]),
		finalizeRenderPassBundle(passes["dynamicOpaque"]),
	}
	if passes["alpha"].VertexCount > 0 {
		ordered = append(ordered, finalizeRenderPassBundle(passes["alpha"]))
	}
	if passes["additive"].VertexCount > 0 {
		ordered = append(ordered, finalizeRenderPassBundle(passes["additive"]))
	}
	return ordered
}

func appendRenderPassObject(passes map[string]*rootengine.RenderPassBundle, bundle rootengine.RenderBundle, object rootengine.RenderObject) {
	if object.ViewCulled || object.VertexCount <= 0 {
		return
	}
	material := bundle.Materials[object.MaterialIndex]
	if !renderObjectUsesLinePass(object, material) {
		return
	}
	passName := renderPassBucketName(object)
	pass := passes[passName]
	if pass == nil {
		return
	}
	pass.Positions = appendPassSlice(pass.Positions, bundle.WorldPositions, object.VertexOffset*3, object.VertexCount*3)
	appendPassColors(pass, bundle.WorldColors, object, material)
	appendPassMaterials(pass, material, object.VertexCount)
}

func renderObjectUsesLinePass(object rootengine.RenderObject, material rootengine.RenderMaterial) bool {
	return !(object.Kind == "plane" && strings.TrimSpace(material.Texture) != "" && !material.Wireframe)
}

func appendPassSlice(target []float64, source []float64, start, length int) []float64 {
	end := start + length
	if start < 0 || end > len(source) || start > end {
		return target
	}
	return append(target, source[start:end]...)
}

func appendPassColors(pass *rootengine.RenderPassBundle, source []float64, object rootengine.RenderObject, material rootengine.RenderMaterial) {
	start := object.VertexOffset * 4
	end := start + object.VertexCount*4
	if start < 0 || end > len(source) || start > end {
		return
	}
	opacity := material.Opacity
	for i := start; i < end; i += 4 {
		pass.Colors = append(pass.Colors,
			source[i],
			source[i+1],
			source[i+2],
			source[i+3]*opacity,
		)
	}
}

func appendPassMaterials(pass *rootengine.RenderPassBundle, material rootengine.RenderMaterial, vertexCount int) {
	if len(material.ShaderData) < 3 || vertexCount <= 0 {
		return
	}
	for i := 0; i < vertexCount; i++ {
		pass.Materials = append(pass.Materials, material.ShaderData[0], material.ShaderData[1], material.ShaderData[2])
	}
}

func finalizeRenderPassBundle(pass *rootengine.RenderPassBundle) rootengine.RenderPassBundle {
	if pass == nil {
		return rootengine.RenderPassBundle{}
	}
	pass.VertexCount = len(pass.Positions) / 3
	return *pass
}

func renderPassBucketName(object rootengine.RenderObject) string {
	switch object.RenderPass {
	case "additive":
		return "additive"
	case "alpha":
		return "alpha"
	case "opaque":
		if object.Static {
			return "staticOpaque"
		}
		return "dynamicOpaque"
	default:
		if object.Static {
			return "staticOpaque"
		}
		return "dynamicOpaque"
	}
}

func renderStaticPassKey(bundle rootengine.RenderBundle) string {
	hasher := fnv.New64a()
	scratch := make([]byte, 0, 32)
	writeStaticPassFloat := func(value float64) {
		scratch = strconv.AppendFloat(scratch[:0], value, 'f', 3, 64)
		_, _ = hasher.Write(scratch)
		_, _ = hasher.Write([]byte{'|'})
	}
	writeStaticPassString := func(value string) {
		_, _ = hasher.Write([]byte(value))
		_, _ = hasher.Write([]byte{'|'})
	}

	writeStaticPassFloat(bundle.Camera.X)
	writeStaticPassFloat(bundle.Camera.Y)
	writeStaticPassFloat(bundle.Camera.Z)
	writeStaticPassFloat(bundle.Camera.RotationX)
	writeStaticPassFloat(bundle.Camera.RotationY)
	writeStaticPassFloat(bundle.Camera.RotationZ)
	writeStaticPassFloat(bundle.Camera.FOV)
	writeStaticPassFloat(bundle.Camera.Near)
	writeStaticPassFloat(bundle.Camera.Far)

	for _, object := range bundle.Objects {
		if object.ViewCulled || object.VertexCount <= 0 || object.RenderPass != "opaque" || !object.Static {
			continue
		}
		if object.MaterialIndex >= 0 && object.MaterialIndex < len(bundle.Materials) {
			if !renderObjectUsesLinePass(object, bundle.Materials[object.MaterialIndex]) {
				continue
			}
		}
		writeStaticPassString(object.ID)
		writeStaticPassString(object.Kind)
		writeStaticPassFloat(float64(object.MaterialIndex))
		writeStaticPassFloat(float64(object.VertexOffset))
		writeStaticPassFloat(float64(object.VertexCount))
		writeStaticPassFloat(object.DepthNear)
		writeStaticPassFloat(object.DepthFar)
		writeStaticPassFloat(object.DepthCenter)
		writeStaticPassFloat(object.Bounds.MinX)
		writeStaticPassFloat(object.Bounds.MinY)
		writeStaticPassFloat(object.Bounds.MinZ)
		writeStaticPassFloat(object.Bounds.MaxX)
		writeStaticPassFloat(object.Bounds.MaxY)
		writeStaticPassFloat(object.Bounds.MaxZ)
		if object.MaterialIndex >= 0 && object.MaterialIndex < len(bundle.Materials) {
			writeStaticPassString(bundle.Materials[object.MaterialIndex].Key)
		}
		start := object.VertexOffset * 4
		end := start + object.VertexCount*4
		if start >= 0 && end <= len(bundle.WorldColors) && start <= end {
			for _, value := range bundle.WorldColors[start:end] {
				writeStaticPassFloat(value)
			}
		}
	}
	return strconv.FormatUint(hasher.Sum64(), 16)
}

func appendSceneLine(bundle *rootengine.RenderBundle, width, height int, from, to rootengine.RenderPoint, color string, lineWidth float64) {
	rgba := sceneColorRGBA(color, [4]float64{0.55, 0.88, 1, 1})
	bundle.Lines = append(bundle.Lines, rootengine.RenderLine{
		From:      from,
		To:        to,
		Color:     color,
		LineWidth: lineWidth,
	})
	fromX, fromY := sceneClipPoint(from, width, height)
	toX, toY := sceneClipPoint(to, width, height)
	bundle.Positions = append(bundle.Positions, fromX, fromY, toX, toY)
	bundle.Colors = append(bundle.Colors,
		rgba[0], rgba[1], rgba[2], rgba[3],
		rgba[0], rgba[1], rgba[2], rgba[3],
	)
}

func sceneRGBAString(rgba [4]float64) string {
	r := int(math.Round(clamp(rgba[0], 0, 1) * 255))
	g := int(math.Round(clamp(rgba[1], 0, 1) * 255))
	bb := int(math.Round(clamp(rgba[2], 0, 1) * 255))
	a := clamp(rgba[3], 0, 1)
	var b strings.Builder
	b.Grow(24)
	b.WriteString("rgba(")
	b.WriteString(strconv.Itoa(r))
	b.WriteString(", ")
	b.WriteString(strconv.Itoa(g))
	b.WriteString(", ")
	b.WriteString(strconv.Itoa(bb))
	b.WriteString(", ")
	b.WriteString(strconv.FormatFloat(a, 'f', 3, 64))
	b.WriteByte(')')
	return b.String()
}

func mixRGBA(left, right [4]float64) [4]float64 {
	return [4]float64{
		(left[0] + right[0]) / 2,
		(left[1] + right[1]) / 2,
		(left[2] + right[2]) / 2,
		(left[3] + right[3]) / 2,
	}
}

func sceneLitColorRGBA(material rootengine.RenderMaterial, worldPoint, normal point3, lights []sceneLight, environment sceneEnvironment) [4]float64 {
	base := sceneColorRGBA(material.Color, [4]float64{0.55, 0.88, 1, 1})
	if !sceneLightingActive(lights, environment) {
		return base
	}

	normal = normalizePoint3(normal)
	if normal == (point3{}) {
		normal = point3{Y: 1}
	}
	baseColor := point3{X: base[0], Y: base[1], Z: base[2]}
	emissive := clamp(material.Emissive, 0, 1)
	lighting := point3{}
	if environment.AmbientIntensity > 0 {
		lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(environment.AmbientColor, point3{X: 1, Y: 1, Z: 1}), environment.AmbientIntensity)))
	}
	if environment.SkyIntensity > 0 || environment.GroundIntensity > 0 {
		hemi := clamp((normal.Y*0.5)+0.5, 0, 1)
		sky := scalePoint3(sceneColorPoint(environment.SkyColor, point3{X: 0.88, Y: 0.94, Z: 1}), environment.SkyIntensity*hemi)
		ground := scalePoint3(sceneColorPoint(environment.GroundColor, point3{X: 0.12, Y: 0.16, Z: 0.22}), environment.GroundIntensity*(1-hemi))
		lighting = addPoint3(lighting, multiplyPoint3(baseColor, addPoint3(sky, ground)))
	}
	for _, light := range lights {
		switch light.Kind {
		case "ambient":
			lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1}), light.Intensity)))
		case "directional":
			direction := normalizePoint3(point3{X: -light.DirectionX, Y: -light.DirectionY, Z: -light.DirectionZ})
			diffuse := clamp(dotPoint3(normal, direction), 0, 1)
			if diffuse > 0 {
				lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1}), light.Intensity*diffuse)))
			}
		case "point":
			offset := point3{X: light.X - worldPoint.X, Y: light.Y - worldPoint.Y, Z: light.Z - worldPoint.Z}
			distance := point3Length(offset)
			if distance == 0 {
				distance = 0.0001
			}
			diffuse := clamp(dotPoint3(normal, scalePoint3(offset, 1/distance)), 0, 1)
			if diffuse <= 0 {
				continue
			}
			attenuation := scenePointLightAttenuation(light, distance)
			if attenuation <= 0 {
				continue
			}
			lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1}), light.Intensity*diffuse*attenuation)))
		}
	}
	lit := addPoint3(scalePoint3(baseColor, emissive), scalePoint3(lighting, environment.Exposure))
	lit = addPoint3(lit, scalePoint3(baseColor, 0.06))
	return [4]float64{
		clamp(lit.X, 0, 1),
		clamp(lit.Y, 0, 1),
		clamp(lit.Z, 0, 1),
		base[3],
	}
}

func sceneLightingActive(lights []sceneLight, environment sceneEnvironment) bool {
	return len(lights) > 0 ||
		environment.AmbientIntensity > 0 ||
		environment.SkyIntensity > 0 ||
		environment.GroundIntensity > 0
}

func sceneObjectWorldNormal(object sceneObject, point point3, spinQ motion.Quat, clip clipTRS) point3 {
	normal := sceneObjectLocalNormal(object, point)
	// Base orientation, then clip rotation, then spin quaternion. Normals are
	// directions: static leaf scale applies as the inverse-scale (then
	// renormalized) so non-uniform scale keeps lighting correct; no translation
	// and no drift offset is applied. Rotation composition mirrors translatePoint
	// (base -> clipR -> spin) so lit normals stay consistent with vertex positions.
	if object.ScaleX != 0 || object.ScaleY != 0 || object.ScaleZ != 0 {
		sx, sy, sz := object.ScaleX, object.ScaleY, object.ScaleZ
		if sx == 0 {
			sx = 1
		}
		if sy == 0 {
			sy = 1
		}
		if sz == 0 {
			sz = 1
		}
		normal = point3{X: normal.X / sx, Y: normal.Y / sy, Z: normal.Z / sz}
		length := math.Sqrt(normal.X*normal.X + normal.Y*normal.Y + normal.Z*normal.Z)
		if length > 0 {
			normal = point3{X: normal.X / length, Y: normal.Y / length, Z: normal.Z / length}
		}
	}
	rotated := rotatePoint(normal, object.RotationX, object.RotationY, object.RotationZ)
	if clip.HasR {
		cx, cy, cz := motion.RotateVec3(clip.R, rotated.X, rotated.Y, rotated.Z)
		rotated = point3{X: cx, Y: cy, Z: cz}
	}
	nx, ny, nz := motion.RotateVec3(spinQ, rotated.X, rotated.Y, rotated.Z)
	return normalizePoint3(point3{X: nx, Y: ny, Z: nz})
}

// lightingContext holds all light and environment colors pre-resolved from
// string→RGBA once per frame. This lets sceneLitColorRGBAResolved skip the
// string parse on every vertex and only do the lighting math.
type lightingContext struct {
	active       bool
	ambientColor point3
	skyColor     point3
	groundColor  point3
	exposure     float64
	ambientInt   float64
	skyInt       float64
	groundInt    float64
	lightColors  []point3 // parallel to the lights slice
}

// buildLightingContext pre-resolves all color strings in lights and environment
// once per frame. When lighting is inactive it short-circuits immediately.
func buildLightingContext(lights []sceneLight, env sceneEnvironment) lightingContext {
	if !sceneLightingActive(lights, env) {
		return lightingContext{}
	}
	ctx := lightingContext{
		active:       true,
		ambientColor: sceneColorPoint(env.AmbientColor, point3{X: 1, Y: 1, Z: 1}),
		skyColor:     sceneColorPoint(env.SkyColor, point3{X: 0.88, Y: 0.94, Z: 1}),
		groundColor:  sceneColorPoint(env.GroundColor, point3{X: 0.12, Y: 0.16, Z: 0.22}),
		exposure:     env.Exposure,
		ambientInt:   env.AmbientIntensity,
		skyInt:       env.SkyIntensity,
		groundInt:    env.GroundIntensity,
	}
	if len(lights) > 0 {
		ctx.lightColors = make([]point3, len(lights))
		for i, light := range lights {
			ctx.lightColors[i] = sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1})
		}
	}
	return ctx
}

// sceneLitColorRGBAResolved is sceneLitColorRGBA with the base material RGBA
// and all light/environment colors already resolved (no string parse per vertex).
// The lighting math is IDENTICAL to sceneLitColorRGBA; only the parse is moved.
func sceneLitColorRGBAResolved(base [4]float64, material rootengine.RenderMaterial, worldPoint, normal point3, lights []sceneLight, ctx lightingContext) [4]float64 {
	if !ctx.active {
		return base
	}

	normal = normalizePoint3(normal)
	if normal == (point3{}) {
		normal = point3{Y: 1}
	}
	baseColor := point3{X: base[0], Y: base[1], Z: base[2]}
	emissive := clamp(material.Emissive, 0, 1)
	lighting := point3{}
	if ctx.ambientInt > 0 {
		lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(ctx.ambientColor, ctx.ambientInt)))
	}
	if ctx.skyInt > 0 || ctx.groundInt > 0 {
		hemi := clamp((normal.Y*0.5)+0.5, 0, 1)
		sky := scalePoint3(ctx.skyColor, ctx.skyInt*hemi)
		ground := scalePoint3(ctx.groundColor, ctx.groundInt*(1-hemi))
		lighting = addPoint3(lighting, multiplyPoint3(baseColor, addPoint3(sky, ground)))
	}
	for i, light := range lights {
		lightColor := ctx.lightColors[i]
		switch light.Kind {
		case "ambient":
			lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(lightColor, light.Intensity)))
		case "directional":
			direction := normalizePoint3(point3{X: -light.DirectionX, Y: -light.DirectionY, Z: -light.DirectionZ})
			diffuse := clamp(dotPoint3(normal, direction), 0, 1)
			if diffuse > 0 {
				lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(lightColor, light.Intensity*diffuse)))
			}
		case "point":
			offset := point3{X: light.X - worldPoint.X, Y: light.Y - worldPoint.Y, Z: light.Z - worldPoint.Z}
			distance := point3Length(offset)
			if distance == 0 {
				distance = 0.0001
			}
			diffuse := clamp(dotPoint3(normal, scalePoint3(offset, 1/distance)), 0, 1)
			if diffuse <= 0 {
				continue
			}
			attenuation := scenePointLightAttenuation(light, distance)
			if attenuation <= 0 {
				continue
			}
			lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(lightColor, light.Intensity*diffuse*attenuation)))
		}
	}
	lit := addPoint3(scalePoint3(baseColor, emissive), scalePoint3(lighting, ctx.exposure))
	lit = addPoint3(lit, scalePoint3(baseColor, 0.06))
	return [4]float64{
		clamp(lit.X, 0, 1),
		clamp(lit.Y, 0, 1),
		clamp(lit.Z, 0, 1),
		base[3],
	}
}

// sceneObjectWorldNormalTrig is the trig-hoisted variant used by bakeSceneObjectWorld
// (the static/cache path). It uses the object's combined (rotation+spin*time) trig
// for the same result as the identity-spin case of sceneObjectWorldNormal.
func sceneObjectWorldNormalTrig(object sceneObject, point point3, objTrig rotTrig) point3 {
	normal := sceneObjectLocalNormal(object, point)
	return normalizePoint3(rotatePointTrig(normal, objTrig))
}

func sceneObjectLocalNormal(object sceneObject, point point3) point3 {
	switch object.Kind {
	case "lines":
		width := math.Max(object.Width/2, 0.0001)
		height := math.Max(object.Height/2, 0.0001)
		depth := math.Max(object.Depth/2, 0.0001)
		ax := math.Abs(point.X / width)
		ay := math.Abs(point.Y / height)
		az := math.Abs(point.Z / depth)
		switch {
		case ax >= ay && ax >= az:
			return point3{X: math.Copysign(1, point.X)}
		case ay >= az:
			return point3{Y: math.Copysign(1, point.Y)}
		default:
			return point3{Z: math.Copysign(1, point.Z)}
		}
	case "plane":
		return point3{Y: 1}
	case "sphere":
		return normalizePoint3(point)
	case "pyramid":
		width := math.Max(object.Width/2, 0.0001)
		height := math.Max(object.Height/2, 0.0001)
		depth := math.Max(object.Depth/2, 0.0001)
		return normalizePoint3(point3{
			X: point.X / width,
			Y: (point.Y / height) + 0.35,
			Z: point.Z / depth,
		})
	default:
		width := math.Max(object.Width/2, 0.0001)
		height := math.Max(object.Height/2, 0.0001)
		depth := math.Max(object.Depth/2, 0.0001)
		ax := math.Abs(point.X / width)
		ay := math.Abs(point.Y / height)
		az := math.Abs(point.Z / depth)
		switch {
		case ax >= ay && ax >= az:
			return point3{X: math.Copysign(1, point.X)}
		case ay >= az:
			return point3{Y: math.Copysign(1, point.Y)}
		default:
			return point3{Z: math.Copysign(1, point.Z)}
		}
	}
}

func scenePointLightAttenuation(light sceneLight, distance float64) float64 {
	if light.Range > 0 {
		falloff := clamp(1-(distance/light.Range), 0, 1)
		return math.Pow(falloff, light.Decay)
	}
	return 1 / (1 + math.Pow(distance*0.35, math.Max(light.Decay, 1)))
}

func sceneColorPoint(value string, fallback point3) point3 {
	rgba := sceneColorRGBA(value, [4]float64{fallback.X, fallback.Y, fallback.Z, 1})
	return point3{X: rgba[0], Y: rgba[1], Z: rgba[2]}
}

func addPoint3(left, right point3) point3 {
	return point3{X: left.X + right.X, Y: left.Y + right.Y, Z: left.Z + right.Z}
}

func scalePoint3(point point3, scale float64) point3 {
	return point3{X: point.X * scale, Y: point.Y * scale, Z: point.Z * scale}
}

func multiplyPoint3(left, right point3) point3 {
	return point3{X: left.X * right.X, Y: left.Y * right.Y, Z: left.Z * right.Z}
}

func point3Length(point point3) float64 {
	return math.Sqrt((point.X * point.X) + (point.Y * point.Y) + (point.Z * point.Z))
}

func normalizePoint3(point point3) point3 {
	length := point3Length(point)
	if length == 0 {
		return point3{}
	}
	return scalePoint3(point, 1/length)
}

func dotPoint3(left, right point3) float64 {
	return (left.X * right.X) + (left.Y * right.Y) + (left.Z * right.Z)
}

func sceneClipPoint(point rootengine.RenderPoint, width, height int) (float64, float64) {
	return (point.X/float64(width))*2 - 1, 1 - (point.Y/float64(height))*2
}

// geometryCacheKey identifies a local-space geometry result. Local geometry is a
// pure function of (kind, dims, segments): it does NOT depend on the object's
// position, rotation, spin, color, or any per-instance state (the bake loop reads
// it and builds NEW transformed point3s — it never mutates the returned slices).
// So two objects of the same kind+dims share one immutable geometry result.
//
// The float dims are exact authored values (never NaN), so they are safe direct
// map keys. "box"/"cube"/default all bake identical box geometry, so they
// normalize to a single "box" key in sceneObjectSegments.
type geometryCacheKey struct {
	kind     string
	width    float64
	height   float64
	depth    float64
	radius   float64
	segments int
}

// segmentGeometryCache and boxVertexCache memoize immutable local-space geometry.
// sync.Map is used because buildRenderBundle runs single-threaded in WASM but may
// run concurrently across requests in the native/SSR path; the stored values are
// read-only slices shared by all callers (callers only ever read them).
var (
	segmentGeometryCache sync.Map // geometryCacheKey -> [][2]point3 (read-only)
	boxVertexCache       sync.Map // geometryCacheKey -> []point3 (read-only)
)

func sceneObjectSegments(object sceneObject) [][2]point3 {
	switch object.Kind {
	case "box", "cube":
		return cachedBoxSegments(object)
	case "lines":
		// Custom line geometry depends on per-object Points/LineSegments, which
		// are arbitrary per instance, so it is not memoized.
		return customLineSegments(object)
	case "plane":
		return cachedPlaneSegments(object)
	case "pyramid":
		return cachedPyramidSegments(object)
	case "sphere":
		return cachedSphereSegments(object)
	default:
		return cachedBoxSegments(object)
	}
}

func cachedBoxSegments(object sceneObject) [][2]point3 {
	key := geometryCacheKey{kind: "box", width: object.Width, height: object.Height, depth: object.Depth}
	if v, ok := segmentGeometryCache.Load(key); ok {
		return v.([][2]point3)
	}
	out := boxSegments(object)
	actual, _ := segmentGeometryCache.LoadOrStore(key, out)
	return actual.([][2]point3)
}

func cachedPlaneSegments(object sceneObject) [][2]point3 {
	key := geometryCacheKey{kind: "plane", width: object.Width, depth: object.Depth}
	if v, ok := segmentGeometryCache.Load(key); ok {
		return v.([][2]point3)
	}
	out := planeSegments(object)
	actual, _ := segmentGeometryCache.LoadOrStore(key, out)
	return actual.([][2]point3)
}

func cachedPyramidSegments(object sceneObject) [][2]point3 {
	key := geometryCacheKey{kind: "pyramid", width: object.Width, height: object.Height, depth: object.Depth}
	if v, ok := segmentGeometryCache.Load(key); ok {
		return v.([][2]point3)
	}
	out := pyramidSegments(object)
	actual, _ := segmentGeometryCache.LoadOrStore(key, out)
	return actual.([][2]point3)
}

func cachedSphereSegments(object sceneObject) [][2]point3 {
	key := geometryCacheKey{kind: "sphere", radius: object.Radius, segments: object.Segments}
	if v, ok := segmentGeometryCache.Load(key); ok {
		return v.([][2]point3)
	}
	out := sphereSegments(object)
	actual, _ := segmentGeometryCache.LoadOrStore(key, out)
	return actual.([][2]point3)
}

// cachedBoxVertices memoizes boxVertices. Callers (scenePlaneSurfaceCornersTrig,
// planeSegments) only read the returned slice and build new point3s, so the
// shared immutable slice is safe.
func cachedBoxVertices(width, height, depth float64) []point3 {
	key := geometryCacheKey{kind: "boxv", width: width, height: height, depth: depth}
	if v, ok := boxVertexCache.Load(key); ok {
		return v.([]point3)
	}
	out := boxVertices(width, height, depth)
	actual, _ := boxVertexCache.LoadOrStore(key, out)
	return actual.([]point3)
}

func customLineSegments(object sceneObject) [][2]point3 {
	out := make([][2]point3, 0, len(object.LineSegments))
	for _, edge := range object.LineSegments {
		if edge[0] < 0 || edge[1] < 0 || edge[0] >= len(object.Points) || edge[1] >= len(object.Points) || edge[0] == edge[1] {
			continue
		}
		out = append(out, [2]point3{object.Points[edge[0]], object.Points[edge[1]]})
	}
	return out
}

func boxSegments(object sceneObject) [][2]point3 {
	return indexSegments(boxVertices(object.Width, object.Height, object.Depth), [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{4, 5}, {5, 6}, {6, 7}, {7, 4},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
	})
}

func planeSegments(object sceneObject) [][2]point3 {
	vertices := cachedBoxVertices(object.Width, 0, object.Depth)
	return indexSegments(vertices[:4], [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
	})
}

func pyramidSegments(object sceneObject) [][2]point3 {
	halfWidth := object.Width / 2
	halfDepth := object.Depth / 2
	halfHeight := object.Height / 2
	vertices := []point3{
		{X: -halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: -halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: 0, Y: halfHeight, Z: 0},
	}
	return indexSegments(vertices, [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{0, 4}, {1, 4}, {2, 4}, {3, 4},
	})
}

func sphereSegments(object sceneObject) [][2]point3 {
	out := circleSegments(object.Radius, "xy", object.Segments)
	out = append(out, circleSegments(object.Radius, "xz", object.Segments)...)
	out = append(out, circleSegments(object.Radius, "yz", object.Segments)...)
	return out
}

func boxVertices(width, height, depth float64) []point3 {
	halfWidth := width / 2
	halfHeight := height / 2
	halfDepth := depth / 2
	return []point3{
		{X: -halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: halfHeight, Z: -halfDepth},
		{X: -halfWidth, Y: halfHeight, Z: -halfDepth},
		{X: -halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: halfWidth, Y: halfHeight, Z: halfDepth},
		{X: -halfWidth, Y: halfHeight, Z: halfDepth},
	}
}

func indexSegments(points []point3, edgePairs [][2]int) [][2]point3 {
	out := make([][2]point3, 0, len(edgePairs))
	for _, edge := range edgePairs {
		out = append(out, [2]point3{points[edge[0]], points[edge[1]]})
	}
	return out
}

func circleSegments(radius float64, axis string, segments int) [][2]point3 {
	points := make([]point3, 0, segments)
	for i := 0; i < segments; i++ {
		angle := (math.Pi * 2 * float64(i)) / float64(segments)
		points = append(points, circlePoint(radius, axis, angle))
	}
	out := make([][2]point3, 0, len(points))
	for i := range points {
		out = append(out, [2]point3{points[i], points[(i+1)%len(points)]})
	}
	return out
}

func circlePoint(radius float64, axis string, angle float64) point3 {
	sin := math.Sin(angle) * radius
	cos := math.Cos(angle) * radius
	switch axis {
	case "xy":
		return point3{X: cos, Y: sin, Z: 0}
	case "yz":
		return point3{X: 0, Y: cos, Z: sin}
	default:
		return point3{X: cos, Y: 0, Z: sin}
	}
}

// sceneObjectRotTrig computes the object's combined (rotation + spin*time)
// rotation trig once. The combined angle is constant across every vertex of the
// object at this frame, so this is hoisted out of the per-vertex loop.
// Used only by the static/cache bake path (bakeSceneObjectWorld).
func sceneObjectRotTrig(object sceneObject, timeSeconds float64) rotTrig {
	return newRotTrig(
		object.RotationX+object.SpinX*timeSeconds,
		object.RotationY+object.SpinY*timeSeconds,
		object.RotationZ+object.SpinZ*timeSeconds,
	)
}

// translatePointTrig is the trig-hoisted transform used by the static/cache bake
// path (bakeSceneObjectWorld). For a bake-eligible object (SpinX/Y/Z == 0, no
// drift, no clip) this is bit-identical to translatePoint with identityQuat+emptyClip.
func translatePointTrig(point point3, object sceneObject, timeSeconds float64, objTrig rotTrig) point3 {
	rotated := rotatePointTrig(applySceneObjectScale(point, object), objTrig)
	offset := sceneMotionOffset(object, timeSeconds)
	return point3{
		X: rotated.X + object.X + offset.X,
		Y: rotated.Y + object.Y + offset.Y,
		Z: rotated.Z + object.Z + offset.Z,
	}
}

// NOTE: The clip composition here (base→clipR→spin, translation in world space)
// intentionally differs from the native render/bundle instanced-mesh matrix path
// (T*R*S left-multiply on the raw transform). These are different renderer/bundle
// structures — not a parity pair — so the divergence is by design.
func translatePoint(point point3, object sceneObject, spinQ motion.Quat, clip clipTRS, timeSeconds float64) point3 {
	local := point
	// Composition order (local vertex -> world position):
	//   1. clip scale (local, pre-rotation): multiply the LOCAL vertex by clip.S.
	//   2. base orientation: the existing intrinsic Euler rotation.
	//   3. clip rotation: the evaluated clip quaternion, AFTER base, BEFORE spin.
	//   4. spin orientation: the GenSpin quaternion (unchanged).
	//   5. translation: object position + drift offset + clip translation.
	// When the object has no clip targeting it (the common case) clip is the zero
	// value (all Has* false) and this path is byte-identical to the pre-clip code.
	local = applySceneObjectScale(local, object)
	if clip.HasS {
		local = point3{X: point.X * clip.S[0], Y: point.Y * clip.S[1], Z: point.Z * clip.S[2]}
	}
	// Base orientation via the existing intrinsic Euler rotation.
	rotated := rotatePoint(local, object.RotationX, object.RotationY, object.RotationZ)
	// Clip rotation: composed AFTER the base orientation and BEFORE spin.
	if clip.HasR {
		cx, cy, cz := motion.RotateVec3(clip.R, rotated.X, rotated.Y, rotated.Z)
		rotated = point3{X: cx, Y: cy, Z: cz}
	}
	// Spin orientation sourced from the canonical motion evaluator (GenSpin),
	// applied as a quaternion. For single-axis spin this is identical to the
	// previous Euler-add path; multi-axis spin now follows canonical qx*qy*qz order.
	sx, sy, sz := motion.RotateVec3(spinQ, rotated.X, rotated.Y, rotated.Z)
	offset := sceneMotionOffset(object, timeSeconds)
	tx, ty, tz := 0.0, 0.0, 0.0
	if clip.HasT {
		tx, ty, tz = clip.T[0], clip.T[1], clip.T[2]
	}
	return point3{
		X: sx + object.X + offset.X + tx,
		Y: sy + object.Y + offset.Y + ty,
		Z: sz + object.Z + offset.Z + tz,
	}
}

// applySceneObjectScale applies the static leaf scale (ObjectIR scaleX/Y/Z)
// to a local vertex. Zero components mean "unset" and keep unit scale so
// pre-scale scenes are byte-identical.
func applySceneObjectScale(point point3, object sceneObject) point3 {
	if object.ScaleX == 0 && object.ScaleY == 0 && object.ScaleZ == 0 {
		return point
	}
	sx, sy, sz := object.ScaleX, object.ScaleY, object.ScaleZ
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	if sz == 0 {
		sz = 1
	}
	return point3{X: point.X * sx, Y: point.Y * sy, Z: point.Z * sz}
}

// cameraModeForKind maps the authored camera kind onto the RenderCamera mode
// so native picking can select the orthographic ray branch.
func cameraModeForKind(kind string) string {
	if kind == "orthographic" {
		return "orthographic"
	}
	return ""
}

func sceneMotionOffset(object sceneObject, timeSeconds float64) point3 {
	if object.ShiftX == 0 && object.ShiftY == 0 && object.ShiftZ == 0 {
		return point3{}
	}
	angle := object.DriftPhase + timeSeconds*object.DriftSpeed
	return point3{
		X: math.Cos(angle) * object.ShiftX,
		Y: math.Sin(angle*0.82+object.DriftPhase*0.35) * object.ShiftY,
		Z: math.Sin(angle) * object.ShiftZ,
	}
}

// rotTrig caches the sin/cos of a rotation's three Euler angles so callers can
// hoist the 6 transcendental calls out of per-vertex loops. The sinX..cosZ
// fields are the EXACT values that rotatePoint/inverseRotatePoint would compute
// inline, so the downstream arithmetic is bit-identical.
type rotTrig struct {
	sinX, cosX float64
	sinY, cosY float64
	sinZ, cosZ float64
}

// newRotTrig computes the forward-rotation trig for angles (x, y, z) once.
func newRotTrig(rotationX, rotationY, rotationZ float64) rotTrig {
	sinX, cosX := math.Sin(rotationX), math.Cos(rotationX)
	sinY, cosY := math.Sin(rotationY), math.Cos(rotationY)
	sinZ, cosZ := math.Sin(rotationZ), math.Cos(rotationZ)
	return rotTrig{sinX: sinX, cosX: cosX, sinY: sinY, cosY: cosY, sinZ: sinZ, cosZ: cosZ}
}

// newInverseRotTrig computes the inverse-rotation trig (negated angles) once.
func newInverseRotTrig(rotationX, rotationY, rotationZ float64) rotTrig {
	sinZ, cosZ := math.Sin(-rotationZ), math.Cos(-rotationZ)
	sinY, cosY := math.Sin(-rotationY), math.Cos(-rotationY)
	sinX, cosX := math.Sin(-rotationX), math.Cos(-rotationX)
	return rotTrig{sinX: sinX, cosX: cosX, sinY: sinY, cosY: cosY, sinZ: sinZ, cosZ: cosZ}
}

func rotatePoint(point point3, rotationX, rotationY, rotationZ float64) point3 {
	return rotatePointTrig(point, newRotTrig(rotationX, rotationY, rotationZ))
}

// rotatePointTrig performs the same arithmetic as rotatePoint but with
// precomputed trig. The product/sum ordering is identical to rotatePoint so the
// float results are bit-for-bit equal.
func rotatePointTrig(point point3, t rotTrig) point3 {
	x := point.X
	y := point.Y
	z := point.Z

	sinX, cosX := t.sinX, t.cosX
	nextY := y*cosX - z*sinX
	nextZ := y*sinX + z*cosX
	y, z = nextY, nextZ

	sinY, cosY := t.sinY, t.cosY
	nextX := x*cosY + z*sinY
	nextZ = -x*sinY + z*cosY
	x, z = nextX, nextZ

	sinZ, cosZ := t.sinZ, t.cosZ
	nextX = x*cosZ - y*sinZ
	nextY = x*sinZ + y*cosZ

	return point3{X: nextX, Y: nextY, Z: z}
}

func projectPoint(point point3, camera sceneCamera, width, height int) *rootengine.RenderPoint {
	return projectPointTrig(point, camera, width, height, cameraInverseRotTrig(camera))
}

func projectPointTrig(point point3, camera sceneCamera, width, height int, camTrig rotTrig) *rootengine.RenderPoint {
	local := cameraLocalPointTrig(point, camera, camTrig)
	depth := local.Z
	if depth <= camera.Near || depth >= camera.Far {
		return nil
	}
	focal := (math.Min(float64(width), float64(height)) / 2) / math.Tan((camera.FOV*math.Pi)/360)
	return &rootengine.RenderPoint{
		X: float64(width)/2 + (local.X*focal)/depth,
		Y: float64(height)/2 - (local.Y*focal)/depth,
	}
}

func clipWorldSegmentForCamera(from, to point3, camera sceneCamera, aspect float64) (point3, point3, bool) {
	return clipWorldSegmentForCameraTrig(from, to, camera, aspect, cameraInverseRotTrig(camera))
}

func clipWorldSegmentForCameraTrig(from, to point3, camera sceneCamera, aspect float64, camTrig rotTrig) (point3, point3, bool) {
	localFrom := cameraLocalPointTrig(from, camera, camTrig)
	localTo := cameraLocalPointTrig(to, camera, camTrig)
	depthFrom := localFrom.Z
	depthTo := localTo.Z
	if depthFrom <= camera.Near && depthTo <= camera.Near {
		return point3{}, point3{}, false
	}
	clippedFrom := from
	clippedTo := to
	if depthFrom <= camera.Near || depthTo <= camera.Near {
		t := (camera.Near - depthFrom) / math.Max(0.000001, depthTo-depthFrom)
		if depthFrom <= camera.Near {
			clippedFrom = lerpPoint3(from, to, t)
		} else {
			clippedTo = lerpPoint3(from, to, t)
		}
	}
	depthFrom = cameraLocalPointTrig(clippedFrom, camera, camTrig).Z
	depthTo = cameraLocalPointTrig(clippedTo, camera, camTrig).Z
	if worldSegmentOutsideFrustumTrig(clippedFrom, depthFrom, clippedTo, depthTo, camera, aspect, camTrig) {
		return point3{}, point3{}, false
	}
	return clippedFrom, clippedTo, true
}

func renderObjectBounds(worldPositions []float64, vertexOffset, vertexCount int) rootengine.RenderBounds {
	if vertexCount <= 0 {
		return rootengine.RenderBounds{}
	}
	start := vertexOffset * 3
	bounds := rootengine.RenderBounds{
		MinX: worldPositions[start],
		MinY: worldPositions[start+1],
		MinZ: worldPositions[start+2],
		MaxX: worldPositions[start],
		MaxY: worldPositions[start+1],
		MaxZ: worldPositions[start+2],
	}
	for i := start + 3; i < start+vertexCount*3; i += 3 {
		x := worldPositions[i]
		y := worldPositions[i+1]
		z := worldPositions[i+2]
		bounds.MinX = math.Min(bounds.MinX, x)
		bounds.MinY = math.Min(bounds.MinY, y)
		bounds.MinZ = math.Min(bounds.MinZ, z)
		bounds.MaxX = math.Max(bounds.MaxX, x)
		bounds.MaxY = math.Max(bounds.MaxY, y)
		bounds.MaxZ = math.Max(bounds.MaxZ, z)
	}
	return bounds
}

func renderBoundsOutsideFrustum(bounds rootengine.RenderBounds, camera sceneCamera, width, height int) bool {
	return renderBoundsOutsideFrustumTrig(bounds, camera, width, height, cameraInverseRotTrig(camera))
}

func renderBoundsOutsideFrustumTrig(bounds rootengine.RenderBounds, camera sceneCamera, width, height int, camTrig rotTrig) bool {
	aspect := math.Max(0.0001, float64(width)/math.Max(1, float64(height)))
	corners := renderBoundsCorners(bounds)
	allLeft, allRight, allBottom, allTop := true, true, true, true
	allNear, allFar := true, true
	for _, corner := range corners {
		local := cameraLocalPointTrig(corner, camera, camTrig)
		allNear = allNear && local.Z <= camera.Near
		allFar = allFar && local.Z >= camera.Far
		clipX, clipY := projectWorldClipPointTrig(corner, camera, aspect, camTrig)
		allLeft = allLeft && clipX < -1
		allRight = allRight && clipX > 1
		allBottom = allBottom && clipY < -1
		allTop = allTop && clipY > 1
	}
	if allNear || allFar {
		return true
	}
	return allLeft || allRight || allBottom || allTop
}

func renderBoundsDepthMetrics(bounds rootengine.RenderBounds, camera sceneCamera) (near, far, center float64) {
	return renderBoundsDepthMetricsTrig(bounds, camera, cameraInverseRotTrig(camera))
}

func renderBoundsDepthMetricsTrig(bounds rootengine.RenderBounds, camera sceneCamera, camTrig rotTrig) (near, far, center float64) {
	corners := renderBoundsCorners(bounds)
	near = cameraLocalPointTrig(corners[0], camera, camTrig).Z
	far = near
	for i := 1; i < len(corners); i++ {
		depth := cameraLocalPointTrig(corners[i], camera, camTrig).Z
		near = math.Min(near, depth)
		far = math.Max(far, depth)
	}
	center = (near + far) / 2
	return near, far, center
}

func expandRenderBounds(bounds rootengine.RenderBounds, hasBounds bool, point point3) (rootengine.RenderBounds, bool) {
	if !hasBounds {
		return rootengine.RenderBounds{
			MinX: point.X,
			MinY: point.Y,
			MinZ: point.Z,
			MaxX: point.X,
			MaxY: point.Y,
			MaxZ: point.Z,
		}, true
	}
	bounds.MinX = math.Min(bounds.MinX, point.X)
	bounds.MinY = math.Min(bounds.MinY, point.Y)
	bounds.MinZ = math.Min(bounds.MinZ, point.Z)
	bounds.MaxX = math.Max(bounds.MaxX, point.X)
	bounds.MaxY = math.Max(bounds.MaxY, point.Y)
	bounds.MaxZ = math.Max(bounds.MaxZ, point.Z)
	return bounds, true
}

// renderBoundsCorners returns all 8 corners of the axis-aligned bounding box as
// a value array (stack-allocated, does not escape to heap).
func renderBoundsCorners(bounds rootengine.RenderBounds) [8]point3 {
	return [8]point3{
		{X: bounds.MinX, Y: bounds.MinY, Z: bounds.MinZ},
		{X: bounds.MinX, Y: bounds.MinY, Z: bounds.MaxZ},
		{X: bounds.MinX, Y: bounds.MaxY, Z: bounds.MinZ},
		{X: bounds.MinX, Y: bounds.MaxY, Z: bounds.MaxZ},
		{X: bounds.MaxX, Y: bounds.MinY, Z: bounds.MinZ},
		{X: bounds.MaxX, Y: bounds.MinY, Z: bounds.MaxZ},
		{X: bounds.MaxX, Y: bounds.MaxY, Z: bounds.MinZ},
		{X: bounds.MaxX, Y: bounds.MaxY, Z: bounds.MaxZ},
	}
}

func projectWorldClipPoint(point point3, camera sceneCamera, aspect float64) (float64, float64) {
	return projectWorldClipPointTrig(point, camera, aspect, cameraInverseRotTrig(camera))
}

func projectWorldClipPointTrig(point point3, camera sceneCamera, aspect float64, camTrig rotTrig) (float64, float64) {
	local := cameraLocalPointTrig(point, camera, camTrig)
	depth := local.Z
	focal := 1 / math.Max(0.0001, math.Tan((camera.FOV*math.Pi)/360))
	x := (local.X * focal / math.Max(depth, 0.0001)) / math.Max(aspect, 0.0001)
	y := local.Y * focal / math.Max(depth, 0.0001)
	return x, y
}

func worldSegmentOutsideFrustum(from point3, depthFrom float64, to point3, depthTo float64, camera sceneCamera, aspect float64) bool {
	return worldSegmentOutsideFrustumTrig(from, depthFrom, to, depthTo, camera, aspect, cameraInverseRotTrig(camera))
}

func worldSegmentOutsideFrustumTrig(from point3, depthFrom float64, to point3, depthTo float64, camera sceneCamera, aspect float64, camTrig rotTrig) bool {
	if depthFrom >= camera.Far && depthTo >= camera.Far {
		return true
	}
	clipFromX, clipFromY := projectWorldClipPointTrig(from, camera, aspect, camTrig)
	clipToX, clipToY := projectWorldClipPointTrig(to, camera, aspect, camTrig)
	return (clipFromX < -1 && clipToX < -1) ||
		(clipFromX > 1 && clipToX > 1) ||
		(clipFromY < -1 && clipToY < -1) ||
		(clipFromY > 1 && clipToY > 1)
}

// cameraInverseRotTrig caches the camera's inverse-rotation trig for an entire
// frame; every cameraLocalPoint call in the frame shares the same angles.
func cameraInverseRotTrig(camera sceneCamera) rotTrig {
	return newInverseRotTrig(camera.RotationX, camera.RotationY, camera.RotationZ)
}

func cameraLocalPoint(point point3, camera sceneCamera) point3 {
	return cameraLocalPointTrig(point, camera, cameraInverseRotTrig(camera))
}

// cameraLocalPointTrig is cameraLocalPoint with the camera inverse-rotation trig
// supplied by the caller (hoisted once per frame). Translation then inverse
// rotation is performed in the identical order, so results are bit-identical.
func cameraLocalPointTrig(point point3, camera sceneCamera, camTrig rotTrig) point3 {
	translated := point3{
		X: point.X - camera.X,
		Y: point.Y - camera.Y,
		Z: point.Z + camera.Z,
	}
	return inverseRotatePointTrig(translated, camTrig)
}

func inverseRotatePoint(point point3, rotationX, rotationY, rotationZ float64) point3 {
	return inverseRotatePointTrig(point, newInverseRotTrig(rotationX, rotationY, rotationZ))
}

// inverseRotatePointTrig performs the same arithmetic as inverseRotatePoint but
// with precomputed (already-negated) trig. The trig fields hold sin/cos of the
// NEGATED angles, matching inverseRotatePoint's inline computation exactly, so
// the float results are bit-for-bit equal.
func inverseRotatePointTrig(point point3, t rotTrig) point3 {
	x := point.X
	y := point.Y
	z := point.Z

	sinZ, cosZ := t.sinZ, t.cosZ
	nextX := x*cosZ - y*sinZ
	nextY := x*sinZ + y*cosZ
	x, y = nextX, nextY

	sinY, cosY := t.sinY, t.cosY
	nextX = x*cosY + z*sinY
	nextZ := -x*sinY + z*cosY
	x, z = nextX, nextZ

	sinX, cosX := t.sinX, t.cosX
	nextY = y*cosX - z*sinX
	nextZ = y*sinX + z*cosX
	return point3{X: x, Y: nextY, Z: nextZ}
}

func lerpPoint3(from, to point3, t float64) point3 {
	t = clamp(t, 0, 1)
	return point3{
		X: from.X + (to.X-from.X)*t,
		Y: from.Y + (to.Y-from.Y)*t,
		Z: from.Z + (to.Z-from.Z)*t,
	}
}
