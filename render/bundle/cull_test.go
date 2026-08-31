package bundle

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu/headless"
)

// scaledTransform returns an identity transform scaled uniformly by s and
// translated to (x, y, z), in the column-major layout the renderer expects.
func scaledTransform(s, x, y, z float64) []float64 {
	return []float64{
		s, 0, 0, 0,
		0, s, 0, 0,
		0, 0, s, 0,
		x, y, z, 1,
	}
}

// TestInstanceCullRadiusFollowsTransformScale pins defect F on the CPU side. The
// renderer stores one unscaled radius per primitive, so the per-instance scale
// has to widen it or a scaled-up instance vanishes while still on screen.
func TestInstanceCullRadiusFollowsTransformScale(t *testing.T) {
	base := float32(1)
	cases := []struct {
		name  string
		model mat4
		want  float32
	}{
		{"identity", matrixForInstance(identityTransform(), 0), float32(math.Sqrt(3))},
		{"uniform 10x", matrixForInstance(scaledTransform(10, 0, 0, 0), 0), float32(10 * math.Sqrt(3))},
		{"uniform 0.25x", matrixForInstance(scaledTransform(0.25, 0, 0, 0), 0), float32(0.25 * math.Sqrt(3))},
		{"non-uniform Frobenius bound", mat4{
			2, 0, 0, 0,
			0, 7, 0, 0,
			0, 0, 3, 0,
			0, 0, 0, 1,
		}, float32(math.Sqrt(62))},
		{"shear exceeds every column", mat4{
			1.5, 0.5, 0, 0,
			-1.5, 0.5, 0, 0,
			0, 0, 1, 0,
			0, 0, 0, 1,
		}, float32(math.Sqrt(6))},
		{"degenerate zero scale keeps the base radius", mat4{}, 1},
	}
	for _, tc := range cases {
		got := instanceCullRadius(base, tc.model)
		if math.Abs(float64(got-tc.want)) > 1e-4 {
			t.Errorf("%s: radius = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestCullShaderScalesRadiusPerInstance keeps the WGSL text and the CPU oracle
// in step. No GPU is available here, so the guard is that the shader reads the
// matrix columns and multiplies the uniform radius by their largest length.
func TestCullShaderScalesRadiusPerInstance(t *testing.T) {
	for _, want := range []string{
		"dot(m[0].xyz, m[0].xyz)",
		"dot(m[1].xyz, m[1].xyz)",
		"dot(m[2].xyz, m[2].xyz)",
		"radius = radius * scale",
		"d < -radius",
	} {
		if !strings.Contains(cullWGSL, want) {
			t.Errorf("cullWGSL is missing %q; per-instance scale culling regressed", want)
		}
	}
}

// TestCascadeSplitsBlendLogAndUniform pins defect H. The schedule must track the
// camera's own near and far planes, sit between the pure logarithmic and pure
// uniform schedules, and rise monotonically.
func TestCascadeSplitsBlendLogAndUniform(t *testing.T) {
	const near, far = float32(0.5), float32(500)
	splits := cascadeSplitDistances(near, far, cascadeCount, defaultCascadeLambda)

	if splits[0] != near {
		t.Errorf("splits[0] = %v, want the camera near plane %v", splits[0], near)
	}
	if splits[cascadeCount] != far {
		t.Errorf("last split = %v, want the camera far plane %v", splits[cascadeCount], far)
	}
	for i := 0; i < cascadeCount; i++ {
		if splits[i+1] <= splits[i] {
			t.Fatalf("splits are not increasing: %v", splits)
		}
	}

	logOnly := cascadeSplitDistances(near, far, cascadeCount, 1)
	uniformOnly := cascadeSplitDistances(near, far, cascadeCount, 0)
	for i := 1; i < cascadeCount; i++ {
		if splits[i] < logOnly[i] || splits[i] > uniformOnly[i] {
			t.Errorf("split %d = %v, want between the log split %v and the uniform split %v",
				i, splits[i], logOnly[i], uniformOnly[i])
		}
	}
	// Uniform splits divide the range evenly, and log splits multiply it by a
	// constant ratio. Checking one of each proves lambda selects the schedule.
	wantUniform := near + (far-near)/float32(cascadeCount)
	if math.Abs(float64(uniformOnly[1]-wantUniform)) > 1e-2 {
		t.Errorf("uniform first cascade far = %v, want %v", uniformOnly[1], wantUniform)
	}
	wantLog := near * float32(math.Pow(float64(far/near), 1.0/float64(cascadeCount)))
	if math.Abs(float64(logOnly[1]-wantLog)) > 1e-2 {
		t.Errorf("log first cascade far = %v, want %v", logOnly[1], wantLog)
	}
}

// TestCascadeSplitsTrackCameraRange proves the schedule is no longer pinned to
// the old fixed 6 / 22 boundaries: a small scene and a large scene must get
// different cascade edges.
func TestCascadeSplitsTrackCameraRange(t *testing.T) {
	small := cascadeSplitDistances(0.1, 10, cascadeCount, defaultCascadeLambda)
	large := cascadeSplitDistances(0.1, 1000, cascadeCount, defaultCascadeLambda)
	if small[1] >= large[1] {
		t.Fatalf("first split did not scale with the camera far plane: %v vs %v", small[1], large[1])
	}
	if small[cascadeCount] != 10 || large[cascadeCount] != 1000 {
		t.Fatalf("cascade coverage must reach the far plane: %v %v", small, large)
	}
}

// TestCascadeSplitsGuardDegenerateCameras keeps the schedule usable when a
// bundle carries a broken camera.
func TestCascadeSplitsGuardDegenerateCameras(t *testing.T) {
	for _, tc := range []struct{ near, far float32 }{
		{0, 0}, {-5, -1}, {100, 1}, {1, 1},
	} {
		splits := cascadeSplitDistances(tc.near, tc.far, cascadeCount, defaultCascadeLambda)
		for i := 0; i < cascadeCount; i++ {
			if splits[i+1] < splits[i] {
				t.Fatalf("near=%v far=%v produced decreasing splits %v", tc.near, tc.far, splits)
			}
			if math.IsNaN(float64(splits[i+1])) || math.IsInf(float64(splits[i+1]), 0) {
				t.Fatalf("near=%v far=%v produced %v", tc.near, tc.far, splits)
			}
		}
	}
}

// TestFrameKeepsScaledInstanceOutOfTheCullDrop is the end-to-end guard for
// defect F. The scaled instance sits far enough off-axis that a constant radius
// culls it, and close enough that its scaled bounding sphere still crosses the
// frustum. It must survive.
func TestFrameKeepsScaledInstanceOutOfTheCullDrop(t *testing.T) {
	// A unit cube's unscaled radius is about 0.91 with the safety margin. Place
	// the instance so its centre is outside the frustum but its 12x sphere is
	// not.
	const offset = 6.0
	scaled := engine.RenderBundle{
		Camera:    engine.RenderCamera{Z: 10, FOV: math.Pi / 4, Near: 0.1, Far: 100},
		Materials: []engine.RenderMaterial{{Kind: "standard", Color: "#ffffff"}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "giant",
			Kind:          "cube",
			Size:          1,
			MaterialIndex: 0,
			InstanceCount: 1,
			Transforms:    scaledTransform(12, offset, 0, 0),
		}},
	}
	if got := cullSurvivorCount(t, scaled); got != 1 {
		t.Fatalf("scaled instance survivors = %d, want 1; the cull ignored its scale", got)
	}

	// The same instance at unit scale is genuinely outside the frustum and must
	// be dropped, which proves the cull is doing real work.
	unscaled := scaled
	unscaled.InstancedMeshes = []engine.RenderInstancedMesh{scaled.InstancedMeshes[0]}
	unscaled.InstancedMeshes[0].Transforms = scaledTransform(1, offset, 0, 0)
	if got := cullSurvivorCount(t, unscaled); got != 0 {
		t.Fatalf("unit-scale instance survivors = %d, want 0", got)
	}
}

// cullSurvivorCount renders one frame on the headless device and reports how
// many instances the GPU-driven cull kept for the first mesh. The headless
// device runs the same bounding-sphere test the WGSL runs, so this reads the
// real compaction result.
func cullSurvivorCount(t *testing.T, b engine.RenderBundle) int {
	t.Helper()
	device, surface := headless.New(64, 64)
	r, err := New(Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()
	if err := r.Frame(b, 64, 64, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if len(r.meshStates) == 0 || r.meshStates[0].cull == nil {
		t.Fatal("mesh has no cull resources")
	}
	args, err := r.meshStates[0].cull.drawArgsBuf.ReadAsync(16)
	if err != nil {
		t.Fatalf("read draw args: %v", err)
	}
	return int(binary.LittleEndian.Uint32(args[4:8]))
}

// TestFrameCascadeCasterCullDropsOffVolumeInstances is the end-to-end guard for
// defect G. Cascade 0 covers only the slice of the view frustum nearest the
// camera, so an instance parked far down the view axis must not be drawn into
// it, while the far cascade must still keep it.
func TestFrameCascadeCasterCullDropsOffVolumeInstances(t *testing.T) {
	// Two instances: one right in front of the camera, one 150 units away.
	transforms := append(scaledTransform(1, 0, 0, 0), scaledTransform(1, 0, 0, -150)...)
	b := engine.RenderBundle{
		Camera:    engine.RenderCamera{Z: 2, FOV: math.Pi / 3, Near: 0.1, Far: 300},
		Materials: []engine.RenderMaterial{{Kind: "standard", Color: "#ffffff"}},
		Lights: []engine.RenderLight{{
			Kind: "directional", Color: "#ffffff", Intensity: 1,
			DirectionX: 0, DirectionY: -1, DirectionZ: 0,
		}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "pair",
			Kind:          "cube",
			Size:          1,
			MaterialIndex: 0,
			InstanceCount: 2,
			Transforms:    transforms,
			CastShadow:    true,
		}},
	}

	device, surface := headless.New(64, 64)
	r, err := New(Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()
	if err := r.Frame(b, 64, 64, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	cull := r.meshStates[0].cull
	if cull == nil {
		t.Fatal("mesh has no cull resources")
	}
	var casters [cascadeCount]int
	for cascade := 0; cascade < cascadeCount; cascade++ {
		if cull.casters[cascade] == nil {
			t.Fatalf("cascade %d has no caster cull resources", cascade)
		}
		args, err := cull.casters[cascade].drawArgsBuf.ReadAsync(16)
		if err != nil {
			t.Fatalf("read cascade %d draw args: %v", cascade, err)
		}
		casters[cascade] = int(binary.LittleEndian.Uint32(args[4:8]))
	}
	if casters[0] != 1 {
		t.Errorf("cascade 0 casters = %d, want 1 (the distant instance is off-volume)", casters[0])
	}
	if casters[cascadeCount-1] != 2 {
		t.Errorf("last cascade casters = %d, want both instances", casters[cascadeCount-1])
	}
	total := 0
	for _, n := range casters {
		total += n
	}
	if total >= cascadeCount*2 {
		t.Errorf("caster draws total %d across %d cascades; per-cascade culling saved nothing",
			total, cascadeCount)
	}
}

// TestExtractFrustumPlanesNearPlaneMatchesProjection pins the near-plane fix.
// Both projections in this package put clip-space z between -1 and 1, so the
// near plane is r3 + r2. The plain r2 form places the plane at the midpoint of an
// orthographic depth range, which threw away half the volume.
func TestExtractFrustumPlanesNearPlaneMatchesProjection(t *testing.T) {
	// Symmetric orthographic box: 20 wide on x and y, depth 1 to 101 along -Z.
	planes := extractFrustumPlanes(mat4Orthographic(20, 1, 101))
	inside := func(p [3]float32) bool {
		for _, plane := range planes {
			if plane[0]*p[0]+plane[1]*p[1]+plane[2]*p[2]+plane[3] < 0 {
				return false
			}
		}
		return true
	}
	if !inside([3]float32{0, 0, -2}) {
		t.Error("a point just past the near plane must be inside")
	}
	if !inside([3]float32{0, 0, -50}) {
		t.Error("a point mid-volume must be inside")
	}
	if !inside([3]float32{0, 0, -100}) {
		t.Error("a point just before the far plane must be inside")
	}
	if inside([3]float32{0, 0, 5}) {
		t.Error("a point behind the near plane must be outside")
	}
	if inside([3]float32{0, 0, -200}) {
		t.Error("a point past the far plane must be outside")
	}
	if inside([3]float32{50, 0, -50}) {
		t.Error("a point outside the lateral extent must be outside")
	}
}

// TestExtractFrustumPlanesKeepsPerspectiveNearPlane checks the same change on a
// perspective projection: the near plane must sit at the camera near distance,
// not at twice it.
func TestExtractFrustumPlanesKeepsPerspectiveNearPlane(t *testing.T) {
	const near, far = float32(0.5), float32(100)
	planes := extractFrustumPlanes(mat4Perspective(math.Pi/3, 1, near, far))
	nearPlane := planes[4]
	// The camera sits at the origin looking down -Z, so the plane normal points
	// along -Z and the plane crosses the axis exactly at the near distance.
	if math.Abs(float64(nearPlane[2]+1)) > 1e-4 {
		t.Fatalf("near plane normal = %v, want (0, 0, -1)", nearPlane)
	}
	if math.Abs(float64(nearPlane[3]+near)) > 1e-3 {
		t.Fatalf("near plane crosses -Z at %v, want the near distance %v", -nearPlane[3], near)
	}
	distance := func(z float32) float32 {
		return nearPlane[2]*z + nearPlane[3]
	}
	if distance(-near-0.01) < 0 {
		t.Error("a point just past the near plane must be inside")
	}
	if distance(-near+0.01) > 0 {
		t.Error("a point just before the near plane must be outside")
	}
}

// TestConfigShadowCascadeLambdaSelectsTheSchedule proves the cascade schedule is
// no longer hardcoded: a host can pull the cascade edges toward the camera.
func TestConfigShadowCascadeLambdaSelectsTheSchedule(t *testing.T) {
	if got := resolveCascadeLambda(nil); got != defaultCascadeLambda {
		t.Fatalf("default lambda = %v, want %v", got, defaultCascadeLambda)
	}
	logarithmic := 1.0
	if got := resolveCascadeLambda(&logarithmic); got != 1 {
		t.Fatalf("configured lambda = %v, want 1", got)
	}
	uniform := 0.0
	if got := resolveCascadeLambda(&uniform); got != 0 {
		t.Fatalf("configured lambda = %v, want 0", got)
	}

	device := newNullDevice()
	r, err := New(Config{Device: device, Surface: fakeSurface{}, ShadowCascadeLambda: &logarithmic})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()
	if r.cascadeLambda != 1 {
		t.Fatalf("renderer lambda = %v, want 1", r.cascadeLambda)
	}

	cam := engine.RenderCamera{Z: 5, FOV: math.Pi / 3, Near: 0.1, Far: 400}
	light := [3]float32{-0.4, -1, -0.3}
	tight := computeCascades(cam, light, 1, 1)
	loose := computeCascades(cam, light, 0, 1)
	if tight.farSplits[0] >= loose.farSplits[0] {
		t.Fatalf("logarithmic first split %v should sit closer than uniform %v",
			tight.farSplits[0], loose.farSplits[0])
	}
	if tight.farSplits[cascadeCount-1] != loose.farSplits[cascadeCount-1] {
		t.Fatalf("both schedules must reach the far plane: %v vs %v",
			tight.farSplits[cascadeCount-1], loose.farSplits[cascadeCount-1])
	}
}
