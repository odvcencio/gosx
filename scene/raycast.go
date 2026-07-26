package scene

import (
	"math"
	"slices"
	"strings"
)

// Ray is a world-space ray used for scene queries such as hitscan weapons,
// editor picking, and interaction probes.
type Ray struct {
	Origin    Vector3 `json:"origin"`
	Direction Vector3 `json:"direction"`
}

// RayHit describes the nearest scene graph intersection.
type RayHit struct {
	ID            string  `json:"id,omitempty"`
	Kind          string  `json:"kind,omitempty"`
	Distance      float64 `json:"distance"`
	Point         Vector3 `json:"point"`
	Normal        Vector3 `json:"normal,omitzero"`
	Pickable      bool    `json:"pickable,omitempty"`
	InstanceIndex *int    `json:"instanceIndex,omitempty"`
	Method        string  `json:"method,omitempty"`
}

// RaycastOptions controls a scene graph ray query.
type RaycastOptions struct {
	PickableOnly bool    `json:"pickableOnly,omitempty"`
	MaxDistance  float64 `json:"maxDistance,omitempty"`
	// PointThreshold is the world-space hit radius for primitives that carry no
	// cross-section: Points particles, Sprite billboards, and Lines strokes.
	// Points and sprites draw as screen-space sprites and a line stroke is a few
	// CSS pixels wide, so none of them own a world thickness. A ray hits one when
	// it passes within this radius of the particle center or of the line segment.
	// Zero selects DefaultPointThreshold. This mirrors the three.js
	// Raycaster.params.Points.threshold and params.Line.threshold controls, which
	// GoSX keeps as one number.
	PointThreshold float64 `json:"pointThreshold,omitempty"`
}

// DefaultPointThreshold is the world-space hit radius applied to Points
// particles, Sprite billboards, and Lines strokes when
// RaycastOptions.PointThreshold is zero.
const DefaultPointThreshold = 0.1

// RayTrace is the deterministic, agent-readable account of a ray query. It is
// deliberately free of wall-clock timings so snapshots remain stable across
// machines and native/headless test runs.
type RayTrace struct {
	Ray                Ray            `json:"ray"`
	Options            RaycastOptions `json:"options"`
	NodesVisited       int            `json:"nodesVisited"`
	PrimitivesTested   int            `json:"primitivesTested"`
	InstancesTested    int            `json:"instancesTested"`
	FilteredPrimitives int            `json:"filteredPrimitives,omitempty"`
	// BroadphaseRejected counts primitives that a bounding-volume test removed
	// before any exact intersection ran. PrimitivesTested counts only the exact
	// tests that followed, so the two numbers sum to the candidate total.
	BroadphaseRejected int      `json:"broadphaseRejected,omitempty"`
	Hits               []RayHit `json:"hits"`
	Closest            *RayHit  `json:"closest,omitempty"`
}

// RaycastOption mutates RaycastOptions.
type RaycastOption func(*RaycastOptions)

// PickableOnly limits ray queries to meshes that have not opted out of
// pointer-style picking.
func PickableOnly() RaycastOption {
	return func(opts *RaycastOptions) {
		opts.PickableOnly = true
	}
}

// MaxDistance caps ray hits to distance world units from the ray origin.
func MaxDistance(distance float64) RaycastOption {
	return func(opts *RaycastOptions) {
		opts.MaxDistance = distance
	}
}

// PointThreshold sets the world-space hit radius for Points particles, Sprite
// billboards, and Lines strokes. Pass a larger radius to make sparse point
// clouds or thin polylines easier to pick.
func PointThreshold(radius float64) RaycastOption {
	return func(opts *RaycastOptions) {
		opts.PointThreshold = radius
	}
}

// Raycast returns the closest hit in props.Graph.
func Raycast(props Props, ray Ray, options ...RaycastOption) (RayHit, bool) {
	return RaycastGraph(props.Graph, ray, options...)
}

// RaycastGraph returns the closest hit in graph.
func RaycastGraph(graph Graph, ray Ray, options ...RaycastOption) (RayHit, bool) {
	trace := TraceGraph(graph, ray, options...)
	if trace.Closest == nil {
		return RayHit{}, false
	}
	return *trace.Closest, true
}

// RaycastAll returns every accepted hit, nearest first. Instanced meshes emit
// one hit per intersected instance and identify it with InstanceIndex.
func RaycastAll(graph Graph, ray Ray, options ...RaycastOption) []RayHit {
	return TraceGraph(graph, ray, options...).Hits
}

// TraceGraph runs the same query as RaycastGraph and includes traversal and
// intersection telemetry suitable for native scene harnesses and agents.
func TraceGraph(graph Graph, ray Ray, options ...RaycastOption) RayTrace {
	opts := RaycastOptions{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	ray.Direction = normalizeVector(ray.Direction)
	trace := RayTrace{Ray: ray, Options: opts, Hits: []RayHit{}}
	if ray.Direction == (Vector3{}) {
		return trace
	}
	// Resolve the pick radius once. trace.Options already holds the caller's
	// values, so this local copy never reaches the report.
	opts.PointThreshold = pointHitRadius(opts)
	for _, node := range graph.Nodes {
		raycastNode(node, identityTransform(), ray, opts, &trace)
	}
	finalizeTrace(&trace)
	return trace
}

// finalizeTrace orders hits nearest first and publishes the closest hit.
func finalizeTrace(trace *RayTrace) {
	slices.SortStableFunc(trace.Hits, func(left, right RayHit) int {
		switch {
		case left.Distance < right.Distance:
			return -1
		case left.Distance > right.Distance:
			return 1
		default:
			return 0
		}
	})
	if len(trace.Hits) > 0 {
		closest := trace.Hits[0]
		trace.Closest = &closest
	}
}

func raycastNode(node Node, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	trace.NodesVisited++
	switch current := node.(type) {
	case Mesh:
		raycastMesh(current, parent, ray, opts, trace)
	case *Mesh:
		if current != nil {
			raycastMesh(*current, parent, ray, opts, trace)
		}
	case InstancedMesh:
		raycastInstancedMesh(current, parent, ray, opts, trace)
	case *InstancedMesh:
		if current != nil {
			raycastInstancedMesh(*current, parent, ray, opts, trace)
		}
	case Group:
		raycastNodes(current.Children, combineTransforms(parent, localTransform(current.Position, current.Rotation)), ray, opts, trace)
	case *Group:
		if current != nil {
			raycastNodes(current.Children, combineTransforms(parent, localTransform(current.Position, current.Rotation)), ray, opts, trace)
		}
	case LODGroup:
		raycastLODGroup(current, parent, ray, opts, trace)
	case *LODGroup:
		if current != nil {
			raycastLODGroup(*current, parent, ray, opts, trace)
		}
	case Points:
		raycastPoints(current, parent, ray, opts, trace)
	case *Points:
		if current != nil {
			raycastPoints(*current, parent, ray, opts, trace)
		}
	case Sprite:
		raycastSprite(current, parent, ray, opts, trace)
	case *Sprite:
		if current != nil {
			raycastSprite(*current, parent, ray, opts, trace)
		}
	case Decal:
		raycastMesh(decalAsMesh(current), parent, ray, opts, trace)
	case *Decal:
		if current != nil {
			raycastMesh(decalAsMesh(*current), parent, ray, opts, trace)
		}
	case Model:
		raycastModel(current, parent, ray, opts, trace)
	case *Model:
		if current != nil {
			raycastModel(*current, parent, ray, opts, trace)
		}
	}
}

// decalAsMesh mirrors graphLowerer.lowerDecal: a Decal renders as a thin plane,
// so the plane intersection is the exact surface the renderer draws.
func decalAsMesh(decal Decal) Mesh {
	width := positiveOr(decal.Width, 1)
	height := positiveOr(decal.Height, width)
	return Mesh{
		ID:       decal.ID,
		Geometry: PlaneGeometry{Width: width, Height: height},
		Position: decal.Position,
		Rotation: decal.Rotation,
		Pickable: decal.Pickable,
	}
}

func raycastNodes(nodes []Node, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	for _, child := range nodes {
		raycastNode(child, parent, ray, opts, trace)
	}
}

func raycastLODGroup(group LODGroup, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	world := combineTransforms(parent, localTransform(group.Position, group.Rotation))
	for _, level := range group.Levels {
		if level.Node == nil {
			continue
		}
		raycastNode(level.Node, world, ray, opts, trace)
	}
}

func raycastMesh(mesh Mesh, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	pickable := mesh.Pickable == nil || *mesh.Pickable
	world := combineTransforms(parent, localTransform(mesh.Position, mesh.Rotation))
	if opts.PickableOnly && !pickable {
		trace.FilteredPrimitives++
		raycastNodes(mesh.Children, world, ray, opts, trace)
		return
	}
	scale := meshScaleOrUnit(mesh.Scale)
	factor := scaleFactor(scale)
	threshold := pointHitRadius(opts)
	// The pick radius is a world length. Divide it once to reach local space,
	// where every geometry test runs.
	localThreshold := threshold / factor
	radius, strokes := geometryBounds(mesh.Geometry)
	// Broadphase: reject the mesh before the exact test when the ray misses its
	// world-space bounding sphere. This costs about ten arithmetic operations
	// and skips a quaternion inverse plus the geometry intersection.
	if rayMissesSphere(ray, world.Position, radius*factor+strokes*threshold, opts.MaxDistance) {
		trace.BroadphaseRejected++
		raycastNodes(mesh.Children, world, ray, opts, trace)
		return
	}
	trace.PrimitivesTested++
	if hit, ok := raycastTransformedGeometry(mesh.Geometry, world, scale, ray, localThreshold); ok {
		hit.ID = strings.TrimSpace(mesh.ID)
		hit.Pickable = pickable
		appendTraceHit(trace, hit, opts)
	}
	// Children remain independently pickable even when the parent's geometry is
	// missed (or absent from the ray), matching scene graph interaction semantics.
	raycastNodes(mesh.Children, world, ray, opts, trace)
}

func raycastInstancedMesh(mesh InstancedMesh, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	count := instanceCount(mesh)
	pickable := mesh.Pickable == nil || *mesh.Pickable
	if opts.PickableOnly && !pickable {
		trace.FilteredPrimitives += count
		return
	}
	id := strings.TrimSpace(mesh.ID)
	threshold := pointHitRadius(opts)
	localRadius, strokes := geometryBounds(mesh.Geometry)
	for i := 0; i < count; i++ {
		position := vectorAt(mesh.Positions, i, Vector3{})
		scale := sanitizedScale(vectorAt(mesh.Scales, i, sceneUnitScale()))
		factor := scaleFactor(scale)
		localThreshold := threshold / factor
		// Broadphase per instance. Instance rotation cannot move the bounding
		// sphere, so this test needs the world center only. Rejected instances
		// never pay for a quaternion inverse or a geometry intersection.
		center := addVectors(parent.Position, parent.Rotation.rotate(position))
		if rayMissesSphere(ray, center, localRadius*factor+strokes*threshold, opts.MaxDistance) {
			trace.BroadphaseRejected++
			continue
		}
		rotation := eulerAt(mesh.Rotations, i, Euler{})
		world := combineTransforms(parent, localTransform(position, rotation))
		trace.PrimitivesTested++
		trace.InstancesTested++
		if hit, ok := raycastTransformedGeometry(mesh.Geometry, world, scale, ray, localThreshold); ok {
			index := i
			hit.ID = id
			hit.Pickable = pickable
			hit.InstanceIndex = &index
			appendTraceHit(trace, hit, opts)
		}
	}
}

// instanceCount reports how many instances an InstancedMesh draws.
func instanceCount(mesh InstancedMesh) int {
	if mesh.Count > 0 {
		return mesh.Count
	}
	return len(mesh.Positions)
}

// raycastPoints tests every particle of a Points cloud as a small sphere. The
// radius comes from RaycastOptions.PointThreshold because the renderer sizes
// particles in screen pixels, not world units. Each intersected particle emits
// one hit and reports its index through RayHit.InstanceIndex.
//
// The browser pick path does NOT cover points yet. sceneRaycastPick in
// client/js/bootstrap-src/17-scene-input.js walks bundle.meshObjects,
// bundle.instancedMeshes, and bundle.objects only; bundle.points is a separate
// array it never reads. The WebGPU GPU picker resolves identity on the GPU and
// then calls the same two shared helpers for every geometric field, so it
// inherits that gap. See TestRaycastCoverageManifest for the current split.
func raycastPoints(points Points, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	count := points.Count
	if count <= 0 {
		count = len(points.Positions)
	}
	if count > len(points.Positions) {
		count = len(points.Positions)
	}
	if count == 0 {
		return
	}
	world := combineTransforms(parent, localTransform(points.Position, points.Rotation))
	radius := pointHitRadius(opts)
	id := strings.TrimSpace(points.ID)
	for i := 0; i < count; i++ {
		center := addVectors(world.Position, world.Rotation.rotate(points.Positions[i]))
		if rayMissesSphere(ray, center, radius, opts.MaxDistance) {
			trace.BroadphaseRejected++
			continue
		}
		trace.PrimitivesTested++
		hit, ok := intersectWorldSphere(ray, center, radius)
		if !ok {
			continue
		}
		index := i
		hit.ID = id
		hit.Kind = "points"
		hit.Pickable = true
		hit.Method = "point-threshold"
		hit.InstanceIndex = &index
		appendTraceHit(trace, hit, opts)
	}
}

// raycastSprite tests a Sprite billboard as a threshold sphere at its world
// position. Sprite sizes are CSS pixels on a DOM overlay, so the sprite has no
// world extent to intersect exactly. Anchored sprites (Target set) resolve
// their position against another node at lower time and are skipped here.
//
// The browser pick path does NOT cover sprites either. A sprite lowers to a DOM
// overlay, so the browser picks it through pointer events on that element, not
// through a ray. See the raycastPoints comment above for the shared helpers.
func raycastSprite(sprite Sprite, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	if strings.TrimSpace(sprite.Target) != "" {
		trace.FilteredPrimitives++
		return
	}
	center := addVectors(parent.Position, parent.Rotation.rotate(sprite.Position))
	radius := pointHitRadius(opts) * spriteRadiusScale(sprite)
	if rayMissesSphere(ray, center, radius, opts.MaxDistance) {
		trace.BroadphaseRejected++
		return
	}
	trace.PrimitivesTested++
	hit, ok := intersectWorldSphere(ray, center, radius)
	if !ok {
		return
	}
	hit.ID = strings.TrimSpace(sprite.ID)
	hit.Kind = "sprite"
	hit.Pickable = true
	hit.Method = "point-threshold"
	appendTraceHit(trace, hit, opts)
}

// raycastModel tests a Model against the axis-aligned box implied by its
// Bounds fit size. Model geometry lives in an external glTF asset that Go never
// reads, so Bounds is the only world extent available. A Model without Bounds
// stays unpickable in native raycasts.
func raycastModel(model Model, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	if model.Bounds <= 0 {
		trace.FilteredPrimitives++
		return
	}
	pickable := model.Pickable == nil || *model.Pickable
	if opts.PickableOnly && !pickable {
		trace.FilteredPrimitives++
		return
	}
	world := combineTransforms(parent, localTransform(model.Position, model.Rotation))
	scale := meshScaleOrUnit(model.Scale)
	half := model.Bounds / 2
	if rayMissesSphere(ray, world.Position, half*math.Sqrt(3)*maxAbsComponent(scale), opts.MaxDistance) {
		trace.BroadphaseRejected++
		return
	}
	trace.PrimitivesTested++
	geometry := BoxGeometry{Width: model.Bounds, Height: model.Bounds, Depth: model.Bounds}
	hit, ok := raycastTransformedGeometry(geometry, world, scale, ray, 0)
	if !ok {
		return
	}
	hit.ID = strings.TrimSpace(model.ID)
	hit.Kind = "model"
	hit.Pickable = pickable
	hit.Method = "bounds-aabb"
	appendTraceHit(trace, hit, opts)
}

// pointHitRadius resolves the world-space hit radius for zero-extent primitives.
func pointHitRadius(opts RaycastOptions) float64 {
	if opts.PointThreshold > 0 {
		return opts.PointThreshold
	}
	return DefaultPointThreshold
}

// spriteRadiusScale grows the sprite hit radius with its authored Scale so
// larger billboards stay easier to pick.
func spriteRadiusScale(sprite Sprite) float64 {
	if sprite.Scale > 0 {
		return sprite.Scale
	}
	return 1
}

// intersectWorldSphere intersects a world-space sphere without moving the ray
// into local space. Points and sprites need no transform inverse.
func intersectWorldSphere(ray Ray, center Vector3, radius float64) (RayHit, bool) {
	oc := subVectors(ray.Origin, center)
	b := dotVector(oc, ray.Direction)
	c := dotVector(oc, oc) - radius*radius
	discriminant := b*b - c
	if discriminant < 0 {
		return RayHit{}, false
	}
	root := math.Sqrt(discriminant)
	t := -b - root
	if t < 0 {
		t = -b + root
	}
	if t < 0 {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
	return RayHit{Distance: t, Point: point, Normal: normalizeVector(subVectors(point, center))}, true
}

// rayMissesSphere reports whether the ray cannot reach a world-space sphere.
// The test needs no square root: it compares squared distances and rejects
// spheres that sit behind the origin or past MaxDistance. ray.Direction must
// already be a unit vector.
func rayMissesSphere(ray Ray, center Vector3, radius, maxDistance float64) bool {
	if radius <= 0 {
		return true
	}
	oc := subVectors(center, ray.Origin)
	along := dotVector(oc, ray.Direction)
	squaredRadius := radius * radius
	squaredCenter := dotVector(oc, oc)
	// The whole sphere sits behind the ray origin.
	if along < 0 && squaredCenter > squaredRadius {
		return true
	}
	// The ray line passes outside the sphere.
	if squaredCenter-along*along > squaredRadius {
		return true
	}
	// The nearest reachable point on the sphere lies past the distance cap.
	if maxDistance > 0 && along-radius > maxDistance {
		return true
	}
	return false
}

// maxAbsComponent returns the largest absolute component of a scale vector. A
// bounding sphere grows by this factor under any non-uniform scale.
func maxAbsComponent(scale Vector3) float64 {
	return math.Max(math.Abs(scale.X), math.Max(math.Abs(scale.Y), math.Abs(scale.Z)))
}

// geometryBounds returns the local-space bounding sphere radius of a geometry
// centered on the node origin, plus how many pick radii that geometry needs on
// top of it. Every builtin geometry is origin centered, so a single radius
// bounds it. The radius must never understate the geometry or the broadphase
// would drop real hits.
//
// Lines strokes are the only geometry that reports a stroke count. A polyline
// has no cross-section, so its pick volume grows by one pick radius around every
// segment. Callers combine the two numbers as
// radius*maxScale + strokes*worldThreshold, which bounds the swept volume
// exactly. Returning both from one type switch lets an instanced mesh resolve
// the switch once and reuse it for every instance.
func geometryBounds(geometry Geometry) (radius float64, strokes float64) {
	switch g := geometry.(type) {
	case SphereGeometry:
		return positiveOr(g.Radius, 1), 0
	case TorusGeometry:
		return torusRadius(g) + torusTube(g), 0
	case TorusKnotGeometry:
		// The knot center curve reaches 1.5*radius from the origin, at the turn
		// where the curve leaves the Z axis, so the tube adds its whole radius.
		return torusKnotRadius(g)*1.5 + torusKnotTube(g), 0
	case LinesGeometry:
		min, max := lineBounds(g)
		return boxCornerRadius(min, max), 1
	case CubeGeometry:
		size := positiveOr(g.Size, 1)
		return size / 2 * math.Sqrt(3), 0
	case BoxGeometry:
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		return boxCornerRadius(min, max), 0
	case PlaneGeometry:
		width := positiveOr(g.Width, 1)
		height := positiveOr(g.Height, 1)
		return math.Hypot(width, height) / 2, 0
	case PyramidGeometry:
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		return boxCornerRadius(min, max), 0
	case CylinderGeometry:
		radiusTop, radiusBottom := cylinderRadii(g.RadiusTop, g.RadiusBottom)
		return math.Hypot(math.Max(radiusTop, radiusBottom), positiveOr(g.Height, 1)/2), 0
	case BufferGeometry:
		return bufferBoundingRadius(g), 0
	case *BufferGeometry:
		if g == nil {
			return 0, 0
		}
		return bufferBoundingRadius(*g), 0
	default:
		// raycastGeometry falls back to a unit cube for unknown geometries.
		return math.Sqrt(3) / 2, 0
	}
}

// bufferBoundingRadius returns the farthest vertex distance from the origin. A
// triangle mesh carries its own extent, so the bound has to read the vertices.
func bufferBoundingRadius(geometry BufferGeometry) float64 {
	longest := 0.0
	positions := geometry.Positions
	for index := 0; index+3 <= len(positions); index += 3 {
		x, y, z := positions[index], positions[index+1], positions[index+2]
		if squared := x*x + y*y + z*z; squared > longest {
			longest = squared
		}
	}
	return math.Sqrt(longest)
}

// scaleFactor returns the factor that converts a local length into the longest
// world length the scale can produce. It never returns zero, so callers can
// divide a world threshold by it.
func scaleFactor(scale Vector3) float64 {
	factor := maxAbsComponent(scale)
	if factor <= 0 {
		return 1
	}
	return factor
}

// boxCornerRadius returns the radius of the sphere that encloses a box.
func boxCornerRadius(min, max Vector3) float64 {
	x := math.Max(math.Abs(min.X), math.Abs(max.X))
	y := math.Max(math.Abs(min.Y), math.Abs(max.Y))
	z := math.Max(math.Abs(min.Z), math.Abs(max.Z))
	return math.Sqrt(x*x + y*y + z*z)
}

// raycastTransformedGeometry runs the exact geometry test in local space.
// localThreshold is the pick radius in local units, which only Lines strokes
// read.
func raycastTransformedGeometry(geometry Geometry, world worldTransform, scale Vector3, ray Ray, localThreshold float64) (RayHit, bool) {
	localRay := localRayFor(world, scale, ray)
	localHit, kind, ok := raycastGeometry(geometry, localRay, localThreshold)
	if !ok {
		return RayHit{}, false
	}
	return worldHitFor(localHit, kind, world, scale, ray), true
}

// localRayFor moves a world ray into the local space of one node.
func localRayFor(world worldTransform, scale Vector3, ray Ray) Ray {
	inv := world.Rotation.conjugate().normalized()
	return Ray{
		Origin:    divideVector(inv.rotate(subVectors(ray.Origin, world.Position)), scale),
		Direction: normalizeVector(divideVector(inv.rotate(ray.Direction), scale)),
	}
}

// worldHitFor lifts a local-space hit back into world space. The distance comes
// from the world point, so a non-uniform scale reports the true world distance.
func worldHitFor(localHit RayHit, kind string, world worldTransform, scale Vector3, ray Ray) RayHit {
	point := addVectors(world.Position, world.Rotation.rotate(multiplyVector(localHit.Point, scale)))
	normal := normalizeVector(world.Rotation.rotate(divideVector(localHit.Normal, scale)))
	return RayHit{
		Kind:     kind,
		Distance: vectorLength(subVectors(point, ray.Origin)),
		Point:    point,
		Normal:   normal,
		Method:   localHit.Method,
	}
}

func appendTraceHit(trace *RayTrace, hit RayHit, opts RaycastOptions) {
	if opts.MaxDistance <= 0 || hit.Distance <= opts.MaxDistance {
		trace.Hits = append(trace.Hits, hit)
	}
}

// raycastGeometry runs the exact intersection for one geometry in its own local
// space. localThreshold is the pick radius in local units for geometries that
// have no cross-section. The returned Method names the routine that ran, so a
// trace stays honest about how exact the answer is.
//
// A geometry with a closed-form solution keeps it. An analytic sphere, box,
// plane, frustum, pyramid or torus answers for the ideal surface, which is more
// exact than any tessellation of it.
//
// Every other surface goes through Tessellate, the shared generator in package
// scene/geom. The torus knot is the one built-in body with no closed form, so its
// soup comes from that call; see torusKnotSoup. Adding a parametric geometry with
// no closed form means adding a generator to scene/geom and a case to
// geometryParams, not a second tessellator here. A second copy is what let the
// native renderer draw a different scene than the browser.
func raycastGeometry(geometry Geometry, ray Ray, localThreshold float64) (RayHit, string, bool) {
	switch g := geometry.(type) {
	case SphereGeometry:
		radius := positiveOr(g.Radius, 1)
		hit, ok := intersectSphere(ray, radius)
		hit.Method = "analytic-sphere"
		return hit, "sphere", ok
	case TorusGeometry:
		hit, ok := intersectTorus(ray, torusRadius(g), torusTube(g))
		hit.Method = "analytic-torus"
		return hit, "torus", ok
	case TorusKnotGeometry:
		// The knot has no closed form, so the test runs against the triangles the
		// renderer draws. torusKnotSoup builds them through Tessellate and caches
		// the hierarchy, so the picker and the renderer read one tessellation.
		soup := torusKnotSoup(g)
		hit, ok := soup.intersect(ray)
		hit.Method = "tessellated-triangle"
		return hit, "torusknot", ok
	case LinesGeometry:
		hit, ok := intersectLines(ray, g, localThreshold)
		hit.Method = "line-threshold"
		return hit, "lines", ok
	case BufferGeometry:
		soup := soupOfBuffer(g)
		hit, ok := soup.intersect(ray)
		hit.Method = "mesh-triangle"
		return hit, "gltf-mesh", ok
	case *BufferGeometry:
		if g == nil {
			return RayHit{}, "gltf-mesh", false
		}
		soup := soupOfBuffer(*g)
		hit, ok := soup.intersect(ray)
		hit.Method = "mesh-triangle"
		return hit, "gltf-mesh", ok
	case *triangleSoup:
		// SceneAccelerator swaps a triangle mesh for its prebuilt hierarchy. The
		// triangles are the same, so the hit is the same as the walk reports.
		hit, ok := g.intersect(ray)
		hit.Method = "mesh-triangle"
		return hit, "gltf-mesh", ok
	case *segmentSoup:
		// The same swap for a polyline. The strokes are the same, and both paths
		// keep the nearest one with the same tie order.
		hit, ok := g.intersect(ray, localThreshold)
		hit.Method = "line-threshold"
		return hit, "lines", ok
	case CubeGeometry:
		size := positiveOr(g.Size, 1)
		hit, ok := intersectAABB(ray, Vector3{X: -size / 2, Y: -size / 2, Z: -size / 2}, Vector3{X: size / 2, Y: size / 2, Z: size / 2})
		hit.Method = "analytic-aabb"
		return hit, "cube", ok
	case BoxGeometry:
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		hit, ok := intersectAABB(ray, min, max)
		hit.Method = "analytic-aabb"
		return hit, "box", ok
	case PlaneGeometry:
		hit, ok := intersectPlane(ray, positiveOr(g.Width, 1), positiveOr(g.Height, 1))
		hit.Method = "analytic-plane"
		return hit, "plane", ok
	case PyramidGeometry:
		hit, ok := intersectPyramid(ray, positiveOr(g.Width, 1), positiveOr(g.Height, 1), positiveOr(g.Depth, 1))
		hit.Method = "analytic-pyramid"
		return hit, "pyramid", ok
	case CylinderGeometry:
		radiusTop, radiusBottom := cylinderRadii(g.RadiusTop, g.RadiusBottom)
		hit, ok := intersectCylinder(ray, radiusTop, radiusBottom, positiveOr(g.Height, 1))
		hit.Method = "analytic-frustum"
		return hit, "cylinder", ok
	default:
		hit, ok := intersectAABB(ray, Vector3{X: -0.5, Y: -0.5, Z: -0.5}, Vector3{X: 0.5, Y: 0.5, Z: 0.5})
		hit.Method = "fallback-aabb"
		return hit, "cube", ok
	}
}

func intersectSphere(ray Ray, radius float64) (RayHit, bool) {
	oc := ray.Origin
	b := dotVector(oc, ray.Direction)
	c := dotVector(oc, oc) - radius*radius
	discriminant := b*b - c
	if discriminant < 0 {
		return RayHit{}, false
	}
	root := math.Sqrt(discriminant)
	t := -b - root
	if t < 0 {
		t = -b + root
	}
	if t < 0 {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
	return RayHit{Distance: t, Point: point, Normal: normalizeVector(point)}, true
}

func intersectPlane(ray Ray, width, height float64) (RayHit, bool) {
	const epsilon = 1e-9
	if math.Abs(ray.Direction.Z) < epsilon {
		return RayHit{}, false
	}
	t := -ray.Origin.Z / ray.Direction.Z
	if t < 0 {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
	if math.Abs(point.X) > width/2 || math.Abs(point.Y) > height/2 {
		return RayHit{}, false
	}
	normal := Vector3{Z: 1}
	if ray.Direction.Z > 0 {
		normal.Z = -1
	}
	return RayHit{Distance: t, Point: point, Normal: normal}, true
}

// intersectCylinder solves a finite Y-axis cylinder or truncated cone,
// including both end caps. This rejects the large false-positive corner
// regions produced by the former bounding-box approximation.
func intersectCylinder(ray Ray, radiusTop, radiusBottom, height float64) (RayHit, bool) {
	const epsilon = 1e-9
	half := height / 2
	slope := (radiusTop - radiusBottom) / height
	radiusAtOrigin := radiusBottom + slope*(ray.Origin.Y+half)
	a := ray.Direction.X*ray.Direction.X + ray.Direction.Z*ray.Direction.Z - slope*slope*ray.Direction.Y*ray.Direction.Y
	b := 2 * (ray.Origin.X*ray.Direction.X + ray.Origin.Z*ray.Direction.Z - radiusAtOrigin*slope*ray.Direction.Y)
	c := ray.Origin.X*ray.Origin.X + ray.Origin.Z*ray.Origin.Z - radiusAtOrigin*radiusAtOrigin

	bestT := math.Inf(1)
	bestNormal := Vector3{}
	considerSide := func(t float64) {
		if t < 0 || t >= bestT {
			return
		}
		point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
		if point.Y < -half-epsilon || point.Y > half+epsilon {
			return
		}
		radius := radiusBottom + slope*(point.Y+half)
		bestT = t
		bestNormal = normalizeVector(Vector3{X: point.X, Y: -radius * slope, Z: point.Z})
	}
	if math.Abs(a) < epsilon {
		if math.Abs(b) >= epsilon {
			considerSide(-c / b)
		}
	} else if discriminant := b*b - 4*a*c; discriminant >= 0 {
		root := math.Sqrt(discriminant)
		t0, t1 := (-b-root)/(2*a), (-b+root)/(2*a)
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		considerSide(t0)
		considerSide(t1)
	}

	considerCap := func(y, radius float64, normal Vector3) {
		if math.Abs(ray.Direction.Y) < epsilon {
			return
		}
		t := (y - ray.Origin.Y) / ray.Direction.Y
		if t < 0 || t >= bestT {
			return
		}
		point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
		if point.X*point.X+point.Z*point.Z <= radius*radius+epsilon {
			bestT, bestNormal = t, normal
		}
	}
	considerCap(-half, radiusBottom, Vector3{Y: -1})
	considerCap(half, radiusTop, Vector3{Y: 1})
	if math.IsInf(bestT, 1) {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, bestT))
	return RayHit{Distance: bestT, Point: point, Normal: bestNormal}, true
}

// torusRadius and torusTube resolve the authored torus size. The defaults match
// the renderer, which draws a zero field as radius 0.7 and tube 0.3. An exact
// test against the wrong size would not be exact, so the two paths must agree.
func torusRadius(geometry TorusGeometry) float64 { return positiveOr(geometry.Radius, 0.7) }

func torusTube(geometry TorusGeometry) float64 { return positiveOr(geometry.Tube, 0.3) }

// intersectTorus solves the exact ray/torus quartic. The torus rings around the
// Y axis and lies in the XZ plane, which is the surface the renderer builds:
//
//	P(u,v) = ((R + t*cos v)*cos u, t*sin v, (R + t*cos v)*sin u)
//
// The same surface in implicit form is
//
//	(x² + y² + z² + R² - t²)² = 4R²(x² + z²)
//
// Substituting the ray gives a quartic in the ray parameter. The solver first
// slides the parameter origin to the point of the ray nearest the torus center.
// That drops the cubic term, keeps every coefficient small, and bounds the
// search to the chord the ray cuts through the bounding sphere, so the root
// finder stays accurate and needs few steps.
//
// ray.Direction must be a unit vector.
func intersectTorus(ray Ray, radius, tube float64) (RayHit, bool) {
	if radius <= 0 || tube <= 0 {
		return RayHit{}, false
	}
	outer := radius + tube
	shift := -dotVector(ray.Origin, ray.Direction)
	nearest := addVectors(ray.Origin, scaleVector(ray.Direction, shift))
	offset := dotVector(nearest, nearest)
	chord := outer*outer - offset
	if chord <= 0 {
		// The ray line stays outside the bounding sphere of the torus.
		return RayHit{}, false
	}
	// Push the bracket a hair outside the bounding sphere. A ray in the plane of
	// the ring meets the surface exactly where it meets that sphere, because the
	// outer equator touches the sphere there. An endpoint that lands on the
	// surface reads as zero, which would hide the crossing from the sign scan
	// below. Widening the bracket cannot invent a hit: every real root of the
	// quartic is a point of the surface, and the surface stays inside the sphere.
	half := math.Sqrt(chord) + 1e-6*(outer+1)
	low, high := -half, half
	// The ray parameter may not go below zero, so the shifted parameter may not
	// go below -shift.
	if low < -shift {
		low = -shift
	}
	if low > high {
		return RayHit{}, false
	}

	// Depressed quartic s⁴ + second*s² + first*s + constant in the shifted
	// parameter. The cubic term vanishes because the shifted origin is
	// perpendicular to the direction.
	spread := offset + radius*radius - tube*tube
	planar := nearest.X*nearest.X + nearest.Z*nearest.Z
	along := nearest.X*ray.Direction.X + nearest.Z*ray.Direction.Z
	slab := ray.Direction.X*ray.Direction.X + ray.Direction.Z*ray.Direction.Z
	ring := 4 * radius * radius
	second := 2*spread - ring*slab
	first := -2 * ring * along
	constant := spread*spread - ring*planar

	root, ok := smallestQuarticRoot(second, first, constant, low, high)
	if !ok {
		return RayHit{}, false
	}
	// The bracket starts at or after the ray origin, so the root cannot sit behind
	// it. The clamp only removes a rounding error in the sum.
	distance := math.Max(0, root+shift)
	point := addVectors(ray.Origin, scaleVector(ray.Direction, distance))
	return RayHit{Distance: distance, Point: point, Normal: torusNormal(point, radius)}, true
}

// torusNormal returns the outward surface normal at a point on the torus. The
// nearest point on the center circle sits at radius R in the XZ plane, so the
// normal points away from it.
func torusNormal(point Vector3, radius float64) Vector3 {
	planar := math.Hypot(point.X, point.Z)
	if planar < 1e-12 {
		// Only a self-intersecting torus (tube >= radius) reaches the Y axis.
		if point.Y < 0 {
			return Vector3{Y: -1}
		}
		return Vector3{Y: 1}
	}
	pull := radius / planar
	return normalizeVector(Vector3{X: point.X * (1 - pull), Y: point.Y, Z: point.Z * (1 - pull)})
}

// smallestQuarticRoot returns the smallest root of
// s⁴ + second*s² + first*s + constant inside [low, high].
//
// The derivative 4s³ + 2*second*s + first is a depressed cubic. Its real roots
// split the interval into pieces on which the quartic runs one way only, so one
// sign change per piece finds every root, and scanning the pieces in order finds
// the smallest one first.
func smallestQuarticRoot(second, first, constant, low, high float64) (float64, bool) {
	var turns [3]float64
	turnCount := depressedCubicRoots(second/2, first/4, &turns)

	var edges [5]float64
	edges[0] = low
	count := 1
	for i := 0; i < turnCount; i++ {
		if turns[i] > low && turns[i] < high {
			edges[count] = turns[i]
			count++
		}
	}
	edges[count] = high
	count++

	previous := edges[0]
	previousValue := quarticAt(second, first, constant, previous)
	if previousValue == 0 {
		return previous, true
	}
	for i := 1; i < count; i++ {
		current := edges[i]
		currentValue := quarticAt(second, first, constant, current)
		if currentValue == 0 {
			return current, true
		}
		if (previousValue < 0) != (currentValue < 0) {
			return polishQuarticRoot(second, first, constant, previous, previousValue, current), true
		}
		previous, previousValue = current, currentValue
	}
	return 0, false
}

// quarticAt evaluates s⁴ + second*s² + first*s + constant.
func quarticAt(second, first, constant, s float64) float64 {
	return ((s*s+second)*s+first)*s + constant
}

// quarticSlopeAt evaluates 4s³ + 2*second*s + first.
func quarticSlopeAt(second, first, s float64) float64 {
	return (4*s*s+2*second)*s + first
}

// polishQuarticRoot finds the single root inside a bracket that holds one sign
// change. It steps with Newton and falls back to bisection whenever a step
// leaves the bracket, so it keeps Newton's speed and bisection's guarantee.
func polishQuarticRoot(second, first, constant, low, lowValue, high float64) float64 {
	lowIsNegative := lowValue < 0
	guess := (low + high) / 2
	for i := 0; i < 40; i++ {
		value := quarticAt(second, first, constant, guess)
		if value == 0 {
			return guess
		}
		if (value < 0) == lowIsNegative {
			low = guess
		} else {
			high = guess
		}
		next := guess
		if slope := quarticSlopeAt(second, first, guess); slope != 0 {
			next = guess - value/slope
		}
		if !(next > low && next < high) {
			next = (low + high) / 2
		}
		if next == guess || math.Abs(next-guess) <= 1e-16*(1+math.Abs(guess)) {
			return next
		}
		guess = next
	}
	return guess
}

// depressedCubicRoots writes the real roots of s³ + p*s + q into out, in
// increasing order, and returns how many it wrote.
func depressedCubicRoots(p, q float64, out *[3]float64) int {
	if p == 0 {
		out[0] = -math.Cbrt(q)
		return 1
	}
	discriminant := q*q/4 + p*p*p/27
	if discriminant > 0 {
		// One real root. Cardano's formula needs no complex arithmetic here.
		root := math.Sqrt(discriminant)
		out[0] = math.Cbrt(-q/2+root) + math.Cbrt(-q/2-root)
		return 1
	}
	// Three real roots, two of which may coincide. p is negative in this branch,
	// so the trigonometric form applies and avoids complex arithmetic.
	spread := 2 * math.Sqrt(-p/3)
	argument := 3 * q / (p * spread)
	if argument < -1 {
		argument = -1
	}
	if argument > 1 {
		argument = 1
	}
	angle := math.Acos(argument) / 3
	const third = 2 * math.Pi / 3
	out[0] = spread * math.Cos(angle)
	out[1] = spread * math.Cos(angle-third)
	out[2] = spread * math.Cos(angle-2*third)
	if out[0] > out[1] {
		out[0], out[1] = out[1], out[0]
	}
	if out[1] > out[2] {
		out[1], out[2] = out[2], out[1]
	}
	if out[0] > out[1] {
		out[0], out[1] = out[1], out[0]
	}
	return 3
}

// pyramidVertices numbers the five corners of a pyramid: four base corners and
// the apex.
const pyramidApex = 4

// pyramidFaces winds the four side triangles and the two base triangles so that
// the cross product of the first two edges points out of the solid. The order
// matches generateInstancedPyramidGeometry in the WebGL renderer.
var pyramidFaces = [6][3]int{
	{0, 1, 2},
	{0, 2, 3},
	{0, pyramidApex, 1},
	{1, pyramidApex, 2},
	{2, pyramidApex, 3},
	{3, pyramidApex, 0},
}

// intersectPyramid solves the exact ray/pyramid intersection. A pyramid is a
// square base plus four triangular sides, so six triangle tests cover the whole
// surface. The former bounding-box test reported the corner regions above the
// slanted faces as hits; this one does not.
func intersectPyramid(ray Ray, width, height, depth float64) (RayHit, bool) {
	halfWidth, halfHeight, halfDepth := width/2, height/2, depth/2
	corners := [5]Vector3{
		{X: -halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: -halfWidth, Y: -halfHeight, Z: halfDepth},
		{Y: halfHeight},
	}
	best := math.Inf(1)
	found := false
	var bestNormal Vector3
	for _, face := range pyramidFaces {
		distance, normal, ok := triangleHit(ray, corners[face[0]], corners[face[1]], corners[face[2]])
		if !ok || distance >= best {
			continue
		}
		best, bestNormal, found = distance, normal, true
	}
	if !found {
		return RayHit{}, false
	}
	return surfaceHit(ray, best, bestNormal), true
}

// surfaceHit builds the local hit for a triangle surface. The normal turns to
// face the ray, which matches the plane and box tests: a hit reports the side
// the ray came from.
func surfaceHit(ray Ray, distance float64, normal Vector3) RayHit {
	if dotVector(normal, ray.Direction) > 0 {
		normal = scaleVector(normal, -1)
	}
	return RayHit{
		Distance: distance,
		Point:    addVectors(ray.Origin, scaleVector(ray.Direction, distance)),
		Normal:   normalizeVector(normal),
	}
}

// triangleHit runs the Möller-Trumbore ray/triangle test and returns the ray
// parameter and the unnormalized face normal. It accepts both faces, because a
// ray can start inside a closed mesh. A degenerate triangle never reports a hit.
func triangleHit(ray Ray, first, second, third Vector3) (float64, Vector3, bool) {
	edge1 := subVectors(second, first)
	edge2 := subVectors(third, first)
	normal := crossVector(edge1, edge2)
	area := dotVector(normal, normal)
	if area <= 0 {
		return 0, Vector3{}, false
	}
	determinant := -dotVector(ray.Direction, normal)
	// Reject a ray that runs along the triangle plane. The test scales with the
	// triangle size, so small triangles keep their tolerance.
	if determinant*determinant < 1e-24*area {
		return 0, Vector3{}, false
	}
	inverse := 1 / determinant
	toFirst := subVectors(ray.Origin, first)
	distance := dotVector(toFirst, normal) * inverse
	if distance < 0 {
		return 0, Vector3{}, false
	}
	swept := crossVector(toFirst, ray.Direction)
	u := dotVector(edge2, swept) * inverse
	if u < 0 {
		return 0, Vector3{}, false
	}
	v := -dotVector(edge1, swept) * inverse
	if v < 0 || u+v > 1 {
		return 0, Vector3{}, false
	}
	return distance, normal, true
}

// intersectLines tests a polyline against the ray. A stroke has no world
// thickness, so each segment picks as a capsule of radius threshold, the same
// model Points particles use with their threshold spheres. The accepted rays are
// exactly the rays whose closest approach to a segment stays within the
// threshold, which is what three.js Line.raycast reports. The capsule form also
// puts the hit point on the swept surface and the distance along the ray.
//
// Segment selection mirrors sceneLineSegments in the runtime: it drops pairs
// that repeat an index or reach outside the point list, and it falls back to a
// connected polyline when no pair survives.
func intersectLines(ray Ray, geometry LinesGeometry, threshold float64) (RayHit, bool) {
	points := geometry.Points
	if threshold <= 0 || len(points) < 2 {
		return RayHit{}, false
	}
	search := strokeSearch{ray: ray, threshold: threshold, best: math.Inf(1), index: -1}
	index := 0
	for _, pair := range geometry.Segments {
		if !lineSegmentIsDrawn(pair, len(points)) {
			continue
		}
		search.consider(index, points[pair[0]], points[pair[1]])
		index++
	}
	if index == 0 {
		for from := 0; from+1 < len(points); from++ {
			search.consider(from, points[from], points[from+1])
		}
	}
	return search.hit()
}

// strokeSearch keeps the nearest stroke a ray reaches. Both the linear scan and
// the accelerated hierarchy drive it, so the two paths pick the same segment: the
// nearest one, and on a tie the one drawn first.
type strokeSearch struct {
	ray       Ray
	threshold float64
	best      float64
	index     int
	from      Vector3
	to        Vector3
}

func (s *strokeSearch) consider(index int, from, to Vector3) {
	// Per-segment broadphase. A polyline holds many segments and a ray crosses one
	// or two, so this reject decides the cost. The sphere around the segment
	// midpoint bounds the capsule, and 2*(halfLength² + threshold²) bounds that
	// sphere's squared radius, so the whole test needs no square root.
	axis := subVectors(to, from)
	middle := scaleVector(addVectors(from, to), 0.5)
	reachSquared := 2 * (dotVector(axis, axis)/4 + s.threshold*s.threshold)
	toMiddle := subVectors(middle, s.ray.Origin)
	along := dotVector(toMiddle, s.ray.Direction)
	gap := dotVector(toMiddle, toMiddle)
	if along < 0 && gap > reachSquared {
		// The whole capsule sits behind the ray origin.
		return
	}
	if gap-along*along > reachSquared {
		// The ray line passes outside the capsule.
		return
	}
	if along > s.best {
		// The capsule starts behind the nearest stroke found so far.
		if excess := along - s.best; excess*excess > reachSquared {
			return
		}
	}
	distance, ok := intersectSegmentCapsule(s.ray, from, to, s.threshold)
	if !ok {
		return
	}
	if distance > s.best || (distance == s.best && index >= s.index) {
		return
	}
	s.best, s.index, s.from, s.to = distance, index, from, to
}

func (s *strokeSearch) hit() (RayHit, bool) {
	if s.index < 0 {
		return RayHit{}, false
	}
	point := addVectors(s.ray.Origin, scaleVector(s.ray.Direction, s.best))
	return RayHit{
		Distance: s.best,
		Point:    point,
		Normal:   normalizeVector(subVectors(point, closestPointOnSegment(point, s.from, s.to))),
	}, true
}

// lineSegmentIsDrawn reports whether the runtime draws one index pair.
func lineSegmentIsDrawn(pair [2]int, points int) bool {
	if pair[0] == pair[1] || pair[0] < 0 || pair[1] < 0 {
		return false
	}
	return pair[0] < points && pair[1] < points
}

// closestPointOnSegment returns the point of the segment nearest to a point.
func closestPointOnSegment(point, from, to Vector3) Vector3 {
	axis := subVectors(to, from)
	length := dotVector(axis, axis)
	if length <= 0 {
		return from
	}
	span := dotVector(subVectors(point, from), axis) / length
	if span < 0 {
		span = 0
	}
	if span > 1 {
		span = 1
	}
	return addVectors(from, scaleVector(axis, span))
}

// intersectSegmentCapsule returns the nearest ray parameter at which the ray
// meets the capsule of radius around a segment. The capsule is the side surface
// of the cylinder around the segment axis plus a sphere at each end, so the test
// covers the whole swept volume. ray.Direction must be a unit vector.
func intersectSegmentCapsule(ray Ray, from, to Vector3, radius float64) (float64, bool) {
	axis := subVectors(to, from)
	offset := subVectors(ray.Origin, from)
	length := dotVector(axis, axis)
	axisAlongRay := dotVector(axis, ray.Direction)
	axisAtOrigin := dotVector(axis, offset)
	best := math.Inf(1)
	found := false

	// Side surface. Multiplying the cylinder equation by the squared axis length
	// keeps the coefficients free of a divide.
	sideA := length - axisAlongRay*axisAlongRay
	if sideA > 0 {
		sideB := length*dotVector(offset, ray.Direction) - axisAtOrigin*axisAlongRay
		sideC := length*(dotVector(offset, offset)-radius*radius) - axisAtOrigin*axisAtOrigin
		if discriminant := sideB*sideB - sideA*sideC; discriminant >= 0 {
			root := math.Sqrt(discriminant)
			for _, distance := range [2]float64{(-sideB - root) / sideA, (-sideB + root) / sideA} {
				if distance < 0 || distance >= best {
					continue
				}
				span := axisAtOrigin + distance*axisAlongRay
				if span >= 0 && span <= length {
					best, found = distance, true
				}
			}
		}
	}

	// End caps. Each sphere owns the half space beyond its own end, so the two of
	// them add no surface inside the cylinder span.
	if near, far, ok := sphereRoots(ray, from, radius); ok {
		for _, distance := range [2]float64{near, far} {
			if distance < 0 || distance >= best {
				continue
			}
			if axisAtOrigin+distance*axisAlongRay <= 0 {
				best, found = distance, true
			}
		}
	}
	if near, far, ok := sphereRoots(ray, to, radius); ok {
		for _, distance := range [2]float64{near, far} {
			if distance < 0 || distance >= best {
				continue
			}
			if axisAtOrigin+distance*axisAlongRay >= length {
				best, found = distance, true
			}
		}
	}
	return best, found
}

// sphereRoots returns both ray parameters where the ray line meets a sphere.
// ray.Direction must be a unit vector.
func sphereRoots(ray Ray, center Vector3, radius float64) (float64, float64, bool) {
	offset := subVectors(ray.Origin, center)
	along := dotVector(offset, ray.Direction)
	gap := dotVector(offset, offset) - radius*radius
	discriminant := along*along - gap
	if discriminant < 0 {
		return 0, 0, false
	}
	root := math.Sqrt(discriminant)
	return -along - root, -along + root, true
}

func intersectAABB(ray Ray, min, max Vector3) (RayHit, bool) {
	tmin := math.Inf(-1)
	tmax := math.Inf(1)
	normal := Vector3{}
	checkAxis := func(origin, direction, axisMin, axisMax float64, axisNormal Vector3) bool {
		const epsilon = 1e-9
		if math.Abs(direction) < epsilon {
			return origin >= axisMin && origin <= axisMax
		}
		t1 := (axisMin - origin) / direction
		t2 := (axisMax - origin) / direction
		enterNormal := axisNormal
		if t1 > t2 {
			t1, t2 = t2, t1
			enterNormal = scaleVector(axisNormal, -1)
		}
		if t1 > tmin {
			tmin = t1
			normal = enterNormal
		}
		if t2 < tmax {
			tmax = t2
		}
		return tmin <= tmax
	}
	if !checkAxis(ray.Origin.X, ray.Direction.X, min.X, max.X, Vector3{X: -1}) {
		return RayHit{}, false
	}
	if !checkAxis(ray.Origin.Y, ray.Direction.Y, min.Y, max.Y, Vector3{Y: -1}) {
		return RayHit{}, false
	}
	if !checkAxis(ray.Origin.Z, ray.Direction.Z, min.Z, max.Z, Vector3{Z: -1}) {
		return RayHit{}, false
	}
	t := tmin
	if t < 0 {
		t = tmax
		normal = scaleVector(normal, -1)
	}
	if t < 0 {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
	return RayHit{Distance: t, Point: point, Normal: normalizeVector(normal)}, true
}

func rayHitIsCloser(candidate, current RayHit, currentOK bool, opts RaycastOptions) bool {
	if opts.MaxDistance > 0 && candidate.Distance > opts.MaxDistance {
		return false
	}
	return !currentOK || candidate.Distance < current.Distance
}

func boxBounds(width, height, depth float64) (Vector3, Vector3) {
	width = positiveOr(width, 1)
	height = positiveOr(height, 1)
	depth = positiveOr(depth, 1)
	return Vector3{X: -width / 2, Y: -height / 2, Z: -depth / 2}, Vector3{X: width / 2, Y: height / 2, Z: depth / 2}
}

func lineBounds(g LinesGeometry) (Vector3, Vector3) {
	if len(g.Points) == 0 {
		return Vector3{X: -0.5, Y: -0.5, Z: -0.5}, Vector3{X: 0.5, Y: 0.5, Z: 0.5}
	}
	min := g.Points[0]
	max := g.Points[0]
	for _, point := range g.Points[1:] {
		min.X = math.Min(min.X, point.X)
		min.Y = math.Min(min.Y, point.Y)
		min.Z = math.Min(min.Z, point.Z)
		max.X = math.Max(max.X, point.X)
		max.Y = math.Max(max.Y, point.Y)
		max.Z = math.Max(max.Z, point.Z)
	}
	padding := math.Max(0.01, positiveOr(g.Width, 1)*0.01)
	min = subVectors(min, Vector3{X: padding, Y: padding, Z: padding})
	max = addVectors(max, Vector3{X: padding, Y: padding, Z: padding})
	return min, max
}

func positiveOr(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func cylinderRadii(top, bottom float64) (float64, float64) {
	if top <= 0 && bottom <= 0 {
		return 0.5, 0.5
	}
	return math.Max(0, top), math.Max(0, bottom)
}

func subVectors(left, right Vector3) Vector3 {
	return Vector3{X: left.X - right.X, Y: left.Y - right.Y, Z: left.Z - right.Z}
}

func scaleVector(value Vector3, scalar float64) Vector3 {
	return Vector3{X: value.X * scalar, Y: value.Y * scalar, Z: value.Z * scalar}
}

func dotVector(left, right Vector3) float64 {
	return left.X*right.X + left.Y*right.Y + left.Z*right.Z
}

func crossVector(left, right Vector3) Vector3 {
	return Vector3{
		X: left.Y*right.Z - left.Z*right.Y,
		Y: left.Z*right.X - left.X*right.Z,
		Z: left.X*right.Y - left.Y*right.X,
	}
}

func vectorLength(value Vector3) float64 {
	return math.Sqrt(dotVector(value, value))
}

func normalizeVector(value Vector3) Vector3 {
	length := vectorLength(value)
	if length == 0 {
		return Vector3{}
	}
	return scaleVector(value, 1/length)
}

func sceneUnitScale() Vector3 { return Vector3{X: 1, Y: 1, Z: 1} }

// meshScaleOrUnit treats the Mesh.Scale zero value as unit scale so scenes
// authored before leaf scale existed keep their behavior.
func meshScaleOrUnit(scale Vector3) Vector3 {
	if scale == (Vector3{}) {
		return sceneUnitScale()
	}
	return scale
}

func sanitizedScale(scale Vector3) Vector3 {
	if scale.X == 0 {
		scale.X = 1
	}
	if scale.Y == 0 {
		scale.Y = 1
	}
	if scale.Z == 0 {
		scale.Z = 1
	}
	return scale
}

func vectorAt(values []Vector3, index int, fallback Vector3) Vector3 {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return fallback
}

func eulerAt(values []Euler, index int, fallback Euler) Euler {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return fallback
}

func multiplyVector(left, right Vector3) Vector3 {
	return Vector3{X: left.X * right.X, Y: left.Y * right.Y, Z: left.Z * right.Z}
}

func divideVector(left, right Vector3) Vector3 {
	return Vector3{X: left.X / right.X, Y: left.Y / right.Y, Z: left.Z / right.Z}
}
