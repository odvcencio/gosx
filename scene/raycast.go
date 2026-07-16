package scene

import (
	"math"
	"sort"
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
}

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
	Hits               []RayHit       `json:"hits"`
	Closest            *RayHit        `json:"closest,omitempty"`
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
	for _, node := range graph.Nodes {
		raycastNode(node, identityTransform(), ray, opts, &trace)
	}
	sort.SliceStable(trace.Hits, func(i, j int) bool { return trace.Hits[i].Distance < trace.Hits[j].Distance })
	if len(trace.Hits) > 0 {
		closest := trace.Hits[0]
		trace.Closest = &closest
	}
	return trace
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
	trace.PrimitivesTested++
	if hit, ok := raycastTransformedGeometry(mesh.Geometry, world, meshScaleOrUnit(mesh.Scale), ray); ok {
		hit.ID = strings.TrimSpace(mesh.ID)
		hit.Pickable = pickable
		appendTraceHit(trace, hit, opts)
	}
	// Children remain independently pickable even when the parent's geometry is
	// missed (or absent from the ray), matching scene graph interaction semantics.
	raycastNodes(mesh.Children, world, ray, opts, trace)
}

func raycastInstancedMesh(mesh InstancedMesh, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	count := mesh.Count
	if count <= 0 {
		count = len(mesh.Positions)
	}
	pickable := mesh.Pickable == nil || *mesh.Pickable
	if opts.PickableOnly && !pickable {
		trace.FilteredPrimitives += count
		return
	}
	for i := 0; i < count; i++ {
		position := vectorAt(mesh.Positions, i, Vector3{})
		rotation := eulerAt(mesh.Rotations, i, Euler{})
		scale := vectorAt(mesh.Scales, i, sceneUnitScale())
		world := combineTransforms(parent, localTransform(position, rotation))
		trace.PrimitivesTested++
		trace.InstancesTested++
		if hit, ok := raycastTransformedGeometry(mesh.Geometry, world, sanitizedScale(scale), ray); ok {
			index := i
			hit.ID = strings.TrimSpace(mesh.ID)
			hit.Pickable = pickable
			hit.InstanceIndex = &index
			appendTraceHit(trace, hit, opts)
		}
	}
}

func raycastTransformedGeometry(geometry Geometry, world worldTransform, scale Vector3, ray Ray) (RayHit, bool) {
	inv := world.Rotation.conjugate().normalized()
	localRay := Ray{
		Origin:    divideVector(inv.rotate(subVectors(ray.Origin, world.Position)), scale),
		Direction: normalizeVector(divideVector(inv.rotate(ray.Direction), scale)),
	}
	localHit, kind, ok := raycastGeometry(geometry, localRay)
	if !ok {
		return RayHit{}, false
	}
	point := addVectors(world.Position, world.Rotation.rotate(multiplyVector(localHit.Point, scale)))
	normal := normalizeVector(world.Rotation.rotate(divideVector(localHit.Normal, scale)))
	hit := RayHit{
		Kind:     kind,
		Distance: vectorLength(subVectors(point, ray.Origin)),
		Point:    point,
		Normal:   normal,
		Method:   localHit.Method,
	}
	return hit, true
}

func appendTraceHit(trace *RayTrace, hit RayHit, opts RaycastOptions) {
	if opts.MaxDistance <= 0 || hit.Distance <= opts.MaxDistance {
		trace.Hits = append(trace.Hits, hit)
	}
}

func raycastGeometry(geometry Geometry, ray Ray) (RayHit, string, bool) {
	switch g := geometry.(type) {
	case SphereGeometry:
		radius := positiveOr(g.Radius, 1)
		hit, ok := intersectSphere(ray, radius)
		hit.Method = "analytic-sphere"
		return hit, "sphere", ok
	case TorusGeometry:
		radius := positiveOr(g.Radius, 1) + positiveOr(g.Tube, 0.25)
		hit, ok := intersectSphere(ray, radius)
		hit.Method = "bounding-sphere"
		return hit, "torus", ok
	case TorusKnotGeometry:
		// Bounding sphere: envelope ≈ 1.5*radius + tube (from (2+1)/2 * radius + tube).
		radius := positiveOr(g.Radius, 0.17)*1.5 + positiveOr(g.Tube, 0.045)
		hit, ok := intersectSphere(ray, radius)
		hit.Method = "bounding-sphere"
		return hit, "torusknot", ok
	case LinesGeometry:
		min, max := lineBounds(g)
		hit, ok := intersectAABB(ray, min, max)
		hit.Method = "bounds-aabb"
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
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		hit, ok := intersectAABB(ray, min, max)
		hit.Method = "bounds-aabb"
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
