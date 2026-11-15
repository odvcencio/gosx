package replayui

import (
	"math"
	"strconv"
	"time"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/camera"
	"m31labs.dev/gosx/game/replay"
	"m31labs.dev/gosx/scene"
)

// ContextName is the actions context Context defines.
const ContextName = "replay"

// Action names in Context.
const (
	ActToggle     = "toggle"
	ActStepBack   = "step_back"
	ActStepFwd    = "step_forward"
	ActScrub      = "scrub"
	ActSpeedDown  = "speed_down"
	ActSpeedUp    = "speed_up"
	ActCamNext    = "camera_next"
	ActCamPrev    = "camera_prev"
	ActCamMode    = "camera_mode"
	ActMarkerNext = "marker_next"
	ActMarkerPrev = "marker_prev"
	ActRestart    = "restart"
	ActMoveX      = "move_x"
	ActMoveZ      = "move_z"
	ActMoveY      = "move_y"
	ActLookX      = "look_x"
	ActLookY      = "look_y"
	ActPadLookX   = "pad_look_x"
	ActPadLookY   = "pad_look_y"
	ActZoom       = "zoom"
)

// Context returns the default input context: Space, K or A to play and pause;
// comma and period or the d-pad to step; arrows, J and L or the triggers to
// scrub; minus and equals or d-pad down and up for speed; C, V or Y for the
// camera; F or X to switch between broadcast, orbit and fly; [ and ] or the
// shoulder buttons for markers; WASD, Q and E or the left stick to move the
// free camera; the mouse (with pointer lock) or the right stick to look; the
// wheel to zoom. Rebind them like any other context.
func Context() actions.Context {
	key := func(c string, scale float64) actions.Binding {
		return actions.Binding{Source: actions.Key(c), Scale: scale}
	}
	btn := func(n int, scale float64) actions.Binding {
		return actions.Binding{Source: actions.PadButton(n), Scale: scale}
	}
	axis := func(n int, scale float64) actions.Binding {
		return actions.Binding{Source: actions.PadAxis(n), Scale: scale}
	}
	button := func(name string, b ...actions.Binding) actions.ActionDef {
		return actions.ActionDef{Name: name, Bindings: b}
	}
	ax := func(name string, dz float64, b ...actions.Binding) actions.ActionDef {
		return actions.ActionDef{Name: name, Kind: actions.Axis, Deadzone: dz, Bindings: b}
	}
	return actions.Context{Name: ContextName, Group: "game", Actions: []actions.ActionDef{
		button(ActToggle, key("Space", 0), key("KeyK", 0), btn(actions.PadA, 0)),
		button(ActStepBack, key("Comma", 0), btn(14, 0)),
		button(ActStepFwd, key("Period", 0), btn(15, 0)),
		ax(ActScrub, 0.1, key("ArrowRight", 1), key("ArrowLeft", -1), key("KeyL", 1), key("KeyJ", -1), btn(7, 1), btn(6, -1)),
		button(ActSpeedDown, key("Minus", 0), btn(13, 0)),
		button(ActSpeedUp, key("Equal", 0), btn(12, 0)),
		button(ActCamNext, key("KeyC", 0), btn(3, 0)),
		button(ActCamPrev, key("KeyV", 0)),
		button(ActCamMode, key("KeyF", 0), btn(2, 0)),
		button(ActMarkerNext, key("BracketRight", 0), btn(5, 0)),
		button(ActMarkerPrev, key("BracketLeft", 0), btn(4, 0)),
		button(ActRestart, key("Home", 0)),
		ax(ActMoveX, 0.15, key("KeyD", 1), key("KeyA", -1), axis(0, 1)),
		ax(ActMoveZ, 0.15, key("KeyW", 1), key("KeyS", -1), axis(1, -1)),
		ax(ActMoveY, 0.15, key("KeyE", 1), key("KeyQ", -1)),
		ax(ActLookX, 0, actions.Binding{Source: actions.Motion(actions.CodeMouseDX), Scale: 0.01}),
		ax(ActLookY, 0, actions.Binding{Source: actions.Motion(actions.CodeMouseDY), Scale: 0.01}),
		ax(ActPadLookX, 0.15, axis(2, 1)),
		ax(ActPadLookY, 0.15, axis(3, 1)),
		ax(ActZoom, 0, actions.Binding{Source: actions.Motion(actions.CodeWheelY), Scale: 0.1}),
	}}
}

// Config wires a Controller.
type Config struct {
	Player *replay.Player
	Rig    *replay.Rig
	// Input is read every Update for the "replay" context actions. It may be
	// nil when only the Bar drives the viewer. The caller calls Input.Update.
	Input *actions.Map
	// ScrubRate is how many seconds of replay the scrub axis crosses per second
	// at full deflection. Zero means 6.
	ScrubRate float64
	// LookRate is the turn rate of the right stick in radians per second at full
	// deflection. Mouse look is per pixel and ignores it. Zero means 2.5.
	LookRate float64
	// MarkerLead is how many seconds before a marker a marker jump lands, so the
	// moment plays out. Zero means 2.
	MarkerLead float64
	// OnChange is called after a command changes what View would show.
	OnChange func()
}

// Controller connects input and UI events to a Player and a Rig. It is not
// safe for concurrent use.
type Controller struct {
	cfg      Config
	scrubAcc float64
	camOrder []string // camera option ids in cycle order
	seeks    uint64
}

// New builds a Controller. Player is required; Rig may be nil for a viewer
// with a fixed camera.
func New(cfg Config) *Controller {
	if cfg.ScrubRate == 0 {
		cfg.ScrubRate = 6
	}
	if cfg.LookRate == 0 {
		cfg.LookRate = 2.5
	}
	if cfg.MarkerLead == 0 {
		cfg.MarkerLead = 2
	}
	c := &Controller{cfg: cfg, seeks: cfg.Player.Seeks()}
	c.camOrder = c.cameraIDs()
	return c
}

// Camera option ids. A named shot is "shot:" followed by its name.
const (
	CamChase = "chase"
	CamOrbit = "orbit"
	CamFly   = "fly"
)

func (c *Controller) cameraIDs() []string {
	if c.cfg.Rig == nil {
		return nil
	}
	ids := []string{CamChase}
	for _, s := range c.cfg.Rig.Shots() {
		ids = append(ids, "shot:"+s)
	}
	return append(ids, CamOrbit, CamFly)
}

func (c *Controller) changed() {
	if c.cfg.OnChange != nil {
		c.cfg.OnChange()
	}
}

func (c *Controller) tickRate() float64 {
	if hz := c.cfg.Player.Recording().TickRate(); hz > 0 {
		return hz
	}
	return 60
}

// Frame is what one Update produced for rendering.
type Frame struct {
	// Pose is the camera for this frame; Camera is the same for the Scene3D
	// renderer.
	Pose   camera.Pose
	Camera scene.PerspectiveCamera
	// Alpha is the fraction between the previous and current tick to blend
	// with a replay.Interpolator.
	Alpha float64
	// Ticks is how many ticks playback ran this frame.
	Ticks int
	// Seeked reports that the tick jumped (a seek, restart or marker jump)
	// rather than advancing.
	Seeked bool
}

// Update reads input, advances playback by dt and the camera by dt, and
// returns the frame to render. Call it once per frame.
func (c *Controller) Update(dt time.Duration) Frame {
	if c.cfg.Input != nil {
		c.readInput(dt)
	}
	p := c.cfg.Player
	f := Frame{}
	f.Ticks = p.Advance(dt)
	if s := p.Seeks(); s != c.seeks {
		c.seeks, f.Seeked = s, true
		if c.cfg.Rig != nil {
			c.cfg.Rig.Snap()
		}
	}
	f.Alpha = p.Alpha()
	if c.cfg.Rig != nil {
		f.Pose = c.cfg.Rig.Update(dt.Seconds(), f.Alpha)
		f.Camera = c.cfg.Rig.Camera()
	}
	return f
}

func (c *Controller) readInput(dt time.Duration) {
	in := c.cfg.Input
	pressed := func(name string) bool { return in.Pressed(name) }
	if pressed(ActToggle) {
		c.TogglePlay()
	}
	if pressed(ActStepBack) {
		c.Step(-1)
	}
	if pressed(ActStepFwd) {
		c.Step(1)
	}
	if pressed(ActSpeedDown) {
		c.StepSpeed(-1)
	}
	if pressed(ActSpeedUp) {
		c.StepSpeed(1)
	}
	if pressed(ActCamNext) {
		c.NextCamera(1)
	}
	if pressed(ActCamPrev) {
		c.NextCamera(-1)
	}
	if pressed(ActCamMode) {
		c.CycleMode()
	}
	if pressed(ActMarkerNext) {
		c.JumpMarker(1)
	}
	if pressed(ActMarkerPrev) {
		c.JumpMarker(-1)
	}
	if pressed(ActRestart) {
		c.SeekTick(c.cfg.Player.Start())
	}
	if v := in.Value(ActScrub); v != 0 {
		c.scrubAcc += v * c.cfg.ScrubRate * c.tickRate() * dt.Seconds()
		if n := int(c.scrubAcc); n != 0 {
			c.scrubAcc -= float64(n)
			c.SeekTick(clampTick(int64(c.cfg.Player.Tick())+int64(n), c.cfg.Player))
		}
	} else {
		c.scrubAcc = 0
	}
	r := c.cfg.Rig
	if r == nil {
		return
	}
	s := dt.Seconds()
	r.Look(in.Value(ActLookX)+in.Value(ActPadLookX)*c.cfg.LookRate*s, in.Value(ActLookY)*-1+in.Value(ActPadLookY)*-1*c.cfg.LookRate*s)
	if z := in.Value(ActZoom); z != 0 {
		r.Zoom(-z)
	}
	r.Move(in.Value(ActMoveZ), in.Value(ActMoveX), in.Value(ActMoveY))
}

func clampTick(t int64, p *replay.Player) uint64 {
	if t < int64(p.Start()) {
		return p.Start()
	}
	if t > int64(p.End()) {
		return p.End()
	}
	return uint64(t)
}

// TogglePlay pauses or resumes. Resuming at the end restarts from the start.
func (c *Controller) TogglePlay() {
	p := c.cfg.Player
	if p.Paused() {
		if p.AtEnd() {
			_ = p.Seek(p.Start())
		}
		p.Resume()
	} else {
		p.Pause()
	}
	c.changed()
}

// Step moves n ticks (negative goes back) and pauses.
func (c *Controller) Step(n int) {
	p := c.cfg.Player
	p.Pause()
	if n < 0 {
		_ = p.StepBack(-n)
	} else {
		p.StepForward(n)
	}
	c.changed()
}

// SeekTick jumps to a tick, clamped to the recording. Playback state is kept.
func (c *Controller) SeekTick(t uint64) {
	p := c.cfg.Player
	if t < p.Start() {
		t = p.Start()
	}
	if t > p.End() {
		t = p.End()
	}
	_ = p.Seek(t)
	c.changed()
}

// SetSpeed sets the playback speed, snapped to the nearest of replay.Speeds.
func (c *Controller) SetSpeed(s float64) {
	best := replay.Speeds[0]
	for _, v := range replay.Speeds {
		if math.Abs(v-s) < math.Abs(best-s) {
			best = v
		}
	}
	c.cfg.Player.SetSpeed(best)
	c.changed()
}

// StepSpeed moves to the next slower (-1) or faster (+1) speed.
func (c *Controller) StepSpeed(delta int) {
	cur := c.cfg.Player.Speed()
	idx := 0
	for i, v := range replay.Speeds {
		if math.Abs(v-cur) < math.Abs(replay.Speeds[idx]-cur) {
			idx = i
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(replay.Speeds) {
		idx = len(replay.Speeds) - 1
	}
	c.cfg.Player.SetSpeed(replay.Speeds[idx])
	c.changed()
}

// SetCamera selects a camera by option id: CamChase, "shot:<name>", CamOrbit
// or CamFly. It reports whether the id exists.
func (c *Controller) SetCamera(id string) bool {
	r := c.cfg.Rig
	if r == nil {
		return false
	}
	switch {
	case id == CamChase:
		r.SetMode(replay.Broadcast)
		r.ShowShot("")
	case id == CamOrbit:
		r.SetMode(replay.Orbit)
	case id == CamFly:
		r.SetMode(replay.Fly)
	case len(id) > 5 && id[:5] == "shot:":
		if !r.ShowShot(id[5:]) {
			return false
		}
	default:
		return false
	}
	c.changed()
	return true
}

// ActiveCamera is the id of the camera in use.
func (c *Controller) ActiveCamera() string {
	r := c.cfg.Rig
	if r == nil {
		return ""
	}
	switch r.Mode() {
	case replay.Orbit:
		return CamOrbit
	case replay.Fly:
		return CamFly
	}
	if r.Shot() != "" {
		return "shot:" + r.Shot()
	}
	return CamChase
}

// NextCamera cycles the camera options by delta (1 or -1).
func (c *Controller) NextCamera(delta int) {
	n := len(c.camOrder)
	if n == 0 {
		return
	}
	cur := 0
	for i, id := range c.camOrder {
		if id == c.ActiveCamera() {
			cur = i
		}
	}
	c.SetCamera(c.camOrder[((cur+delta)%n+n)%n])
}

// CycleMode switches between broadcast, orbit and fly.
func (c *Controller) CycleMode() {
	r := c.cfg.Rig
	if r == nil {
		return
	}
	switch r.Mode() {
	case replay.Broadcast:
		c.SetCamera(CamOrbit)
	case replay.Orbit:
		c.SetCamera(CamFly)
	default:
		c.SetCamera(CamChase)
	}
}

// JumpMarker seeks to the next (+1) or previous (-1) marker, landing
// MarkerLead seconds before it. It reports whether there was one.
func (c *Controller) JumpMarker(dir int) bool {
	p := c.cfg.Player
	lead := uint64(c.cfg.MarkerLead * c.tickRate())
	cur := p.Tick()
	land := func(m replay.Marker) uint64 {
		if m.Tick < p.Start()+lead {
			return p.Start()
		}
		return m.Tick - lead
	}
	ms := p.Recording().Markers()
	var target uint64
	found := false
	if dir > 0 {
		for _, m := range ms {
			if t := land(m); t > cur && (!found || t < target) {
				target, found = t, true
			}
		}
	} else {
		for _, m := range ms {
			if t := land(m); t < cur && (!found || t > target) {
				target, found = t, true
			}
		}
	}
	if found {
		c.SeekTick(target)
	}
	return found
}

// Handle runs a kit "action" event from the Bar. name is the data-kit-action
// ("replay.toggle", "replay.step_back", "replay.step_forward", "replay.speed",
// "replay.camera", "replay.marker", "replay.restart") and value is its
// data-kit-value. It reports whether the name was a replay action.
func (c *Controller) Handle(name, value string) bool {
	switch name {
	case "replay.toggle":
		c.TogglePlay()
	case "replay.step_back":
		c.Step(-1)
	case "replay.step_forward":
		c.Step(1)
	case "replay.restart":
		c.SeekTick(c.cfg.Player.Start())
	case "replay.speed":
		if s, err := strconv.ParseFloat(value, 64); err == nil {
			c.SetSpeed(s)
		}
	case "replay.camera":
		c.SetCamera(value)
	case "replay.marker":
		if t, err := strconv.ParseUint(value, 10, 64); err == nil {
			c.SeekTick(t)
		}
	default:
		return false
	}
	return true
}

// HandleSetting runs a kit "setting" event: the scrubber, key "replay.scrub",
// whose value is a tick. It reports whether the key was the scrubber.
func (c *Controller) HandleSetting(key, value string) bool {
	if key != "replay.scrub" {
		return false
	}
	if t, err := strconv.ParseUint(value, 10, 64); err == nil {
		c.SeekTick(t)
	}
	return true
}
