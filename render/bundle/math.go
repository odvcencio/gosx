package bundle

import (
	"math"

	"github.com/odvcencio/gosx/engine"
)

// mat4 is a column-major 4x4 float32 matrix. m[0..3] = column 0, etc.
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

// mat4Orthographic is a symmetric orthographic projection from
// [-size/2, size/2] on x/y and [near, far] on z. Used for directional-light
// shadow view-proj.
func mat4Orthographic(size, near, far float32) mat4 {
	rl := size // right - left = size  (symmetric -size/2..size/2)
	tb := size
	fn := far - near
	var m mat4
	m[0] = 2 / rl
	m[5] = 2 / tb
	m[10] = -2 / fn
	m[12] = 0
	m[13] = 0
	m[14] = -(far + near) / fn
	m[15] = 1
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

// mat4LookAt builds a view matrix for a camera at eye looking at center with
// an up-axis hint. Column-major, right-handed.
func mat4LookAt(eye, center, upHint [3]float32) mat4 {
	// Forward (camera -Z): from eye to center.
	fx, fy, fz := center[0]-eye[0], center[1]-eye[1], center[2]-eye[2]
	fl := float32(math.Sqrt(float64(fx*fx + fy*fy + fz*fz)))
	if fl == 0 {
		return mat4Identity()
	}
	fx, fy, fz = fx/fl, fy/fl, fz/fl

	// Right = forward × up.
	sx := fy*upHint[2] - fz*upHint[1]
	sy := fz*upHint[0] - fx*upHint[2]
	sz := fx*upHint[1] - fy*upHint[0]
	sl := float32(math.Sqrt(float64(sx*sx + sy*sy + sz*sz)))
	if sl == 0 {
		return mat4Identity()
	}
	sx, sy, sz = sx/sl, sy/sl, sz/sl

	// Up = right × forward.
	ux := sy*fz - sz*fy
	uy := sz*fx - sx*fz
	uz := sx*fy - sy*fx

	// Column-major layout: m[col*4+row].
	var m mat4
	m[0] = sx
	m[1] = ux
	m[2] = -fx
	m[3] = 0
	m[4] = sy
	m[5] = uy
	m[6] = -fy
	m[7] = 0
	m[8] = sz
	m[9] = uz
	m[10] = -fz
	m[11] = 0
	m[12] = -(sx*eye[0] + sy*eye[1] + sz*eye[2])
	m[13] = -(ux*eye[0] + uy*eye[1] + uz*eye[2])
	m[14] = fx*eye[0] + fy*eye[1] + fz*eye[2]
	m[15] = 1
	return m
}

// computeMVP derives the combined projection*view matrix from a RenderCamera
// plus framebuffer aspect. R2 treats the camera as a free-moving rig with
// RotationX/Y driving orientation (R3 adds quaternion rotations).
func computeMVP(cam engine.RenderCamera, width, height int) mat4 {
	aspect := float32(1)
	if height > 0 {
		aspect = float32(width) / float32(height)
	}
	fov := float32(cam.FOV)
	if fov <= 0 {
		fov = float32(math.Pi / 3)
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

// computeLightViewProj builds an orthographic view-proj for a directional
// light covering a fixed-size scene volume around the origin. The light is
// placed far along -lightDir so the scene is in front of its near plane.
//
// R2 uses a fixed 30-unit ortho volume which is plenty for the pbr-spike
// demo. R3 replaces this with cascaded shadow maps fitted to the view
// frustum per-frame.
func computeLightViewProj(lightDir [3]float32) mat4 {
	const (
		orthoSize = 30.0
		lightDist = 50.0
		near      = 0.5
		far       = 100.0
	)
	// Position the light eye opposite the light direction.
	eye := [3]float32{
		-lightDir[0] * lightDist,
		-lightDir[1] * lightDist,
		-lightDir[2] * lightDist,
	}
	target := [3]float32{0, 0, 0}
	up := [3]float32{0, 1, 0}
	// If lightDir is almost parallel to up, fall back to +Z up to avoid
	// degenerate cross products.
	if float32(math.Abs(float64(lightDir[1]))) > 0.99 {
		up = [3]float32{0, 0, 1}
	}
	view := mat4LookAt(eye, target, up)
	proj := mat4Orthographic(orthoSize, near, far)
	return mat4Mul(proj, view)
}
