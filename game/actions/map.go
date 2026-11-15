package actions

import (
	"math"
	"time"
)

// ActionKind says whether an action is read as a button or an analog axis.
type ActionKind uint8

const (
	// Button actions are on or off. Down is true while the scaled value of
	// any binding reaches Threshold.
	Button ActionKind = iota
	// Axis actions carry a value in [-1,1], the clamped sum of their bindings.
	Axis
)

// Default timing and threshold values, used when an ActionDef field is zero.
const (
	DefaultThreshold = 0.5
	DefaultTap       = 200 * time.Millisecond
)

// ActionDef defines one action inside a Context.
type ActionDef struct {
	Name     string
	Kind     ActionKind
	Bindings []Binding
	// Deadzone zeroes analog values whose magnitude is below it, and
	// rescales the rest to fill [0,1]. Applies to gamepad axes and triggers.
	Deadzone float64
	// Threshold is the scaled value at which the action counts as down.
	// Zero selects DefaultThreshold.
	Threshold float64
	// Tap is the longest press that counts as a tap. Zero selects DefaultTap.
	Tap time.Duration
	// Hold is how long a press must last before State.HoldStarted fires and
	// State.Holding turns on. Zero disables hold.
	Hold time.Duration
	// DoubleTap is the longest gap between the starts of two taps that
	// counts as a double tap. Zero disables double tap.
	DoubleTap time.Duration
}

// Context is a named set of actions that can be pushed on the stack.
type Context struct {
	Name string
	// Opaque hides every context below this one while it is active, as a
	// modal menu does.
	Opaque bool
	// Group scopes conflict detection: contexts with the same Group conflict
	// with each other. Empty means the context only conflicts with itself.
	Group   string
	Actions []ActionDef
}

// State is one action's state for the current frame.
type State struct {
	Down     bool
	Pressed  bool
	Released bool
	// Value is the action value: 0/1 for buttons unless a gamepad trigger
	// reports an analog level, and [-1,1] for axes.
	Value float64
	// Tapped is true on the frame a press shorter than the tap limit ends.
	Tapped bool
	// DoubleTapped is true on the frame of the second press of a double tap.
	DoubleTapped bool
	// HoldStarted is true on the frame a press reaches the hold time.
	HoldStarted bool
	// Holding is true from HoldStarted until release.
	Holding bool
	// HeldFor is how long the current press has lasted.
	HeldFor time.Duration
}

type actionDef struct {
	ActionDef
	defaults []Binding
	state    State
	pressAt  time.Duration
	holdDone bool
	lastTap  time.Duration
	haveTap  bool
}

type context struct {
	Context
	defs  []*actionDef
	index map[string]*actionDef
}

type srcState struct {
	value      float64
	latch      float64
	suppressed bool
}

// Map turns raw events into action state. Use one Map per player. It is not
// safe for concurrent use: feed events and call Update from one goroutine.
type Map struct {
	contexts map[string]*context
	order    []string // definition order, for stable iteration
	stack    []*context
	active   []*context // top first, cut at the first Opaque
	src      map[Source]*srcState
	now      time.Duration

	pendDX, pendDY, pendWX, pendWY float64
	dx, dy, wx, wy                 float64

	capturing bool
	captured  Source
	haveCap   bool
	deadzone  float64
}

// NewMap creates an empty Map.
func NewMap() *Map {
	return &Map{
		contexts: map[string]*context{},
		src:      map[Source]*srcState{},
	}
}

// Define adds or replaces a context. It copies the definition. The current
// bindings of a replaced context are lost, so call Import afterwards to
// reapply saved overrides. Defining a context that is on the stack keeps it there.
func (m *Map) Define(c Context) {
	ctx := &context{Context: c, index: map[string]*actionDef{}}
	ctx.Actions = nil
	for _, a := range c.Actions {
		a.Name = normalize(a.Name)
		if a.Name == "" || ctx.index[a.Name] != nil {
			continue
		}
		a.Bindings = append([]Binding(nil), a.Bindings...)
		for i := range a.Bindings {
			a.Bindings[i].Scale = a.Bindings[i].scale()
		}
		d := &actionDef{ActionDef: a, defaults: append([]Binding(nil), a.Bindings...)}
		ctx.defs = append(ctx.defs, d)
		ctx.index[a.Name] = d
		ctx.Actions = append(ctx.Actions, a)
	}
	if _, ok := m.contexts[c.Name]; !ok {
		m.order = append(m.order, c.Name)
	}
	old := m.contexts[c.Name]
	m.contexts[c.Name] = ctx
	for i, s := range m.stack {
		if s == old {
			m.stack[i] = ctx
		}
	}
	m.recompute()
}

// Push makes the named context the top of the stack. It reports false if
// the context is not defined. Pushing a context already on the stack moves it
// to the top.
func (m *Map) Push(name string) bool {
	ctx := m.contexts[name]
	if ctx == nil {
		return false
	}
	m.removeFromStack(ctx)
	m.stack = append(m.stack, ctx)
	m.recompute()
	return true
}

// Pop removes the top context and returns its name ("" when the stack is empty).
func (m *Map) Pop() string {
	if len(m.stack) == 0 {
		return ""
	}
	top := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	m.recompute()
	return top.Name
}

// Remove takes the named context off the stack wherever it is.
func (m *Map) Remove(name string) {
	if ctx := m.contexts[name]; ctx != nil {
		m.removeFromStack(ctx)
		m.recompute()
	}
}

// SetStack replaces the stack with the named contexts, bottom first.
// Unknown names are skipped.
func (m *Map) SetStack(names ...string) {
	m.stack = m.stack[:0]
	for _, n := range names {
		if ctx := m.contexts[n]; ctx != nil {
			m.stack = append(m.stack, ctx)
		}
	}
	m.recompute()
}

// Stack returns the context names on the stack, bottom first.
func (m *Map) Stack() []string {
	out := make([]string, len(m.stack))
	for i, c := range m.stack {
		out[i] = c.Name
	}
	return out
}

// Top returns the name of the top context, or "".
func (m *Map) Top() string {
	if len(m.stack) == 0 {
		return ""
	}
	return m.stack[len(m.stack)-1].Name
}

func (m *Map) removeFromStack(ctx *context) {
	for i, s := range m.stack {
		if s == ctx {
			m.stack = append(m.stack[:i], m.stack[i+1:]...)
			return
		}
	}
}

// recompute rebuilds the active list and suppresses every input held right
// now. A key held when a menu opens must not also press the menu's buttons,
// and the key that closed a menu must not press the gameplay action under it.
func (m *Map) recompute() {
	m.active = m.active[:0]
	for i := len(m.stack) - 1; i >= 0; i-- {
		m.active = append(m.active, m.stack[i])
		if m.stack[i].Opaque {
			break
		}
	}
	for _, s := range m.src {
		if s.value != 0 || s.latch != 0 {
			s.suppressed = true
			s.latch = 0
		}
	}
}

func (m *Map) lookup(name string) *actionDef {
	for _, c := range m.active {
		if d := c.index[name]; d != nil {
			return d
		}
	}
	return nil
}

func normalize(name string) string {
	// Names are matched exactly except for surrounding space, which would be
	// a typo. Case is significant.
	start, end := 0, len(name)
	for start < end && name[start] == ' ' {
		start++
	}
	for end > start && name[end-1] == ' ' {
		end--
	}
	return name[start:end]
}

// Feed records one raw input change. Events for sources that no action
// uses are cheap. A zero Source is ignored.
func (m *Map) Feed(ev Event) {
	s := ev.Source
	if s.Zero() {
		return
	}
	if s.Kind == KindMouseMotion {
		switch s.Code {
		case CodeMouseDX:
			m.pendDX += ev.Value
		case CodeMouseDY:
			m.pendDY += ev.Value
		case CodeWheelX:
			m.pendWX += ev.Value
		case CodeWheelY:
			m.pendWY += ev.Value
		}
		return
	}
	st := m.src[s]
	if st == nil {
		st = &srcState{}
		m.src[s] = st
	}
	prev := st.value
	if m.capturing && !m.haveCap {
		if ev.Source.Kind == KindKey && s.Code == "Escape" && ev.Value >= DefaultThreshold && prev < DefaultThreshold {
			m.capturing = false
			st.value = ev.Value
			st.suppressed = true
			return
		}
		if math.Abs(ev.Value) >= DefaultThreshold && math.Abs(prev) < DefaultThreshold {
			m.captured, m.haveCap = s, true
			m.capturing = false
			st.value = ev.Value
			st.suppressed = true
			return
		}
	}
	st.value = ev.Value
	if ev.Value == 0 {
		st.suppressed = false
		return
	}
	if s.latches() && !st.suppressed && math.Abs(ev.Value) > math.Abs(st.latch) {
		st.latch = ev.Value
	}
}

// ReleaseAll zeroes every held input. Games call it on window blur so keys
// pressed before a focus change do not stay stuck.
func (m *Map) ReleaseAll() {
	for _, s := range m.src {
		s.value = 0
		s.suppressed = false
	}
	m.pendDX, m.pendDY, m.pendWX, m.pendWY = 0, 0, 0, 0
}

func (m *Map) read(b Binding, deadzone float64) float64 {
	var v float64
	switch b.Source.Kind {
	case KindMouseMotion:
		switch b.Source.Code {
		case CodeMouseDX:
			v = m.dx
		case CodeMouseDY:
			v = m.dy
		case CodeWheelX:
			v = m.wx
		case CodeWheelY:
			v = m.wy
		}
	default:
		st := m.src[b.Source]
		if st == nil || st.suppressed {
			return 0
		}
		v = st.value
		if st.latch != 0 && math.Abs(st.latch) > math.Abs(v) {
			v = st.latch // pressed and released between two frames
		}
		if deadzone > 0 && (b.Source.Kind == KindPadAxis || b.Source.Kind == KindPadButton) {
			v = applyDeadzone(v, deadzone)
		}
	}
	return v * b.scale()
}

func applyDeadzone(v, dz float64) float64 {
	if dz >= 1 {
		dz = 0.99
	}
	a := math.Abs(v)
	if a < dz {
		return 0
	}
	if a > 1 {
		a = 1
	}
	return math.Copysign((a-dz)/(1-dz), v)
}

// Update advances the clock by dt and recomputes every action state from the
// events fed since the last call. Call it once per frame, before game logic.
func (m *Map) Update(dt time.Duration) {
	if dt < 0 {
		dt = 0
	}
	m.now += dt
	m.dx, m.dy, m.wx, m.wy = m.pendDX, m.pendDY, m.pendWX, m.pendWY
	m.pendDX, m.pendDY, m.pendWX, m.pendWY = 0, 0, 0, 0
	for _, name := range m.order {
		c := m.contexts[name]
		on := false
		for _, a := range m.active {
			if a == c {
				on = true
				break
			}
		}
		for _, d := range c.defs {
			if on {
				m.updateDef(d)
			} else {
				d.deactivate()
			}
		}
	}
	for _, s := range m.src {
		s.latch = 0
	}
}

func (d *actionDef) deactivate() {
	was := d.state.Down
	d.state = State{Released: was}
	d.holdDone, d.haveTap = false, false
}

func (m *Map) updateDef(d *actionDef) {
	var sum float64
	for _, b := range d.Bindings {
		sum += m.read(b, d.Deadzone)
	}
	if sum > 1 {
		sum = 1
	} else if sum < -1 {
		sum = -1
	}
	th := d.Threshold
	if th <= 0 {
		th = DefaultThreshold
	}
	tap := d.Tap
	if tap <= 0 {
		tap = DefaultTap
	}
	down := math.Abs(sum) >= th
	prev := d.state
	st := State{Down: down, Value: sum, Holding: prev.Holding}
	if d.Kind == Button && down && sum < 0 {
		// A button treats a negative scaled value as its magnitude; Scale is
		// for axes. Keep Value non-negative so UI code can show a level.
		st.Value = -sum
	}
	st.Pressed = down && !prev.Down
	st.Released = !down && prev.Down
	switch {
	case st.Pressed:
		d.pressAt = m.now
		d.holdDone = false
		st.Holding = false
		if d.DoubleTap > 0 && d.haveTap && m.now-d.lastTap <= d.DoubleTap {
			st.DoubleTapped = true
			d.haveTap = false
		}
	case st.Released:
		dur := m.now - d.pressAt
		st.HeldFor = dur
		st.Holding = false
		if dur <= tap && !d.holdDone {
			st.Tapped = true
			d.lastTap, d.haveTap = d.pressAt, true
		} else {
			d.haveTap = false
		}
	case down:
		st.HeldFor = m.now - d.pressAt
	default:
		st.Holding = false
	}
	if down {
		st.HeldFor = m.now - d.pressAt
		if d.Hold > 0 && !d.holdDone && st.HeldFor >= d.Hold {
			d.holdDone = true
			st.HoldStarted = true
		}
		st.Holding = d.holdDone
	}
	d.state = st
}

// State returns the current frame's state for the named action, looking
// through the active contexts from the top. An action that no active context
// defines reads as the zero State.
func (m *Map) State(name string) State {
	if d := m.lookup(name); d != nil {
		return d.state
	}
	return State{}
}

// Down reports whether the action is held.
func (m *Map) Down(name string) bool { return m.State(name).Down }

// Pressed reports whether the action went down this frame.
func (m *Map) Pressed(name string) bool { return m.State(name).Pressed }

// Released reports whether the action went up this frame.
func (m *Map) Released(name string) bool { return m.State(name).Released }

// Value returns the action value.
func (m *Map) Value(name string) float64 { return m.State(name).Value }

// Tapped reports a short press that ended this frame.
func (m *Map) Tapped(name string) bool { return m.State(name).Tapped }

// DoubleTapped reports the second press of a double tap on this frame.
func (m *Map) DoubleTapped(name string) bool { return m.State(name).DoubleTapped }

// Holding reports that the press has lasted at least the action's Hold time.
func (m *Map) Holding(name string) bool { return m.State(name).Holding }

// Vec2 reads two axis actions as a vector and limits its length to 1, so
// diagonal movement is not faster than straight movement.
func (m *Map) Vec2(x, y string) (float64, float64) {
	vx, vy := m.Value(x), m.Value(y)
	if l := math.Hypot(vx, vy); l > 1 {
		vx, vy = vx/l, vy/l
	}
	return vx, vy
}

// Capture arms the Map to record the next press instead of acting on it.
// Pressing Escape cancels. A captured press does not trigger any action.
func (m *Map) Capture() { m.capturing, m.haveCap, m.captured = true, false, Source{} }

// Capturing reports whether Capture is armed and nothing was captured yet.
func (m *Map) Capturing() bool { return m.capturing && !m.haveCap }

// CancelCapture disarms Capture.
func (m *Map) CancelCapture() { m.capturing, m.haveCap = false, false }

// TakeCaptured returns the captured source once and clears it.
func (m *Map) TakeCaptured() (Source, bool) {
	if !m.haveCap {
		return Source{}, false
	}
	m.haveCap = false
	return m.captured, true
}

// Now returns the Map's clock: the sum of every dt passed to Update.
func (m *Map) Now() time.Duration { return m.now }
