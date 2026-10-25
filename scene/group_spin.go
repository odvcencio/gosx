package scene

import (
	"math"
	"strings"

	"m31labs.dev/gosx/internal/scenehierarchy"
	"m31labs.dev/gosx/motion"
)

// GroupIR retains a transform boundary for an animated subtree. Descendants
// address it by ParentNode and keep their transforms in its local coordinates.
// Groups without spin remain flattened unless an animated ancestor needs the
// boundary. ParentMatrix holds any statically lowered prefix between boundaries.
type GroupIR = scenehierarchy.Group

func groupLegacyProps(group GroupIR) map[string]any {
	record := map[string]any{"id": group.ID}
	setString(record, "parentNode", group.ParentNode)
	for _, field := range []struct {
		name  string
		value float64
	}{
		{"x", group.X}, {"y", group.Y}, {"z", group.Z},
		{"rotationX", group.RotationX}, {"rotationY", group.RotationY}, {"rotationZ", group.RotationZ},
		{"spinX", group.SpinX}, {"spinY", group.SpinY}, {"spinZ", group.SpinZ},
		{"scaleX", group.ScaleX}, {"scaleY", group.ScaleY}, {"scaleZ", group.ScaleZ},
	} {
		setNumeric(record, field.name, field.value)
	}
	if len(group.ParentMatrix) > 0 {
		record["parentMatrix"] = append([]float64(nil), group.ParentMatrix...)
	}
	return record
}

func spinningTransform(position Vector3, rotation Euler, scale Vector3, spin Euler, seconds float64) worldTransform {
	if spin == (Euler{}) || seconds == 0 {
		return localScaledTransform(position, rotation, scale)
	}
	q := motion.QuatFromEuler(spin.X*seconds, spin.Y*seconds, spin.Z*seconds)
	local := worldTransform{Position: position, Rotation: (quaternion{X: q.X, Y: q.Y, Z: q.Z, W: q.W}).mul(quaternionFromEuler(rotation)).normalized()}
	if !isUnitSceneScale(scale) {
		local.HasMatrix = true
		local.Matrix = affineFromTRS(position, local.Rotation, scale)
	}
	return local
}

func (l *graphLowerer) lowerRetainedGroup(group Group, parent worldTransform) {
	id := strings.TrimSpace(group.ID)
	if id == "" {
		l.nextGroupID++
		id = "scene-group-" + intString(l.nextGroupID)
	}
	record := GroupIR{
		ID: id, ParentNode: parent.ParentNode,
		X: group.Position.X, Y: group.Position.Y, Z: group.Position.Z,
		RotationX: group.Rotation.X, RotationY: group.Rotation.Y, RotationZ: group.Rotation.Z,
		SpinX: group.Spin.X, SpinY: group.Spin.Y, SpinZ: group.Spin.Z,
		ScaleX: group.Scale.X, ScaleY: group.Scale.Y, ScaleZ: group.Scale.Z,
	}
	if parent.HasMatrix || parent.Position != (Vector3{}) || parent.Rotation != (quaternion{W: 1}) {
		record.ParentMatrix = affineSlice(worldAffine(parent))
	}
	l.groups = append(l.groups, record)
	if group.Spin != (Euler{}) {
		l.spinTracks = append(l.spinTracks, spinMotionTrack(group.Spin, id))
	}
	childParent := identityTransform()
	childParent.ParentNode = id
	l.anchors[id] = childParent
	for _, child := range group.Children {
		l.lowerGroupChild(child, childParent)
	}
}

func (l *graphLowerer) lowerGroupChild(node Node, parent worldTransform) {
	starts := [7]int{len(l.objects), len(l.models), len(l.points), len(l.instancedMeshes), len(l.instancedGLBMeshes), len(l.computeParticles), len(l.lights)}
	l.lowerNode(node, parent)
	// A nested retained group has already bound its own records. Fill only
	// unbound descendants, including helper-generated geometry and mesh children.
	for index := starts[0]; index < len(l.objects); index++ {
		if l.objects[index].ParentNode == "" {
			l.objects[index].ParentNode = parent.ParentNode
		}
	}
	for index := starts[1]; index < len(l.models); index++ {
		if l.models[index].ParentNode == "" {
			l.models[index].ParentNode = parent.ParentNode
		}
	}
	for index := starts[2]; index < len(l.points); index++ {
		if l.points[index].ParentNode == "" {
			l.points[index].ParentNode = parent.ParentNode
		}
	}
	for index := starts[3]; index < len(l.instancedMeshes); index++ {
		if l.instancedMeshes[index].ParentNode == "" {
			l.instancedMeshes[index].ParentNode = parent.ParentNode
		}
	}
	for index := starts[4]; index < len(l.instancedGLBMeshes); index++ {
		if l.instancedGLBMeshes[index].ParentNode == "" {
			l.instancedGLBMeshes[index].ParentNode = parent.ParentNode
		}
	}
	for index := starts[5]; index < len(l.computeParticles); index++ {
		if l.computeParticles[index].ParentNode == "" {
			l.computeParticles[index].ParentNode = parent.ParentNode
		}
	}
	for index := starts[6]; index < len(l.lights); index++ {
		if l.lights[index].ParentNode == "" {
			l.lights[index].ParentNode = parent.ParentNode
		}
	}
}

// GroupWorldMatrices evaluates retained parent transforms at scene-clock time.
// The spin orientation uses the same motion generator convention as Mesh.Spin:
// authored base orientation, then the angular-velocity quaternion, then parents.
// Empty hierarchies perform no allocation.
func GroupWorldMatrices(groups []GroupIR, seconds float64) (map[string][16]float64, error) {
	return scenehierarchy.WorldMatrices(groups, seconds)
}

func validSpinTime(spin Euler, seconds float64) bool {
	if spin == (Euler{}) {
		return true
	}
	for _, angle := range []float64{spin.X * seconds, spin.Y * seconds, spin.Z * seconds} {
		if math.IsNaN(angle) || math.IsInf(angle, 0) {
			return false
		}
	}
	return true
}
