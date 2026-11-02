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
	"unsafe"
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
	oceanWaterHeader // the number of water frame values; the probe follows
)

// The probe shares the record: the host writes the count and the points, the page the answer and the per-point results
// (height, normal x, normal z). Keep these in step with HP in client/runtime/scene3d/ocean-fft.ts.
const (
	oceanProbeCount      = 10
	oceanProbeSeq        = 11
	oceanProbeGeneration = 12
	oceanProbeFlags      = 13
	oceanProbeN          = 14
	oceanProbeLatency    = 15
	oceanProbeAtMS       = 16
	oceanProbeReadings   = 17
	oceanProbePoints     = 18
	oceanProbeResults    = oceanProbePoints + 2*scene.MaxOceanProbePoints
	oceanProbeRequest    = oceanProbeResults + 3*scene.MaxOceanProbePoints // the host's request counter
	oceanProbeEcho       = oceanProbeRequest + 1                           // the counter of the request the reading answers
	oceanWaterFloats     = oceanProbeEcho + 1
)

// OceanWaterReader reads where a mounted scene draws its water, without
// allocating in Go. Get one from Scene3D.OceanWater once, then call Read each
// frame. In an engine worker the page posts each frame to the worker as a
// message; that relay allocates in the browser, not in Read.
type OceanWaterReader struct {
	record js.Value
	// views of the record's bytes for the probe, and the worker's function that tells the page the host wrote its points
	count, points, answer, results, request, echo js.Value
	send                                          js.Value
	requests                                      uint64
}

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
	buffer, uint8 := value.Get("buffer"), js.Global().Get("Uint8Array")
	view := func(first, n int) js.Value { return uint8.New(buffer, first*8, n*8) }
	return &OceanWaterReader{
		record: value, send: value.Get("send"),
		count: view(oceanProbeCount, 1), points: view(oceanProbePoints, 2*scene.MaxOceanProbePoints),
		answer: view(oceanProbeSeq, oceanProbeReadings-oceanProbeSeq+1), results: view(oceanProbeResults, 3*scene.MaxOceanProbePoints),
		request: view(oceanProbeRequest, 1), echo: view(oceanProbeEcho, 1),
	}, nil
}

// Read returns the latest water frame. It reports false until the page has
// submitted a frame with an ocean. After the ocean is removed it keeps
// returning the last frame; see scene.OceanWaterFrame. It does not allocate.
func (r *OceanWaterReader) Read() (scene.OceanWaterFrame, bool) {
	var v [oceanWaterHeader]float64
	for i := range v {
		v[i] = r.record.Index(i).Float()
	}
	return scene.OceanWaterFrame{
		X: v[oceanWaterX], Z: v[oceanWaterZ], Time: v[oceanWaterTime], Spectral: v[oceanWaterSpectral] != 0,
		TimeScale: v[oceanWaterTimeScale], WaterDriftX: v[oceanWaterDriftX], WaterDriftZ: v[oceanWaterDriftZ],
		Seq: uint64(v[oceanWaterSeq]), Generation: uint64(v[oceanWaterGeneration]), PageEpochMS: v[oceanWaterPageEpochMS],
	}, v[oceanWaterSeq] != 0
}

func floatBytes(v []float64) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(v))), len(v)*8)
}

// SetProbe asks the page to read the drawn sea back at these world points (at most scene.MaxOceanProbePoints;
// more are ignored), from the next frame it draws on. Nothing is read back, and the page does no work for the
// probe, until a call with at least one point, or after a call with none. Call it as often as the points move;
// it does not allocate in Go. Read the answer with ReadProbe. See scene.OceanProbeReading for what the heights
// are and for the latency. The reading is for drawing only.
func (r *OceanWaterReader) SetProbe(points []scene.OceanProbePoint) {
	n := min(len(points), scene.MaxOceanProbePoints)
	var buf [2 * scene.MaxOceanProbePoints]float64
	for i := 0; i < n; i++ {
		buf[2*i], buf[2*i+1] = points[i].X, points[i].Z
	}
	count, request := [1]float64{float64(n)}, [1]float64{}
	r.requests++
	request[0] = float64(r.requests)
	js.CopyBytesToJS(r.points, floatBytes(buf[:2*n]))
	js.CopyBytesToJS(r.request, floatBytes(request[:]))
	js.CopyBytesToJS(r.count, floatBytes(count[:]))
	if r.send.Type() == js.TypeFunction {
		r.send.Invoke() // an engine worker: tell the page
	}
}

// ReadProbe fills out with the newest reading the page has written. It reports false until the page has
// written one. It does not allocate.
func (r *OceanWaterReader) ReadProbe(out *scene.OceanProbeReading) bool {
	var head [oceanProbeReadings - oceanProbeSeq + 1]float64
	js.CopyBytesToGo(floatBytes(head[:]), r.answer)
	if head[oceanProbeReadings-oceanProbeSeq] == 0 {
		return false
	}
	flags := int(head[oceanProbeFlags-oceanProbeSeq])
	out.Seq, out.Generation = uint64(head[0]), uint64(head[oceanProbeGeneration-oceanProbeSeq])
	out.Count, out.LatencyFrames = int(head[oceanProbeN-oceanProbeSeq]), uint32(head[oceanProbeLatency-oceanProbeSeq])
	out.PageEpochMS, out.Readings = head[oceanProbeAtMS-oceanProbeSeq], uint64(head[oceanProbeReadings-oceanProbeSeq])
	out.Gerstner, out.Unavailable = flags&2 != 0, flags&4 != 0
	out.Count = min(out.Count, scene.MaxOceanProbePoints)
	var echo [1]float64
	js.CopyBytesToGo(floatBytes(echo[:]), r.echo)
	out.Request = uint64(echo[0])
	var res [3 * scene.MaxOceanProbePoints]float64
	js.CopyBytesToGo(floatBytes(res[:]), r.results)
	for i := 0; i < out.Count; i++ {
		out.Height[i], out.NormalX[i], out.NormalZ[i] = res[3*i], res[3*i+1], res[3*i+2]
	}
	return true
}

// Close stops the probe: the page stops reading points back for this scene. Call it before dropping a reader that
// used SetProbe, or the page keeps asking until the scene ends. Close is SetProbe(nil).
func (r *OceanWaterReader) Close() { r.SetProbe(nil) }
