//go:build js && wasm

package scene

import (
	"math"
	"syscall/js"
	"testing"
)

// cameraFixture adds a setCamera stand-in to the surface fixture's renderer
// handle. It keeps a copy of every camera it is given and the object itself.
func cameraFixture(t *testing.T) js.Value {
	t.Helper()
	f := surfaceFixture(t)
	js.Global().Get("Function").New("f", `
f.cameras = []; f.objects = [];
f.addCamera = handle => { handle.setCamera = camera => { f.cameras.push(Object.assign({}, camera)); f.objects.push(camera); return true; }; };
f.addCamera(f.handle);
`).Invoke(f)
	return f
}

func cameraNumber(t *testing.T, camera js.Value, name string) float64 {
	t.Helper()
	v := camera.Get(name)
	if v.Type() != js.TypeNumber {
		t.Fatalf("camera.%s is %v, want a number", name, v.Type())
	}
	return v.Float()
}

func TestCameraWriterAppliesSynchronouslyAndSkipsUnchangedCameras(t *testing.T) {
	f := cameraFixture(t)
	w := NewCameraWriter("stage")
	if applied, err := w.Apply(IRCamera{X: 1}); applied || err != nil {
		t.Fatalf("Apply before Prepare = %v, %v; want false, nil", applied, err)
	}
	if !w.Prepare() {
		t.Fatal("writer is not ready for a mounted renderer")
	}
	first := IRCamera{X: 1, Y: 2, Z: 3, RotationX: -0.25, RotationY: 0.5, FOV: 50, Near: 0.25, Far: 400}
	applied, err := w.Apply(first)
	if err != nil || !applied {
		t.Fatalf("first Apply = %v, %v", applied, err)
	}
	cameras := f.Get("cameras")
	if cameras.Length() != 1 {
		t.Fatalf("renderer saw %d cameras after one Apply", cameras.Length())
	}
	got := cameras.Index(0)
	if got.Get("kind").String() != "perspective" {
		t.Fatalf("kind = %q, want the perspective default", got.Get("kind").String())
	}
	// Given values arrive unchanged; zoom takes its default of 1 and the
	// unset fields are sent as zero, so they reset.
	want := map[string]float64{"x": 1, "y": 2, "z": 3, "rotationX": -0.25, "rotationY": 0.5, "rotationZ": 0,
		"fov": 50, "portraitFOV": 0, "left": 0, "right": 0, "top": 0, "bottom": 0, "zoom": 1, "near": 0.25, "far": 400, "transitionMS": 0}
	for name, value := range want {
		if v := cameraNumber(t, got, name); v != value {
			t.Errorf("camera.%s = %v, want %v", name, v, value)
		}
	}
	// The same camera again is not sent.
	if applied, err := w.Apply(first); applied || err != nil {
		t.Fatalf("unchanged Apply = %v, %v; want false, nil", applied, err)
	}
	if cameras.Length() != 1 {
		t.Fatal("an unchanged camera reached the renderer")
	}
	// One changed field: the renderer gets the same object, with that field
	// updated and the rest as before.
	second := first
	second.Z = -7.5
	if applied, err := w.Apply(second); err != nil || !applied {
		t.Fatalf("changed Apply = %v, %v", applied, err)
	}
	if cameras.Length() != 2 || !f.Get("objects").Index(0).Equal(f.Get("objects").Index(1)) {
		t.Fatal("the writer did not reuse its camera object")
	}
	next := cameras.Index(1)
	if cameraNumber(t, next, "z") != -7.5 || cameraNumber(t, next, "x") != 1 || cameraNumber(t, next, "fov") != 50 {
		t.Fatalf("second camera = x %v z %v fov %v", next.Get("x"), next.Get("z"), next.Get("fov"))
	}
	if f.Get("queries").Int() != 1 {
		t.Fatalf("Apply looked the mount up %d times; only Prepare may", f.Get("queries").Int())
	}
}

func TestCameraWriterAppliesAPerspectiveCamera(t *testing.T) {
	f := cameraFixture(t)
	w := NewCameraWriter("stage")
	if !w.Prepare() {
		t.Fatal("not ready")
	}
	camera := PerspectiveCamera{Position: Vec3(1.5, 6, -20), Rotation: Rotate(-0.3, 0.75, 0.125), FOV: 42, PortraitFOV: 60, Near: 0.5, Far: 600}
	if applied, err := w.ApplyPerspective(camera); err != nil || !applied {
		t.Fatalf("ApplyPerspective = %v, %v", applied, err)
	}
	got := f.Get("cameras").Index(0)
	want := map[string]float64{"x": 1.5, "y": 6, "z": -20, "rotationX": -0.3, "rotationY": 0.75, "rotationZ": 0.125,
		"fov": 42, "portraitFOV": 60, "near": 0.5, "far": 600, "zoom": 1, "transitionMS": 0}
	for name, value := range want {
		if v := cameraNumber(t, got, name); v != value {
			t.Errorf("camera.%s = %v, want %v", name, v, value)
		}
	}
	if got.Get("kind").String() != "perspective" {
		t.Fatalf("kind = %q", got.Get("kind").String())
	}
	camera.Orbit = &Orbit{}
	if applied, err := w.ApplyPerspective(camera); applied || err == nil {
		t.Fatalf("ApplyPerspective with an orbit = %v, %v; want an error", applied, err)
	}
}

func TestCameraWriterRejectsOrbitAndNonFiniteValues(t *testing.T) {
	f := cameraFixture(t)
	w := NewCameraWriter("stage")
	if !w.Prepare() {
		t.Fatal("not ready")
	}
	if applied, err := w.Apply(IRCamera{Orbit: &Orbit{}}); applied || err == nil {
		t.Fatalf("Apply with an orbit = %v, %v; want an error", applied, err)
	}
	for name, camera := range map[string]IRCamera{
		"NaN position": {X: math.NaN()},
		"infinite fov": {FOV: math.Inf(1)},
		"NaN rotation": {RotationY: math.NaN()},
	} {
		if applied, err := w.Apply(camera); applied || err == nil {
			t.Errorf("%s: Apply = %v, %v; want an error", name, applied, err)
		}
	}
	if f.Get("cameras").Length() != 0 {
		t.Fatal("a rejected camera reached the renderer")
	}
}

func TestCameraWriterResendsEveryFieldToANewRenderer(t *testing.T) {
	f := cameraFixture(t)
	w := NewCameraWriter("stage")
	if !w.Prepare() {
		t.Fatal("not ready")
	}
	camera := IRCamera{X: 4, Y: 5, Z: 6, FOV: 40}
	if applied, err := w.Apply(camera); err != nil || !applied {
		t.Fatalf("Apply = %v, %v", applied, err)
	}
	// The mount is replaced: a new element with a new renderer handle that
	// has never seen a camera.
	js.Global().Get("Function").New("f", `
f.mount.isConnected = false;
f.handle = { applyInstanceStream() { return { applied: true }; } };
f.addCamera(f.handle);
f.mount = f.newMount();
`).Invoke(f)
	if !w.Prepare() {
		t.Fatal("not ready after the remount")
	}
	if applied, err := w.Apply(camera); err != nil || !applied {
		t.Fatalf("Apply to the new renderer = %v, %v; an unchanged camera must still be sent to a renderer that never had it", applied, err)
	}
	cameras := f.Get("cameras")
	if cameras.Length() != 2 {
		t.Fatalf("renderers saw %d cameras, want 2", cameras.Length())
	}
	if got := cameras.Index(1); cameraNumber(t, got, "x") != 4 || cameraNumber(t, got, "fov") != 40 || cameraNumber(t, got, "zoom") != 1 {
		t.Fatal("the new renderer got an incomplete camera")
	}
	// No renderer: not ready, and Apply does nothing.
	f.Get("mount").Delete("__gosxScene3DHandle")
	if w.Prepare() {
		t.Fatal("ready with no renderer handle")
	}
	if applied, err := w.Apply(IRCamera{X: 9}); applied || err != nil {
		t.Fatalf("Apply with no renderer = %v, %v; want false, nil", applied, err)
	}
}

func TestCameraWriterForwardsToThePageFromAWorker(t *testing.T) {
	f := workerFixture(t)
	js.Global().Get("Function").New("f", `
f.cameras = [];
f.scene3d.setCamera = (target, camera) => { f.cameras.push({ target, camera: Object.assign({}, camera) }); return Promise.resolve(true); };
`).Invoke(f)
	w := NewCameraWriter("arena")
	if !w.Prepare() {
		t.Fatal("a worker camera writer is ready without a renderer")
	}
	if applied, err := w.Apply(IRCamera{X: 2, Z: -30, FOV: 45}); err != nil || !applied {
		t.Fatalf("Apply = %v, %v", applied, err)
	}
	sent := f.Get("cameras").Index(0)
	if sent.Get("target").String() != "arena" {
		t.Fatalf("target = %v", sent.Get("target"))
	}
	if c := sent.Get("camera"); cameraNumber(t, c, "x") != 2 || cameraNumber(t, c, "z") != -30 || cameraNumber(t, c, "fov") != 45 {
		t.Fatal("the page did not receive the camera values")
	}
}
