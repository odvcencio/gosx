package scene

import (
	"math"
	"sync"
)

//go:generate go run ./internal/genobjectproof/main.go

type objectProofOmissions uint8

const (
	objectProofOmitTransform objectProofOmissions = 1 << iota
	objectProofOmitState
)

var objectProofOnce sync.Once
var objectProofCurrent bool

// Keep schema validation lazy: unused scene diff machinery adds no startup work
// to browser runtime profiles that do not compare SceneIR records.
func objectProofEnabled() bool {
	objectProofOnce.Do(func() { objectProofCurrent = objectProofSchemaMatches() })
	return objectProofCurrent
}

// A patch can skip comparison of fields it replaces, but their new scalar
// values must still satisfy the JSON encoder's finite-number contract.
func finiteObjectTransform(next *ObjectIR) bool {
	for _, value := range [...]float64{next.X, next.Y, next.Z, next.RotationX, next.RotationY, next.RotationZ, next.ScaleX, next.ScaleY, next.ScaleZ} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func objectPatchFieldsEqual(previous, next *ObjectIR, omitted objectProofOmissions) bool {
	if objectProofEnabled() && finiteObjectTransform(next) && provenObjectFieldsEqual(previous, next, omitted) {
		return true
	}
	return objectPatchFieldsJSONEqual(previous, next, omitted)
}

// Only ambiguous comparisons construct a hypothetical replaced record. This
// preserves encoder normalization, signed-zero and nil/empty semantics while
// keeping the large ObjectIR copy off the ordinary transform/state path.
func objectPatchFieldsJSONEqual(previous, next *ObjectIR, omitted objectProofOmissions) bool {
	probe := *previous
	probe.X, probe.Y, probe.Z = next.X, next.Y, next.Z
	probe.RotationX, probe.RotationY, probe.RotationZ = next.RotationX, next.RotationY, next.RotationZ
	probe.ScaleX, probe.ScaleY, probe.ScaleZ = next.ScaleX, next.ScaleY, next.ScaleZ
	probe.ParentMatrix = next.ParentMatrix
	if omitted&objectProofOmitState != 0 {
		probe.Interactive, probe.Label, probe.InteractiveOrder = next.Interactive, next.Label, next.InteractiveOrder
		probe.Pickable, probe.Visible, probe.Selected = next.Pickable, next.Visible, next.Selected
	}
	return sceneRecordPointerJSONEqual(&probe, next)
}
