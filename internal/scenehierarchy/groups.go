// Package scenehierarchy evaluates retained Scene3D parent transforms for
// native and browser render preparation without importing scene authoring.
package scenehierarchy

import (
	"fmt"
	"math"

	"m31labs.dev/gosx/internal/sceneaffine"
	"m31labs.dev/gosx/motion"
)

// Group is a retained local transform with an optional animated parent.
type Group struct {
	ID           string    `json:"id"`
	ParentNode   string    `json:"parentNode,omitempty"`
	X            float64   `json:"x,omitempty"`
	Y            float64   `json:"y,omitempty"`
	Z            float64   `json:"z,omitempty"`
	RotationX    float64   `json:"rotationX,omitempty"`
	RotationY    float64   `json:"rotationY,omitempty"`
	RotationZ    float64   `json:"rotationZ,omitempty"`
	SpinX        float64   `json:"spinX,omitempty"`
	SpinY        float64   `json:"spinY,omitempty"`
	SpinZ        float64   `json:"spinZ,omitempty"`
	ScaleX       float64   `json:"scaleX,omitempty"`
	ScaleY       float64   `json:"scaleY,omitempty"`
	ScaleZ       float64   `json:"scaleZ,omitempty"`
	ParentMatrix []float64 `json:"parentMatrix,omitempty"`
}

// WorldMatrices composes local transforms at scene-clock time. Empty input
// returns nil without allocating; malformed or cyclic hierarchies fail closed.
func WorldMatrices(groups []Group, seconds float64) (map[string][16]float64, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return nil, fmt.Errorf("scene group time must be finite")
	}
	indexes := make(map[string]int, len(groups))
	for index, group := range groups {
		if group.ID == "" {
			return nil, fmt.Errorf("scene group %d has no ID", index)
		}
		if _, found := indexes[group.ID]; found {
			return nil, fmt.Errorf("scene group %q has a duplicate ID", group.ID)
		}
		indexes[group.ID] = index
	}
	states := make([]uint8, len(groups))
	result := make(map[string][16]float64, len(groups))
	var evaluate func(int) ([16]float64, error)
	evaluate = func(index int) ([16]float64, error) {
		group := &groups[index]
		if states[index] == 2 {
			return [16]float64(result[group.ID]), nil
		}
		if states[index] == 1 {
			return [16]float64{}, fmt.Errorf("scene group %q forms a parent cycle", group.ID)
		}
		states[index] = 1
		for _, value := range []float64{group.X, group.Y, group.Z, group.RotationX, group.RotationY, group.RotationZ, group.SpinX, group.SpinY, group.SpinZ, group.ScaleX, group.ScaleY, group.ScaleZ} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return [16]float64{}, fmt.Errorf("scene group %q has a nonfinite transform", group.ID)
			}
		}
		spin := motion.QuatFromEuler(group.SpinX*seconds, group.SpinY*seconds, group.SpinZ*seconds)
		base := mul(mul(motion.QuatFromEuler(0, 0, group.RotationZ), motion.QuatFromEuler(0, group.RotationY, 0)), motion.QuatFromEuler(group.RotationX, 0, 0))
		rotation := mul(spin, base).Normalize()
		world := compose(group, rotation)
		if len(group.ParentMatrix) > 0 {
			if !sceneaffine.ValidParentMatrix(group.ParentMatrix) {
				return [16]float64{}, fmt.Errorf("scene group %q has an invalid parent matrix", group.ID)
			}
			var prefix [16]float64
			copy(prefix[:], group.ParentMatrix)
			world = Multiply(prefix, world)
		}
		if group.ParentNode != "" {
			parentIndex, found := indexes[group.ParentNode]
			if !found {
				return [16]float64{}, fmt.Errorf("scene group %q has unknown parent %q", group.ID, group.ParentNode)
			}
			parent, err := evaluate(parentIndex)
			if err != nil {
				return [16]float64{}, err
			}
			world = Multiply(parent, world)
		}
		for _, value := range world {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return [16]float64{}, fmt.Errorf("scene group %q produces a nonfinite matrix", group.ID)
			}
		}
		states[index] = 2
		result[group.ID] = [16]float64(world)
		return world, nil
	}
	for index := range groups {
		if _, err := evaluate(index); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func mul(a, b motion.Quat) motion.Quat {
	return motion.Quat{
		X: a.W*b.X + a.X*b.W + a.Y*b.Z - a.Z*b.Y,
		Y: a.W*b.Y - a.X*b.Z + a.Y*b.W + a.Z*b.X,
		Z: a.W*b.Z + a.X*b.Y - a.Y*b.X + a.Z*b.W,
		W: a.W*b.W - a.X*b.X - a.Y*b.Y - a.Z*b.Z,
	}
}
func compose(g *Group, q motion.Quat) [16]float64 {
	sx, sy, sz := g.ScaleX, g.ScaleY, g.ScaleZ
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	if sz == 0 {
		sz = 1
	}
	xx, yy, zz, xy, xz, yz, wx, wy, wz := q.X*q.X, q.Y*q.Y, q.Z*q.Z, q.X*q.Y, q.X*q.Z, q.Y*q.Z, q.W*q.X, q.W*q.Y, q.W*q.Z
	return [16]float64{
		(1 - 2*(yy+zz)) * sx, 2 * (xy + wz) * sx, 2 * (xz - wy) * sx, 0,
		2 * (xy - wz) * sy, (1 - 2*(xx+zz)) * sy, 2 * (yz + wx) * sy, 0,
		2 * (xz + wy) * sz, 2 * (yz - wx) * sz, (1 - 2*(xx+yy)) * sz, 0,
		g.X, g.Y, g.Z, 1,
	}
}

// Multiply composes two column-major affine transforms, applying right first.
func Multiply(left, right [16]float64) (out [16]float64) {
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			out[col*4+row] = left[row]*right[col*4] + left[4+row]*right[col*4+1] + left[8+row]*right[col*4+2] + left[12+row]*right[col*4+3]
		}
	}
	return
}
