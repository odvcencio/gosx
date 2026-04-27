package scene

import (
	"math"
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
	ID       string  `json:"id,omitempty"`
	Kind     string  `json:"kind,omitempty"`
	Distance float64 `json:"distance"`
	Point    Vector3 `json:"point"`
	Normal   Vector3 `json:"normal,omitzero"`
	Pickable bool    `json:"pickable,omitempty"`
}

// RaycastOptions controls a scene graph ray query.
type RaycastOptions struct {
	PickableOnly bool
	MaxDistance  float64
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
	opts := RaycastOptions{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	ray.Direction = normalizeVector(ray.Direction)
	if ray.Direction == (Vector3{}) {
		return RayHit{}, false
	}
	var closest RayHit
	ok := false
	for _, node := range graph.Nodes {
		if hit, hitOK := raycastNode(node, identityTransform(), ray, opts); hitOK && rayHitIsCloser(hit, closest, ok, opts) {
			closest = hit
			ok = true
		}
	}
	return closest, ok
}

func raycastNode(node Node, parent worldTransform, ray Ray, opts RaycastOptions) (RayHit, bool) {
	switch current := node.(type) {
	case Mesh:
		return raycastMesh(current, parent, ray, opts)
	case *Mesh:
		if current == nil {
			return RayHit{}, false
		}
		return raycastMesh(*current, parent, ray, opts)
	case Group:
		return raycastNodes(current.Children, combineTransforms(parent, localTransform(current.Position, current.Rotation)), ray, opts)
	case *Group:
		if current == nil {
			return RayHit{}, false
		}
		return raycastNodes(current.Children, combineTransforms(parent, localTransform(current.Position, current.Rotation)), ray, opts)
	case LODGroup:
		return raycastLODGroup(current, parent, ray, opts)
	case *LODGroup:
		if current == nil {
			return RayHit{}, false
		}
		return raycastLODGroup(*current, parent, ray, opts)
	default:
		return RayHit{}, false
	}
}

func raycastNodes(nodes []Node, parent worldTransform, ray Ray, opts RaycastOptions) (RayHit, bool) {
	var closest RayHit
	ok := false
	for _, child := range nodes {
		if hit, hitOK := raycastNode(child, parent, ray, opts); hitOK && rayHitIsCloser(hit, closest, ok, opts) {
			closest = hit
			ok = true
		}
	}
	return closest, ok
}

func raycastLODGroup(group LODGroup, parent worldTransform, ray Ray, opts RaycastOptions) (RayHit, bool) {
	world := combineTransforms(parent, localTransform(group.Position, group.Rotation))
	var closest RayHit
	ok := false
	for _, level := range group.Levels {
		if level.Node == nil {
			continue
		}
		if hit, hitOK := raycastNode(level.Node, world, ray, opts); hitOK && rayHitIsCloser(hit, closest, ok, opts) {
			closest = hit
			ok = true
		}
	}
	return closest, ok
}

func raycastMesh(mesh Mesh, parent worldTransform, ray Ray, opts RaycastOptions) (RayHit, bool) {
	pickable := mesh.Pickable == nil || *mesh.Pickable
	if opts.PickableOnly && !pickable {
		return RayHit{}, false
	}
	world := combineTransforms(parent, localTransform(mesh.Position, mesh.Rotation))
	inv := world.Rotation.conjugate().normalized()
	localRay := Ray{
		Origin:    inv.rotate(subVectors(ray.Origin, world.Position)),
		Direction: normalizeVector(inv.rotate(ray.Direction)),
	}
	localHit, kind, ok := raycastGeometry(mesh.Geometry, localRay)
	if !ok {
		return RayHit{}, false
	}
	point := addVectors(world.Position, world.Rotation.rotate(localHit.Point))
	normal := normalizeVector(world.Rotation.rotate(localHit.Normal))
	hit := RayHit{
		ID:       strings.TrimSpace(mesh.ID),
		Kind:     kind,
		Distance: vectorLength(subVectors(point, ray.Origin)),
		Point:    point,
		Normal:   normal,
		Pickable: pickable,
	}
	if opts.MaxDistance > 0 && hit.Distance > opts.MaxDistance {
		return RayHit{}, false
	}
	if childHit, childOK := raycastNodes(mesh.Children, world, ray, opts); childOK && childHit.Distance < hit.Distance {
		return childHit, true
	}
	return hit, true
}

func raycastGeometry(geometry Geometry, ray Ray) (RayHit, string, bool) {
	switch g := geometry.(type) {
	case SphereGeometry:
		radius := positiveOr(g.Radius, 1)
		hit, ok := intersectSphere(ray, radius)
		return hit, "sphere", ok
	case TorusGeometry:
		radius := positiveOr(g.Radius, 1) + positiveOr(g.Tube, 0.25)
		hit, ok := intersectSphere(ray, radius)
		return hit, "torus", ok
	case LinesGeometry:
		min, max := lineBounds(g)
		hit, ok := intersectAABB(ray, min, max)
		return hit, "lines", ok
	case CubeGeometry:
		size := positiveOr(g.Size, 1)
		hit, ok := intersectAABB(ray, Vector3{X: -size / 2, Y: -size / 2, Z: -size / 2}, Vector3{X: size / 2, Y: size / 2, Z: size / 2})
		return hit, "cube", ok
	case BoxGeometry:
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		hit, ok := intersectAABB(ray, min, max)
		return hit, "box", ok
	case PlaneGeometry:
		min, max := boxBounds(g.Width, g.Height, 0.001)
		hit, ok := intersectAABB(ray, min, max)
		return hit, "plane", ok
	case PyramidGeometry:
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		hit, ok := intersectAABB(ray, min, max)
		return hit, "pyramid", ok
	case CylinderGeometry:
		radius := math.Max(positiveOr(g.RadiusTop, 0.5), positiveOr(g.RadiusBottom, 0.5))
		height := positiveOr(g.Height, 1)
		hit, ok := intersectAABB(ray, Vector3{X: -radius, Y: -height / 2, Z: -radius}, Vector3{X: radius, Y: height / 2, Z: radius})
		return hit, "cylinder", ok
	default:
		hit, ok := intersectAABB(ray, Vector3{X: -0.5, Y: -0.5, Z: -0.5}, Vector3{X: 0.5, Y: 0.5, Z: 0.5})
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
