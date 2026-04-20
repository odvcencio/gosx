package bundle

import (
	"math"

	"github.com/odvcencio/gosx/engine"
)

// mat4 is a column-major 4x4 float32 matrix. Columns come first in memory:
// m[0..3] = column 0, m[4..7] = column 1, etc.
type mat4 [16]float32

func mat4Identity() mat4 {
	var m mat4
	m[0], m[5], m[10], m[15] = 1, 1, 1, 1
	return m
}

func mat4Mul(a, b mat4) mat4 {
	var r mat4
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			var sum float32
			for k := 0; k < 4; k++ {
				sum += a[k*4+row] * b[col*4+k]
			}
			r[col*4+row] = sum
		}
	}
	return r
}

func mat4Perspective(fovRad, aspect, near, far float32) mat4 {
	f := float32(1.0) / float32(math.Tan(float64(fovRad/2)))
	nf := 1 / (near - far)
	var m mat4
	m[0] = f / aspect
	m[5] = f
	m[10] = (far + near) * nf
	m[11] = -1
	m[14] = (2 * far * near) * nf
	return m
}

func mat4Translate(x, y, z float32) mat4 {
	m := mat4Identity()
	m[12], m[13], m[14] = x, y, z
	return m
}

func mat4RotateY(a float32) mat4 {
	c := float32(math.Cos(float64(a)))
	s := float32(math.Sin(float64(a)))
	m := mat4Identity()
	m[0], m[2], m[8], m[10] = c, -s, s, c
	return m
}

func mat4RotateX(a float32) mat4 {
	c := float32(math.Cos(float64(a)))
	s := float32(math.Sin(float64(a)))
	m := mat4Identity()
	m[5], m[6], m[9], m[10] = c, s, -s, c
	return m
}

// computeMVP derives an MVP matrix from a RenderCamera + framebuffer size.
// For R1 the camera has no view-matrix machinery yet (no lookAt in the
// bundle), so we treat RenderCamera.X/Y/Z as camera position with identity
// orientation and derive a simple perspective from FOV. Good enough for a
// first working frame; R2 extends this.
func computeMVP(cam engine.RenderCamera, width, height int) mat4 {
	aspect := float32(1)
	if height > 0 {
		aspect = float32(width) / float32(height)
	}
	fov := float32(cam.FOV)
	if fov <= 0 {
		fov = float32(math.Pi / 3) // 60° default
	}
	near := float32(cam.Near)
	if near <= 0 {
		near = 0.1
	}
	far := float32(cam.Far)
	if far <= 0 {
		far = 100
	}
	proj := mat4Perspective(fov, aspect, near, far)

	rotX := mat4RotateX(float32(cam.RotationX))
	rotY := mat4RotateY(float32(cam.RotationY))
	trans := mat4Translate(-float32(cam.X), -float32(cam.Y), -float32(cam.Z))
	view := mat4Mul(mat4Mul(rotX, rotY), trans)

	return mat4Mul(proj, view)
}
