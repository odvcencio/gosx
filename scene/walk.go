package scene

import (
	"encoding/base64"
	"encoding/binary"
	"math"
)

// Walk opts first-person controls into ground-following walking. Zero values
// leave defaults to the browser. Distances are metres and speeds metres/second.
type Walk struct {
	EyeHeight        float64        `json:"eyeHeight,omitempty"`        // default 1.7
	Radius           float64        `json:"radius,omitempty"`           // default 0.35
	WalkSpeed        float64        `json:"walkSpeed,omitempty"`        // default 1.6
	SprintMultiplier float64        `json:"sprintMultiplier,omitempty"` // default 2.2
	MaxSlope         float64        `json:"maxSlope,omitempty"`         // degrees, default 38
	StepHeight       float64        `json:"stepHeight,omitempty"`       // default 0.3
	HeadBob          *float64       `json:"headBob,omitempty"`          // default 0.03; 0 disables
	LookSpeed        float64        `json:"lookSpeed,omitempty"`        // radians/1000px, default 2.2
	Ground           *WalkGround    `json:"ground,omitempty"`
	Water            *WalkWater     `json:"water,omitempty"`
	Colliders        []WalkCollider `json:"colliders,omitempty"`
	Bounds           *WalkBounds    `json:"bounds,omitempty"`
	Hint             string         `json:"hint,omitempty"`    // empty = built-in; "none" disables
	Gamepad          *bool          `json:"gamepad,omitempty"` // default true
}

// WalkGround is a row-major heightfield: rows run along +Z, columns along +X.
// Heights encodes little-endian uint16 samples as base64. A sample v decodes as
// MinHeight + v/65535*(MaxHeight-MinHeight). Outside the grid, sampling clamps.
type WalkGround struct {
	MinX      float64 `json:"minX"`
	MinZ      float64 `json:"minZ"`
	SizeX     float64 `json:"sizeX"`
	SizeZ     float64 `json:"sizeZ"`
	Cols      int     `json:"cols"`
	Rows      int     `json:"rows"`
	MinHeight float64 `json:"minHeight"`
	MaxHeight float64 `json:"maxHeight"`
	Heights   string  `json:"heights"`
}

// NewWalkGround quantizes finite heights into a heightfield. It panics if
// dimensions or sizes are nonpositive, the sample count differs from cols*rows,
// or any coordinate, size, or height is nonfinite. Constant fields encode zeros.
func NewWalkGround(minX, minZ, sizeX, sizeZ float64, cols, rows int, heights []float64) WalkGround {
	if cols <= 0 || rows <= 0 || cols > int(^uint(0)>>1)/rows || len(heights) != cols*rows || sizeX <= 0 || sizeZ <= 0 {
		panic("scene: invalid walk ground dimensions or sample count")
	}
	for _, v := range []float64{minX, minZ, sizeX, sizeZ} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			panic("scene: walk ground coordinates must be finite")
		}
	}
	lo, hi := heights[0], heights[0]
	for _, h := range heights {
		if math.IsNaN(h) || math.IsInf(h, 0) {
			panic("scene: walk ground heights must be finite")
		}
		lo, hi = math.Min(lo, h), math.Max(hi, h)
	}
	data := make([]byte, 2*len(heights))
	for i, h := range heights {
		var v uint16
		if hi > lo {
			v = uint16(math.Round((h - lo) / (hi - lo) * 65535))
		}
		binary.LittleEndian.PutUint16(data[2*i:], v)
	}
	return WalkGround{minX, minZ, sizeX, sizeZ, cols, rows, lo, hi, base64.StdEncoding.EncodeToString(data)}
}

// WalkWater rejects ground below Level-MaxDepth. MaxDepth defaults to 0.55m.
type WalkWater struct {
	Level    float64 `json:"level"`
	MaxDepth float64 `json:"maxDepth,omitempty"`
}

// WalkCollider describes a horizontal obstruction at feet height. Cylinders
// have a base at Y and Height (0 = infinite); boxes use centre and full sizes,
// with yaw RotationY in radians; spheres use centre and Radius.
type WalkCollider struct {
	Kind      string  `json:"kind"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	Radius    float64 `json:"radius,omitempty"`
	Height    float64 `json:"height,omitempty"`
	SizeX     float64 `json:"sizeX,omitempty"`
	SizeY     float64 `json:"sizeY,omitempty"`
	SizeZ     float64 `json:"sizeZ,omitempty"`
	RotationY float64 `json:"rotationY,omitempty"`
}

// WalkBounds constrains the feet position in the XZ plane.
type WalkBounds struct {
	MinX float64 `json:"minX"`
	MinZ float64 `json:"minZ"`
	MaxX float64 `json:"maxX"`
	MaxZ float64 `json:"maxZ"`
}
