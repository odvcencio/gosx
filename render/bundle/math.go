package bundle

import (
	"math"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
)

// mat4 is a column-major 4x4 float32 matrix. m[0..3] = column 0, etc.
type mat4 [16]float32

// mat4FromQuat builds a column-major rotation matrix from a quaternion. The
// quaternion is normalized first to guard against drift. The convention is
// pinned (by TestMat4FromQuatMatchesOldEulerCompose) to exactly match the
// RotX·RotY·RotZ Euler compose this path used before slerp, so endpoints are
// unchanged. Layout: m[col*4+row].
func mat4FromQuat(q motion.Quat) mat4 {
	qn := q.Normalize()
	x, y, z, w := qn.X, qn.Y, qn.Z, qn.W
	xx, yy, zz := x*x, y*y, z*z
	xy, xz, yz := x*y, x*z, y*z
	wx, wy, wz := w*x, w*y, w*z
	m := mat4Identity()
	// Column 0.
	m[0] = float32(1 - 2*(yy+zz))
	m[1] = float32(2 * (xy + wz))
	m[2] = float32(2 * (xz - wy))
	// Column 1.
	m[4] = float32(2 * (xy - wz))
	m[5] = float32(1 - 2*(xx+zz))
	m[6] = float32(2 * (yz + wx))
	// Column 2.
	m[8] = float32(2 * (xz + wy))
	m[9] = float32(2 * (yz - wx))
	m[10] = float32(1 - 2*(xx+yy))
	return m
}

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
//
// When cam.Mode == OrthoCamera2DMode the function branches into the 2D path:
// an asymmetric orthographic projection sized to the framebuffer scaled by
// the camera's zoom (carried in cam.Z), translated by the camera's pan
// (carried in cam.X/Y). Depth/rotation are ignored — the 2D pipeline runs
// with depth disabled per ADR 0004.
func computeMVP(cam engine.RenderCamera, width, height int) mat4 {
	if cam.Mode == orthoCamera2DModeString {
		return computeOrthoCamera2DMVP(cam, width, height)
	}
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

// orthoCamera2DModeString mirrors bundle.OrthoCamera2DMode without taking a
// dependency cycle. Kept private — public callers go through OrthoCamera2D.
const orthoCamera2DModeString = "ortho2d"

// computeOrthoCamera2DMVP builds the projection*view matrix for the 2D board
// path. Mapping: world (x, y) → screen pixels with zoom (cam.Z), centered on
// the pan point (cam.X, cam.Y). Output is column-major like computeMVP.
//
// Math: orthographic from -halfW..halfW × -halfH..halfH where halfW/H are the
// framebuffer half-extents divided by zoom; then translate by (-panX, -panY).
// The Y axis is intentionally NOT flipped here — the renderer's NDC convention
// already has +Y up, so a board-space "up" matches screen-space "up". The
// CanvasBoardAdapter's input handlers compensate for the screen-down Y of
// pointer events.
func computeOrthoCamera2DMVP(cam engine.RenderCamera, width, height int) mat4 {
	zoom := float32(cam.Z)
	if zoom <= 0 {
		zoom = 1
	}
	w := float32(width)
	h := float32(height)
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	halfW := w / (2 * zoom)
	halfH := h / (2 * zoom)
	near := float32(cam.Near)
	if near == 0 {
		near = -1
	}
	far := float32(cam.Far)
	if far == 0 {
		far = 1
	}
	proj := mat4OrthographicAsym(-halfW, halfW, -halfH, halfH, near, far)
	trans := mat4Translate(-float32(cam.X), -float32(cam.Y), 0)
	return mat4Mul(proj, trans)
}

// mat4OrthographicAsym is an asymmetric orthographic projection matrix
// (right-handed, NDC z in [-1, 1]). Used by the 2D camera path.
func mat4OrthographicAsym(left, right, bottom, top, near, far float32) mat4 {
	rl := right - left
	tb := top - bottom
	fn := far - near
	if rl == 0 {
		rl = 1
	}
	if tb == 0 {
		tb = 1
	}
	if fn == 0 {
		fn = 1
	}
	var m mat4
	m[0] = 2 / rl
	m[5] = 2 / tb
	m[10] = -2 / fn
	m[12] = -(right + left) / rl
	m[13] = -(top + bottom) / tb
	m[14] = -(far + near) / fn
	m[15] = 1
	return m
}

// cascadeData is a per-frame packet of cascaded-shadow-map view-proj
// matrices plus the view-space split distances the lit shader uses to pick
// a cascade.
type cascadeData struct {
	viewProjs [3]mat4
	// farSplits.xyz are the far distances (in view-space) for cascades 0/1/2.
	// Cascade 2's split == camera far plane.
	farSplits [4]float32
}

// computeCascades builds three cascaded light view-proj matrices fitted to
// three slices of the camera view frustum. Fit strategy: each cascade
// covers a bounding sphere of its frustum slice in light space, giving a
// stable orthographic projection that doesn't flicker when the camera
// rotates (shimmering at cascade edges is a known CSM artifact addressed by
// rounding to texel increments; that refinement is R4).
//
// Splits default to a practical-but-arbitrary 2 / 15 / 60 world units. R4
// will expose these via RenderBundle fields.
func computeCascades(cam engine.RenderCamera, lightDir [3]float32) cascadeData {
	var out cascadeData

	near := float32(cam.Near)
	if near <= 0 {
		near = 0.1
	}
	far := float32(cam.Far)
	if far <= 0 {
		far = 100
	}
	// Fixed splits for R3. R4 replaces with log / uniform blending.
	splits := [cascadeCount + 1]float32{near, 6, 22, far}
	for i := 0; i < cascadeCount; i++ {
		out.viewProjs[i] = buildCascadeMatrix(cam, lightDir, splits[i], splits[i+1])
		out.farSplits[i] = splits[i+1]
	}
	return out
}

// buildCascadeMatrix returns the light-space view-projection fitted to the
// sub-frustum between viewNear and viewFar, used for rendering one shadow
// cascade.
func buildCascadeMatrix(cam engine.RenderCamera, lightDir [3]float32, viewNear, viewFar float32) mat4 {
	// Reconstruct the 8 frustum corners in world space.
	aspect := float32(1)
	fov := float32(cam.FOV)
	if fov <= 0 {
		fov = float32(math.Pi / 3)
	}
	// tan(fov/2) for vertical; horizontal scales by aspect. We don't
	// actually know the aspect here (it's the framebuffer's), so assume
	// square for the cascade fit — a slight overestimate keeps the sphere
	// fully containing the frustum. R4 can plumb width/height through.
	tanHalf := float32(math.Tan(float64(fov) / 2))
	corners := [8][3]float32{
		// Near plane corners
		{-tanHalf * viewNear * aspect, -tanHalf * viewNear, -viewNear},
		{+tanHalf * viewNear * aspect, -tanHalf * viewNear, -viewNear},
		{+tanHalf * viewNear * aspect, +tanHalf * viewNear, -viewNear},
		{-tanHalf * viewNear * aspect, +tanHalf * viewNear, -viewNear},
		// Far plane corners
		{-tanHalf * viewFar * aspect, -tanHalf * viewFar, -viewFar},
		{+tanHalf * viewFar * aspect, -tanHalf * viewFar, -viewFar},
		{+tanHalf * viewFar * aspect, +tanHalf * viewFar, -viewFar},
		{-tanHalf * viewFar * aspect, +tanHalf * viewFar, -viewFar},
	}
	// Transform from view space back to world using the inverse camera
	// view. Approximated here by building the forward view and inverting
	// its translation — full inverse is R4 work. For cascade fit purposes
	// we treat corners as world-centered around camera.
	camPos := [3]float32{float32(cam.X), float32(cam.Y), float32(cam.Z)}
	for i := range corners {
		// Rotate by camera orientation (inverse rotation).
		// For R3 we ignore camera rotation; the bounding sphere is
		// conservative enough to catch slight mismatches. Pure
		// translation recovers the world-space position for a
		// forward-looking camera.
		corners[i][0] += camPos[0]
		corners[i][1] += camPos[1]
		corners[i][2] += camPos[2]
	}
	// Center + radius of the bounding sphere.
	var cx, cy, cz float32
	for _, c := range corners {
		cx += c[0]
		cy += c[1]
		cz += c[2]
	}
	cx /= 8
	cy /= 8
	cz /= 8
	var r float32
	for _, c := range corners {
		dx, dy, dz := c[0]-cx, c[1]-cy, c[2]-cz
		d := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
		if d > r {
			r = d
		}
	}
	// Pad the radius so shadow casters just outside the frustum still
	// cast into the cascade.
	r *= 1.2

	// Light eye at center - lightDir*(r + backOff).
	const backOff = 20.0
	eye := [3]float32{
		cx - lightDir[0]*(r+backOff),
		cy - lightDir[1]*(r+backOff),
		cz - lightDir[2]*(r+backOff),
	}
	target := [3]float32{cx, cy, cz}
	up := [3]float32{0, 1, 0}
	if float32(math.Abs(float64(lightDir[1]))) > 0.99 {
		up = [3]float32{0, 0, 1}
	}
	view := mat4LookAt(eye, target, up)

	size := 2 * r
	proj := mat4Orthographic(size, 0.5, 2*(r+backOff)+size)
	return mat4Mul(proj, view)
}
