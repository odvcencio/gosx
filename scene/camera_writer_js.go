//go:build js && wasm

package scene

import (
	"errors"
	"fmt"
	"math"
	"syscall/js"
)

// CameraWriter moves a mounted Scene3D's camera with one synchronous call:
// no JSON, no promise and, after the first frame, no allocation. Use it for a
// camera that moves every frame, and inside the renderer's frame hook
// (host.SceneFrameSource), where wasm.Scene3D.SetCamera cannot run because it
// waits on a promise.
//
//	cam := scene.NewCameraWriter("arena")
//	driver.Start(func(alpha float64, frame game.Frame) {
//		if cam.Prepare() {
//			_, _ = cam.ApplyPerspective(director.Update(frame.Delta.Seconds()).Perspective(0.1, 500))
//		}
//	})
//
// Prepare finds the renderer and reports whether Apply can run; call it once
// per frame. It picks up a new renderer after a remount or a restored
// graphics context. A writer belongs to one browser event loop.
//
// In an engine worker there is no renderer to call. Apply then forwards the
// camera to the page, which applies it one message later.
type CameraWriter struct {
	surface *Surface
	handle  js.Value
	camera  js.Value
	last    IRCamera
	sent    bool
	ready   bool
	worker  bool
}

// NewCameraWriter returns a writer for the Scene3D mount with the given
// element ID.
func NewCameraWriter(target string) *CameraWriter {
	return &CameraWriter{surface: NewSurface(target)}
}

// Prepare reports whether the renderer can take a camera now. It is false
// while the mount is loading.
func (w *CameraWriter) Prepare() (ready bool) {
	defer func() {
		if recover() != nil {
			if w != nil {
				w.ready = false
			}
			ready = false
		}
	}()
	if w == nil {
		return false
	}
	w.ready = false
	if scene := workerScene3D(); scene.Truthy() {
		w.worker = true
		w.ready = true
		return true
	}
	handle, err := w.surface.renderer()
	if err != nil || handle.Get("setCamera").Type() != js.TypeFunction {
		return false
	}
	if !w.handle.Equal(handle) {
		// A new renderer knows none of the values the old one was sent.
		w.handle = handle
		w.sent = false
	}
	w.ready = true
	return true
}

// Apply moves the camera. It returns only after the renderer has taken the
// values, so the frame being built uses them. It reports false, with no
// error, when Prepare has not succeeded or the renderer kept its camera
// because nothing changed.
//
// The zero values of Kind, FOV, Zoom, Near and Far mean "perspective", 75
// degrees, 1, 0.05 and 128, as in scene commands. TransitionMS above zero
// asks the renderer to ease; a camera that moves every frame leaves it zero.
// Orbit is not carried: move an orbit target with wasm.Scene3D.SetOrbit.
func (w *CameraWriter) Apply(camera IRCamera) (applied bool, err error) {
	defer func() {
		if value := recover(); value != nil {
			applied, err = false, fmt.Errorf("scene: camera writer: %v", value)
		}
	}()
	if w == nil || !w.ready {
		return false, nil
	}
	if camera.Orbit != nil {
		return false, errors.New("scene: camera writer does not carry an orbit; use Scene3D.SetOrbit")
	}
	c := cameraWriterDefaults(camera)
	if !finiteCamera(c) {
		return false, errors.New("scene: camera writer needs finite camera values")
	}
	if !w.camera.Truthy() {
		w.camera = js.Global().Get("Object").New()
		w.sent = false
	}
	if w.sent && c == w.last {
		return false, nil
	}
	w.write(c)
	w.last, w.sent = c, true
	if w.worker {
		// The page applies it; the returned promise is not awaited.
		workerScene3D().Call("setCamera", w.surface.target, w.camera)
		return true, nil
	}
	return w.handle.Call("setCamera", w.camera).Truthy(), nil
}

// ApplyPerspective is Apply for a PerspectiveCamera, the type the game
// camera director returns (camera.Pose.Perspective).
func (w *CameraWriter) ApplyPerspective(camera PerspectiveCamera) (applied bool, err error) {
	if camera.Orbit != nil {
		return false, errors.New("scene: camera writer does not carry an orbit; use Scene3D.SetOrbit")
	}
	return w.Apply(IRCamera{
		Kind: "perspective",
		X:    camera.Position.X, Y: camera.Position.Y, Z: camera.Position.Z,
		RotationX: camera.Rotation.X, RotationY: camera.Rotation.Y, RotationZ: camera.Rotation.Z,
		FOV: camera.FOV, PortraitFOV: camera.PortraitFOV,
		Near: camera.Near, Far: camera.Far, TransitionMS: camera.TransitionMS,
	})
}

// write copies the fields that changed since the last call onto the reused
// JavaScript object. Every field is written the first time.
func (w *CameraWriter) write(c IRCamera) {
	all := !w.sent
	old := &w.last
	if all || c.Kind != old.Kind {
		w.camera.Set("kind", c.Kind)
	}
	setNumber := func(name string, next, previous float64) {
		if all || next != previous {
			w.camera.Set(name, next)
		}
	}
	setNumber("x", c.X, old.X)
	setNumber("y", c.Y, old.Y)
	setNumber("z", c.Z, old.Z)
	setNumber("rotationX", c.RotationX, old.RotationX)
	setNumber("rotationY", c.RotationY, old.RotationY)
	setNumber("rotationZ", c.RotationZ, old.RotationZ)
	setNumber("fov", c.FOV, old.FOV)
	setNumber("portraitFOV", c.PortraitFOV, old.PortraitFOV)
	setNumber("left", c.Left, old.Left)
	setNumber("right", c.Right, old.Right)
	setNumber("top", c.Top, old.Top)
	setNumber("bottom", c.Bottom, old.Bottom)
	setNumber("zoom", c.Zoom, old.Zoom)
	setNumber("near", c.Near, old.Near)
	setNumber("far", c.Far, old.Far)
	setNumber("transitionMS", c.TransitionMS, old.TransitionMS)
}

// cameraWriterDefaults fills the same defaults scene commands use, so a
// zero field resets rather than keeping the renderer's last value.
func cameraWriterDefaults(c IRCamera) IRCamera {
	if c.Kind == "" {
		c.Kind = "perspective"
	}
	if c.FOV == 0 {
		c.FOV = 75
	}
	if c.Zoom == 0 {
		c.Zoom = 1
	}
	if c.Near == 0 {
		c.Near = .05
	}
	if c.Far == 0 {
		c.Far = 128
	}
	c.Orbit = nil
	return c
}

func finiteCamera(c IRCamera) bool {
	for _, v := range [...]float64{c.X, c.Y, c.Z, c.RotationX, c.RotationY, c.RotationZ, c.FOV, c.PortraitFOV,
		c.Left, c.Right, c.Top, c.Bottom, c.Zoom, c.Near, c.Far, c.TransitionMS} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}
