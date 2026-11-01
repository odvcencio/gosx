//go:build js && wasm

package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"m31labs.dev/gosx/client/jsutil"
	"m31labs.dev/gosx/scene"
	"strings"
	"syscall/js"
)

// Scene3D is a command target scoped to a mounted GoSX engine. It never exposes
// the renderer's mutable handle or its internal object cache.
type Scene3D struct {
	owner Context
	mount js.Value
}

func (c Context) Scene3D(mount js.Value) Scene3D { return Scene3D{owner: c, mount: mount} }

// Scene3DByID targets the Scene3D mount element with the given DOM id. Use it
// in a worker-thread engine, which has no document to look the element up in;
// the page resolves the id. It works on the main thread too.
func (c Context) Scene3DByID(id string) Scene3D { return c.Scene3D(js.ValueOf(id)) }

func (s Scene3D) call(method string, args ...any) (value js.Value, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("Scene3D %s: %v", method, failure)
		}
	}()
	if !s.owner.IsCurrent() {
		return js.Undefined(), context.Canceled
	}
	all := []any{method, s.mount}
	all = append(all, args...)
	return s.owner.value.Call("scene3D", all...), nil
}

func (s Scene3D) async(ctx context.Context, method string, args ...any) error {
	if ctx == nil {
		return fmt.Errorf("Scene3D context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	controller := js.Global().Get("AbortController").New()
	defer controller.Call("abort")
	args = append(args, map[string]any{"signal": controller.Get("signal")})
	promise, err := s.call(method, args...)
	if err != nil {
		return err
	}
	_, err = jsutil.AwaitPromiseContext(ctx, promise)
	return err
}

// Ready waits for a mounted command renderer without polling from application code.
func (s Scene3D) Ready(ctx context.Context) error { return s.async(ctx, "whenReady") }

// Dispatch applies typed commands to this scene and awaits renderer acceptance.
func (s Scene3D) Dispatch(ctx context.Context, commands []scene.Command) error {
	if commands == nil {
		commands = []scene.Command{}
	}
	value, err := encodeEventDetail(commands)
	if err != nil {
		return err
	}
	return s.async(ctx, "dispatchCommands", value)
}

// Live delivers one named Live event to this scene. It takes the same path as
// a hub event the scene listens for, so scene.Live nodes, model animation and
// audio cues react to it without a hub or an internal document event. data is
// JSON-encoded; nil sends no data.
func (s Scene3D) Live(ctx context.Context, event string, data any) error {
	if strings.TrimSpace(event) == "" {
		return fmt.Errorf("Scene3D Live event name is required")
	}
	value := js.Null()
	if data != nil {
		var err error
		if value, err = encodeEventDetail(data); err != nil {
			return err
		}
	}
	return s.async(ctx, "live", event, value)
}

// Camera returns the effective mounted camera, including user orbit/zoom.
func (s Scene3D) Camera() (camera scene.IRCamera, err error) {
	value, err := s.call("getCamera")
	if err != nil {
		return camera, err
	}
	if !value.Truthy() {
		return camera, fmt.Errorf("Scene3D camera is not ready")
	}
	err = json.Unmarshal([]byte(js.Global().Get("JSON").Call("stringify", value).String()), &camera)
	return camera, err
}

// SetCamera updates both renderer and interactive camera controls coherently.
// A camera with TransitionMS above zero eases to its pose in the page over that
// many milliseconds, and SetCamera returns at once. A later camera replaces the
// move from the pose on screen; one without TransitionMS cuts and ends it, and
// so does the player's drag, wheel or key under orbit or fly controls, or the
// wheel, zoom keys, pinch or pan of ControlZoom. Reduced motion and a change of
// camera kind cut. Set TransitionMS to zero on a camera you send every frame;
// otherwise each frame starts a new move.
func (s Scene3D) SetCamera(ctx context.Context, camera any) error {
	value, err := encodeEventDetail(scene.SetCameraCommand(camera).Data)
	if err != nil {
		return err
	}
	return s.async(ctx, "setCamera", value)
}

// SetOrbit changes the orbit controls' target and limits after mount. The camera
// keeps its position and turns to face the new target, and the player's mouse
// orbit continues around it. It needs Props.Controls = scene.ControlOrbit.
// Moving the camera and the target together is one SetCamera call with
// PerspectiveCamera.Orbit set. A move that TransitionMS started carries on
// around the new target.
func (s Scene3D) SetOrbit(ctx context.Context, orbit scene.Orbit) error {
	value, err := encodeEventDetail(orbit)
	if err != nil {
		return err
	}
	return s.async(ctx, "setOrbit", value)
}

// SetAnimationClock controls the scene clock without exposing renderer internals.
func (s Scene3D) SetAnimationClock(ctx context.Context, seconds float64, paused bool) error {
	return s.async(ctx, "setAnimationClock", map[string]any{"timeSeconds": seconds, "paused": paused})
}

// The page keeps a scene's water frame in a Float64Array of oceanWaterFloats values, in this order.
const (
	oceanWaterSeq = iota
	oceanWaterTime
	oceanWaterX
	oceanWaterZ
	oceanWaterSpectral
	oceanWaterTimeScale
	oceanWaterDriftX
	oceanWaterDriftZ
	oceanWaterPageEpochMS
	oceanWaterGeneration
	oceanWaterFloats
)

// OceanWaterReader reads where a mounted scene draws its water, without
// allocating in Go. Get one from Scene3D.OceanWater once, then call Read each
// frame. In an engine worker the page posts each frame to the worker as a
// message; that relay allocates in the browser, not in Read.
type OceanWaterReader struct{ record js.Value }

// OceanWater returns a reader for the water frame of this scene's ocean: the
// origin and wave time the renderer drew the sea with, on WebGPU and WebGL2
// (see scene.OceanWaterFrame for the fields, what the frame does and does not
// give, the Gerstner fallback and the latency). A host that drives a vessel's
// pose itself (an external-pose vessel) uses it to stay in phase with the sea
// the player sees:
//
//	frame, ok := reader.Read()
//	q.SampleWater(frame, x, z) // q is a *scene.OceanQuery; swell waves only
//
// The reading is for drawing only. Presentation never feeds a simulation: keep
// it out of game rules, saves and replays.
//
// Call it after Ready. The page starts recording a frame for this scene when
// the reader is created; until the renderer has submitted an ocean frame, Read
// reports false. Take one reader per scene and keep it. Several engines, or
// one engine and the main thread, may each take a reader for the same mount.
// The page keeps one spectral ocean per page: with two mounted spectral scenes
// the frames are not told apart.
func (s Scene3D) OceanWater() (*OceanWaterReader, error) {
	value, err := s.call("oceanWater")
	if err != nil {
		return nil, err
	}
	if !value.Truthy() {
		return nil, fmt.Errorf("Scene3D ocean water is not ready")
	}
	return &OceanWaterReader{record: value}, nil
}

// Read returns the latest water frame. It reports false until the page has
// submitted a frame with an ocean. After the ocean is removed it keeps
// returning the last frame; see scene.OceanWaterFrame. It does not allocate.
func (r *OceanWaterReader) Read() (scene.OceanWaterFrame, bool) {
	var v [oceanWaterFloats]float64
	for i := range v {
		v[i] = r.record.Index(i).Float()
	}
	return scene.OceanWaterFrame{
		X: v[oceanWaterX], Z: v[oceanWaterZ], Time: v[oceanWaterTime], Spectral: v[oceanWaterSpectral] != 0,
		TimeScale: v[oceanWaterTimeScale], WaterDriftX: v[oceanWaterDriftX], WaterDriftZ: v[oceanWaterDriftZ],
		Seq: uint64(v[oceanWaterSeq]), Generation: uint64(v[oceanWaterGeneration]), PageEpochMS: v[oceanWaterPageEpochMS],
	}, v[oceanWaterSeq] != 0
}
