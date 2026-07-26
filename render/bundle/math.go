package bundle

import (
	"math"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
)

// mat4 is a column-major 4x4 float32 matrix. m[0..3] = column 0, etc.
type mat4 [16]float32

// mat4FromQuat builds a column-major rotation matrix from a quaternion.
// Callers are expected to pass a unit quaternion; normalization is applied
// as a cheap guard against accumulated floating-point drift. The convention
// is pinned (by TestMat4FromQuatMatchesOldEulerCompose) to exactly match the
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
	return mat4Mul(proj, cameraViewMatrix(cam))
}

// cameraViewMatrix builds the world-to-view matrix for a 3D camera:
// rotate about X, then about Y, then translate by the negated position.
// computeMVP and buildCascadeMatrix both call it, so the cascade fit inverts
// exactly the transform the main pass applies. A private copy in either place
// would drift.
//
// RotationZ is not part of the composition. computeMVP never applied it, and a
// cascade fit that added roll would put shadows where the main pass does not
// draw them.
func cameraViewMatrix(cam engine.RenderCamera) mat4 {
	rotX := mat4RotateX(float32(cam.RotationX))
	rotY := mat4RotateY(float32(cam.RotationY))
	trans := mat4Translate(-float32(cam.X), -float32(cam.Y), -float32(cam.Z))
	return mat4Mul(mat4Mul(rotX, rotY), trans)
}

// cameraViewToWorldRotation returns the rotation that carries a view-space
// direction back into world space. The view rotation is orthonormal, so its
// inverse is its transpose, and the transpose costs nine copies instead of a
// general 4x4 inverse.
//
// buildCascadeMatrix needs this to place frustum-slice corners. Without it the
// corners land as though the camera always looked down world -Z, so a rotated
// camera fits every cascade to the wrong part of the world and loses all its
// shadows.
func cameraViewToWorldRotation(cam engine.RenderCamera) mat4 {
	rotX := mat4RotateX(float32(cam.RotationX))
	rotY := mat4RotateY(float32(cam.RotationY))
	r := mat4Mul(rotX, rotY)
	var t mat4
	for col := 0; col < 3; col++ {
		for row := 0; row < 3; row++ {
			t[col*4+row] = r[row*4+col]
		}
	}
	t[15] = 1
	return t
}

// rotateVec3 applies the upper-left 3x3 of m to v. Used for direction vectors,
// so the translation column is deliberately ignored.
func rotateVec3(m mat4, v [3]float32) [3]float32 {
	return [3]float32{
		m[0]*v[0] + m[4]*v[1] + m[8]*v[2],
		m[1]*v[0] + m[5]*v[1] + m[9]*v[2],
		m[2]*v[0] + m[6]*v[1] + m[10]*v[2],
	}
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

// defaultCascadeLambda blends the logarithmic and the uniform split schedules.
// 0 gives uniform splits, which spend resolution far away. 1 gives logarithmic
// splits, which spend it near the camera and starve the far cascade. 0.5 is the
// practical default the JavaScript WebGL backend already uses, so both renderers
// place their cascade edges in the same places.
const defaultCascadeLambda = 0.5

// resolveCascadeLambda reads Config.ShadowCascadeLambda. A nil pointer selects
// the shared default so a host that does not care matches the web renderer.
func resolveCascadeLambda(configured *float64) float32 {
	if configured == nil {
		return defaultCascadeLambda
	}
	return float32(*configured)
}

// cascadeSplitDistances returns the cascade boundaries in view space, from the
// camera near plane through each cascade's far distance. Element 0 is near and
// element count is far, so cascade i covers [out[i], out[i+1]].
//
// The schedule is the Parallel-Split Shadow Maps practical scheme:
//
//	log_i     = near * (far/near)^p
//	uniform_i = near + (far - near) * p
//	split_i   = lambda*log_i + (1 - lambda)*uniform_i,  p = (i+1)/count
//
// The renderer used fixed 6 / 22 / far boundaries before, which suited one
// scene scale and banded every other. This schedule tracks the camera's own
// near and far planes instead.
func cascadeSplitDistances(near, far float32, count int, lambda float32) [cascadeCount + 1]float32 {
	var out [cascadeCount + 1]float32
	if count < 1 {
		count = 1
	}
	if count > cascadeCount {
		count = cascadeCount
	}
	if near <= 0 {
		near = 0.1
	}
	if far <= near {
		far = near + 0.0001
	}
	if lambda < 0 {
		lambda = 0
	}
	if lambda > 1 {
		lambda = 1
	}
	ratio := float64(far / near)
	out[0] = near
	for i := 0; i < count; i++ {
		p := float64(i+1) / float64(count)
		logSplit := float64(near) * math.Pow(ratio, p)
		uniSplit := float64(near) + float64(far-near)*p
		out[i+1] = float32(float64(lambda)*logSplit + (1-float64(lambda))*uniSplit)
	}
	// The last cascade always reaches the camera far plane, even when float
	// rounding leaves the blend a hair short.
	out[count] = far
	for i := count + 1; i <= cascadeCount; i++ {
		out[i] = far
	}
	return out
}

// computeCascades builds three cascaded light view-proj matrices fitted to
// three slices of the camera view frustum. Each cascade covers the bounding
// sphere of its slice. A sphere has one radius whatever the camera heading, so
// the orthographic extent never changes as the camera turns, and
// buildCascadeMatrix rounds the sphere centre to whole shadow-map texels so the
// shadow edges do not crawl as the camera moves.
//
// aspect is the framebuffer width divided by its height. Pass the real value:
// the fit used to assume a square framebuffer, which left the true frustum
// wider than the fitted box on every wide viewport and dropped the shadows of
// casters near the left and right screen edges.
func computeCascades(cam engine.RenderCamera, lightDir [3]float32, lambda, aspect float32) cascadeData {
	var out cascadeData

	near := float32(cam.Near)
	if near <= 0 {
		near = 0.1
	}
	far := float32(cam.Far)
	if far <= 0 {
		far = 100
	}
	if aspect <= 0 {
		aspect = 1
	}
	fov := float32(cam.FOV)
	if fov <= 0 {
		fov = float32(math.Pi / 3)
	}
	splits := cascadeSplitDistances(near, far, cascadeCount, lambda)
	// The lit shader selects a cascade from the straight-line distance between
	// the camera and the pixel, not from the pixel's view-space depth. The two
	// differ most at the frustum corners, where the straight-line distance is
	// radialRatio times the depth. Pulling each cascade's near edge back by that
	// ratio makes the fitted box cover every pixel the shader can route to it.
	// Without the pull-back a corner pixel just inside one split lands in the
	// next cascade, falls outside that cascade's box, and loses its shadow.
	ratio := radialRatio(fov, aspect)
	// The camera rotation and the light frame are the same for every cascade.
	// Building them once takes eight transcendental calls and two matrix
	// products out of the per-cascade loop.
	shape := cascadeShape{
		toWorld:    cameraViewToWorldRotation(cam),
		lightFrame: lightFrameMatrix(lightDir),
		camPos:     [3]float32{float32(cam.X), float32(cam.Y), float32(cam.Z)},
		tanHalf:    float32(math.Tan(float64(fov) / 2)),
		aspect:     aspect,
	}
	for i := 0; i < cascadeCount; i++ {
		sliceNear := splits[i] / ratio
		if sliceNear < near {
			sliceNear = near
		}
		out.viewProjs[i] = buildCascadeMatrix(shape, sliceNear, splits[i+1])
		out.farSplits[i] = splits[i+1]
	}
	return out
}

// cascadeShape carries the parts of a cascade fit that do not change between
// cascades. computeCascades builds it once per frame.
type cascadeShape struct {
	// toWorld rotates a view-space direction back into world space.
	toWorld mat4
	// lightFrame is the light view rotation anchored at the world origin. The
	// anchor has to stay put while the camera moves, or the shadow-map texel
	// grid moves with the camera and the snap in buildCascadeMatrix buys
	// nothing.
	lightFrame mat4
	camPos     [3]float32
	tanHalf    float32
	aspect     float32
}

// lightFrameMatrix builds the light view rotation for a directional light,
// anchored at the world origin.
func lightFrameMatrix(lightDir [3]float32) mat4 {
	up := [3]float32{0, 1, 0}
	if float32(math.Abs(float64(lightDir[1]))) > 0.99 {
		up = [3]float32{0, 0, 1}
	}
	return mat4LookAt([3]float32{0, 0, 0}, lightDir, up)
}

// radialRatio is the largest ratio of straight-line camera distance to
// view-space depth inside a frustum with this field of view and aspect. It is
// the length of the direction to a frustum corner, taken at unit depth.
func radialRatio(fovRad, aspect float32) float32 {
	tanHalf := float32(math.Tan(float64(fovRad) / 2))
	x := tanHalf * aspect
	y := tanHalf
	return float32(math.Sqrt(float64(1 + x*x + y*y)))
}

// buildCascadeMatrix returns the light-space view-projection fitted to the
// sub-frustum between viewNear and viewFar, used for rendering one shadow
// cascade.
//
// The box is fitted tight to the slice. The fit used to widen the bounding
// sphere by a fifth so that casters just outside the frustum still cast into
// the cascade, but a directional light does not need the margin: the shadow of
// a point lands at the point's own light-space x and y, so a caster outside the
// box in x or y drops its shadow outside the box too. Only the light-space
// depth range has to reach back toward the light, and backOff does that. The
// removed margin returns its square in shadow-map texel density.
func buildCascadeMatrix(shape cascadeShape, viewNear, viewFar float32) mat4 {
	// The 8 slice corners in view space. tan(fov/2) gives the vertical
	// half-extent per unit of depth; the horizontal half-extent scales by the
	// framebuffer aspect, exactly as mat4Perspective does.
	tanHalf, aspect := shape.tanHalf, shape.aspect
	corners := [8][3]float32{
		// Near plane corners.
		{-tanHalf * viewNear * aspect, -tanHalf * viewNear, -viewNear},
		{+tanHalf * viewNear * aspect, -tanHalf * viewNear, -viewNear},
		{+tanHalf * viewNear * aspect, +tanHalf * viewNear, -viewNear},
		{-tanHalf * viewNear * aspect, +tanHalf * viewNear, -viewNear},
		// Far plane corners.
		{-tanHalf * viewFar * aspect, -tanHalf * viewFar, -viewFar},
		{+tanHalf * viewFar * aspect, -tanHalf * viewFar, -viewFar},
		{+tanHalf * viewFar * aspect, +tanHalf * viewFar, -viewFar},
		{-tanHalf * viewFar * aspect, +tanHalf * viewFar, -viewFar},
	}
	// Carry the corners back into world space through the true inverse of the
	// camera view matrix: rotate by the transposed view rotation, then add the
	// camera position.
	toWorld, camPos := shape.toWorld, shape.camPos
	for i := range corners {
		rotated := rotateVec3(toWorld, corners[i])
		corners[i] = [3]float32{
			rotated[0] + camPos[0],
			rotated[1] + camPos[1],
			rotated[2] + camPos[2],
		}
	}
	// Centre + radius of the bounding sphere.
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
	if r <= 0 {
		r = 0.001
	}

	// backOff extends the volume toward the light so casters above the slice
	// still reach the depth range.
	const backOff = 20.0
	lightFrame := shape.lightFrame
	lx := lightFrame[0]*cx + lightFrame[4]*cy + lightFrame[8]*cz
	ly := lightFrame[1]*cx + lightFrame[5]*cy + lightFrame[9]*cz
	lz := lightFrame[2]*cx + lightFrame[6]*cy + lightFrame[10]*cz

	// Round the sphere centre to whole shadow-map texels. A tight fit follows
	// the camera exactly, so without the rounding every sub-texel camera step
	// slides the whole shadow map and the shadow edges crawl. The sphere radius
	// already holds still under rotation, so the centre is all that is left.
	size := 2 * r
	texel := size / float32(shadowMapSize)
	if texel > 0 {
		lx = float32(math.Round(float64(lx/texel))) * texel
		ly = float32(math.Round(float64(ly/texel))) * texel
	}
	// Put the snapped centre on the light frame's view axis, backOff + r in
	// front of the eye.
	view := mat4Mul(mat4Translate(-lx, -ly, -lz-(r+backOff)), lightFrame)

	proj := mat4Orthographic(size, 0.5, 2*(r+backOff)+size)
	return mat4Mul(proj, view)
}
