package actions

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"m31labs.dev/gosx/game/host"
	"m31labs.dev/gosx/game/storage"
)

const frame = 16 * time.Millisecond

func gameplay() Context {
	return Context{
		Name:  "gameplay",
		Group: "play",
		Actions: []ActionDef{
			{Name: "jump", Bindings: []Binding{{Source: Key("Space")}, {Source: PadButton(0)}}},
			{Name: "dash", DoubleTap: 300 * time.Millisecond, Bindings: []Binding{{Source: Key("ShiftLeft")}}},
			{Name: "charge", Hold: 400 * time.Millisecond, Bindings: []Binding{{Source: MouseButton(0)}}},
			{Name: "moveX", Kind: Axis, Deadzone: 0.2, Bindings: []Binding{{Source: Key("KeyD")}, {Source: Key("KeyA"), Scale: -1}, {Source: PadAxis(0)}}},
			{Name: "moveY", Kind: Axis, Bindings: []Binding{{Source: Key("KeyW")}, {Source: Key("KeyS"), Scale: -1}}},
			{Name: "pause", Bindings: []Binding{{Source: Key("Escape")}}},
		},
	}
}

func menu() Context {
	return Context{
		Name: "menu", Opaque: true,
		Actions: []ActionDef{{Name: "back", Bindings: []Binding{{Source: Key("Escape")}}},
			{Name: "confirm", Bindings: []Binding{{Source: Key("Enter")}}}},
	}
}

func newMap() *Map {
	m := NewMap()
	m.Define(gameplay())
	m.Define(menu())
	m.Push("gameplay")
	return m
}

func press(m *Map, s Source)   { m.Feed(Event{Source: s, Value: 1}) }
func release(m *Map, s Source) { m.Feed(Event{Source: s}) }

func TestPressHoldRelease(t *testing.T) {
	m := newMap()
	press(m, Key("Space"))
	m.Update(frame)
	if !m.Pressed("jump") || !m.Down("jump") {
		t.Fatalf("frame 1: %+v", m.State("jump"))
	}
	m.Update(frame)
	if m.Pressed("jump") || !m.Down("jump") {
		t.Fatalf("frame 2: %+v", m.State("jump"))
	}
	release(m, Key("Space"))
	m.Update(frame)
	if !m.Released("jump") || m.Down("jump") {
		t.Fatalf("frame 3: %+v", m.State("jump"))
	}
}

func TestTapLatchBetweenFrames(t *testing.T) {
	m := newMap()
	press(m, Key("Space"))
	release(m, Key("Space")) // both before the next Update
	m.Update(frame)
	if !m.Pressed("jump") || !m.Down("jump") {
		t.Fatalf("latched tap lost: %+v", m.State("jump"))
	}
	m.Update(frame)
	st := m.State("jump")
	if !st.Released || st.Down || !st.Tapped {
		t.Fatalf("expected release+tap next frame: %+v", st)
	}
	m.Update(frame)
	if m.State("jump") != (State{}) {
		t.Fatalf("state should settle: %+v", m.State("jump"))
	}
}

func TestLatchDoesNotApplyToAxisSticks(t *testing.T) {
	m := newMap()
	m.Feed(Event{Source: PadAxis(0), Value: 1})
	m.Feed(Event{Source: PadAxis(0), Value: 0})
	m.Update(frame)
	if m.Value("moveX") != 0 {
		t.Fatalf("stick flick should not latch: %v", m.Value("moveX"))
	}
}

func TestAxisSumVecAndDeadzone(t *testing.T) {
	m := newMap()
	press(m, Key("KeyD"))
	press(m, Key("KeyW"))
	m.Update(frame)
	x, y := m.Vec2("moveX", "moveY")
	if x < 0.70 || x > 0.71 || y < 0.70 || y > 0.71 {
		t.Fatalf("vec not normalized: %v %v", x, y)
	}
	release(m, Key("KeyD"))
	press(m, Key("KeyA"))
	m.Update(frame)
	if m.Value("moveX") != -1 {
		t.Fatalf("A: %v", m.Value("moveX"))
	}
	release(m, Key("KeyA"))
	m.Feed(Event{Source: PadAxis(0), Value: 0.1})
	m.Update(frame)
	if m.Value("moveX") != 0 {
		t.Fatalf("deadzone: %v", m.Value("moveX"))
	}
	m.Feed(Event{Source: PadAxis(0), Value: 0.6})
	m.Update(frame)
	if v := m.Value("moveX"); v < 0.49 || v > 0.51 {
		t.Fatalf("rescaled deadzone: %v", v)
	}
}

func TestHoldTapDoubleTap(t *testing.T) {
	m := newMap()
	press(m, MouseButton(0))
	m.Update(frame)
	started := false
	for i := 0; i < 40; i++ {
		m.Update(frame)
		if m.State("charge").HoldStarted {
			if started {
				t.Fatal("HoldStarted fired twice")
			}
			started = true
			if !m.Holding("charge") {
				t.Fatal("Holding must be on at HoldStarted")
			}
		}
	}
	if !started || !m.Holding("charge") {
		t.Fatalf("hold never started: %+v", m.State("charge"))
	}
	release(m, MouseButton(0))
	m.Update(frame)
	if st := m.State("charge"); st.Tapped || st.Holding || !st.Released {
		t.Fatalf("long press is not a tap: %+v", st)
	}

	// double tap: two quick taps
	press(m, Key("ShiftLeft"))
	m.Update(frame)
	release(m, Key("ShiftLeft"))
	m.Update(frame)
	if !m.Tapped("dash") {
		t.Fatal("first tap")
	}
	m.Update(frame)
	press(m, Key("ShiftLeft"))
	m.Update(frame)
	if !m.DoubleTapped("dash") {
		t.Fatalf("second tap should double tap: %+v", m.State("dash"))
	}
	release(m, Key("ShiftLeft"))
	m.Update(frame)
	// too slow: third tap after the window is not a double
	for i := 0; i < 30; i++ {
		m.Update(frame)
	}
	press(m, Key("ShiftLeft"))
	m.Update(frame)
	if m.DoubleTapped("dash") {
		t.Fatal("slow press must not double tap")
	}
}

func TestContextStackAndSuppression(t *testing.T) {
	m := newMap()
	press(m, Key("Escape"))
	m.Update(frame)
	if !m.Pressed("pause") {
		t.Fatal("pause not pressed")
	}
	m.Push("menu")
	m.Update(frame)
	// Escape is still held; it must not press "back", and gameplay is hidden.
	if m.Pressed("back") || m.Down("pause") {
		t.Fatalf("held key leaked: back=%+v pause=%+v", m.State("back"), m.State("pause"))
	}
	if !m.contexts["gameplay"].index["pause"].state.Released {
		t.Fatal("hidden action should report one release")
	}
	release(m, Key("Escape"))
	m.Update(frame)
	press(m, Key("Escape"))
	m.Update(frame)
	if !m.Pressed("back") {
		t.Fatal("fresh press should reach the menu")
	}
	if got := m.Stack(); !reflect.DeepEqual(got, []string{"gameplay", "menu"}) || m.Top() != "menu" {
		t.Fatal(got)
	}
	release(m, Key("Escape"))
	m.Update(frame)
	if m.Pop() != "menu" {
		t.Fatal("pop")
	}
	m.Update(frame)
	if m.Down("back") {
		t.Fatal("menu action visible after pop")
	}
}

func TestMotionAccumulatesPerFrame(t *testing.T) {
	m := NewMap()
	m.Define(Context{Name: "g", Actions: []ActionDef{{Name: "lookX", Kind: Axis, Bindings: []Binding{{Source: Motion(CodeMouseDX), Scale: 0.01}}}}})
	m.Push("g")
	m.Feed(Event{Source: Motion(CodeMouseDX), Value: 30})
	m.Feed(Event{Source: Motion(CodeMouseDX), Value: 20})
	m.Update(frame)
	if v := m.Value("lookX"); v < 0.49 || v > 0.51 {
		t.Fatalf("lookX = %v", v)
	}
	m.Update(frame)
	if m.Value("lookX") != 0 {
		t.Fatal("motion must not persist")
	}
}

func TestReleaseAll(t *testing.T) {
	m := newMap()
	press(m, Key("Space"))
	m.Update(frame)
	m.ReleaseAll()
	m.Update(frame)
	if m.Down("jump") || !m.Released("jump") {
		t.Fatalf("%+v", m.State("jump"))
	}
}

func TestRebindConflicts(t *testing.T) {
	m := newMap()
	// Space -> dash conflicts with jump.
	conf, err := m.Rebind("gameplay", "dash", 0, Key("Space"), ConflictReject)
	if !errors.Is(err, ErrConflict) || len(conf) != 1 || conf[0].Action != "jump" {
		t.Fatalf("reject: %v %v", conf, err)
	}
	if m.Bindings("gameplay", "dash")[0].Source != Key("ShiftLeft") {
		t.Fatal("reject must not change anything")
	}
	if _, err := m.Rebind("gameplay", "dash", 0, Key("Space"), ConflictSwap); err != nil {
		t.Fatal(err)
	}
	if m.Bindings("gameplay", "jump")[0].Source != Key("ShiftLeft") || m.Bindings("gameplay", "dash")[0].Source != Key("Space") {
		t.Fatalf("swap: %v %v", m.Bindings("gameplay", "jump"), m.Bindings("gameplay", "dash"))
	}
	if _, err := m.Rebind("gameplay", "jump", 0, Key("Space"), ConflictUnbind); err != nil {
		t.Fatal(err)
	}
	if !m.Bindings("gameplay", "dash")[0].Source.Zero() {
		t.Fatal("unbind should clear dash")
	}
	// A key cannot take over a gamepad slot.
	if _, err := m.Rebind("gameplay", "jump", 1, Key("KeyZ"), ConflictReject); !errors.Is(err, ErrWrongClass) {
		t.Fatalf("class: %v", err)
	}
	if _, err := m.Rebind("gameplay", "nope", 0, Key("KeyZ"), ConflictReject); !errors.Is(err, ErrUnknownAction) {
		t.Fatal(err)
	}
	if _, err := m.Rebind("gameplay", "jump", 9, Key("KeyZ"), ConflictReject); !errors.Is(err, ErrBadSlot) {
		t.Fatal(err)
	}
	// Same source on the same action's own slot is not a conflict.
	if c := m.Conflicts("gameplay", "jump", 0, Key("Space")); len(c) != 0 {
		t.Fatalf("self conflict: %v", c)
	}
	// Opposite directions of one axis do not conflict.
	m2 := NewMap()
	m2.Define(Context{Name: "g", Actions: []ActionDef{
		{Name: "l", Kind: Axis, Bindings: []Binding{{Source: PadAxis(0), Scale: -1}}},
		{Name: "r", Kind: Axis, Bindings: []Binding{{Source: PadAxis(0), Scale: 1}}}}})
	if c := m2.Conflicts("g", "l", 0, PadAxis(0)); len(c) != 0 {
		t.Fatalf("axis directions: %v", c)
	}
}

func TestRebindAppliesToLiveInput(t *testing.T) {
	m := newMap()
	if _, err := m.Rebind("gameplay", "jump", 0, Key("KeyJ"), ConflictReject); err != nil {
		t.Fatal(err)
	}
	press(m, Key("Space"))
	m.Update(frame)
	if m.Down("jump") {
		t.Fatal("old key still bound")
	}
	press(m, Key("KeyJ"))
	m.Update(frame)
	if !m.Down("jump") {
		t.Fatal("new key not bound")
	}
}

func TestCapture(t *testing.T) {
	m := newMap()
	m.Capture()
	if !m.Capturing() {
		t.Fatal("not capturing")
	}
	press(m, Key("KeyQ"))
	src, ok := m.TakeCaptured()
	if !ok || src != Key("KeyQ") || m.Capturing() {
		t.Fatalf("capture: %v %v", src, ok)
	}
	if _, ok := m.TakeCaptured(); ok {
		t.Fatal("capture taken twice")
	}
	release(m, Key("KeyQ"))
	m.Capture()
	press(m, Key("Escape"))
	if m.Capturing() {
		t.Fatal("Escape should cancel")
	}
	if _, ok := m.TakeCaptured(); ok {
		t.Fatal("cancelled capture returned a source")
	}
	m.Update(frame)
	if m.Pressed("pause") {
		t.Fatal("cancelling key must not trigger the action")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	m := newMap()
	if _, err := m.Rebind("gameplay", "jump", 0, Key("KeyJ"), ConflictReject); err != nil {
		t.Fatal(err)
	}
	if err := m.Unbind("gameplay", "moveY", 1); err != nil {
		t.Fatal(err)
	}
	store := storage.New("test", storage.NewMemoryBackend())
	if err := m.Save(store, "bindings"); err != nil {
		t.Fatal(err)
	}
	m2 := newMap()
	ok, err := m2.Load(store, "bindings")
	if err != nil || !ok {
		t.Fatalf("load: %v %v", ok, err)
	}
	for _, a := range m.Actions("gameplay") {
		if !reflect.DeepEqual(m.Bindings("gameplay", a), m2.Bindings("gameplay", a)) {
			t.Errorf("%s differs: %v vs %v", a, m.Bindings("gameplay", a), m2.Bindings("gameplay", a))
		}
	}
	if !reflect.DeepEqual(m.Export(), m2.Export()) {
		t.Fatal("export not stable")
	}
	// Only changed actions are stored.
	if n := len(m.Export().Contexts["gameplay"]); n != 2 {
		t.Fatalf("exported %d actions, want 2", n)
	}
	m2.ResetAll()
	if len(m2.Export().Contexts) != 0 {
		t.Fatal("reset left overrides")
	}
	// Missing key.
	if ok, err := newMap().Load(store, "missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
	// Corrupt data and unknown actions.
	_ = store.Set("bad", "{nope")
	if _, err := newMap().Load(store, "bad"); err == nil {
		t.Fatal("corrupt data should error")
	}
	n, err := newMap().Import(SavedBindings{Version: 1, Contexts: map[string]map[string][]SavedBinding{
		"gameplay": {"gone": {{Source: "key:KeyA"}}, "jump": {{Source: "key:KeyK"}}}, // wrong slot count
		"old":      {"x": {{Source: "key:KeyA"}}},
	}})
	if n != 0 || err != nil {
		t.Fatalf("import skip: %d %v", n, err)
	}
	if _, err := newMap().Import(SavedBindings{Version: 99}); err == nil {
		t.Fatal("newer version should error")
	}
}

func TestSourceStringRoundTrip(t *testing.T) {
	for _, s := range []Source{Key("KeyW"), MouseButton(2), Motion(CodeWheelY), PadButton(3), PadAxis(1), {}} {
		got, err := ParseSource(s.String())
		if err != nil || got != s {
			t.Errorf("%v -> %q -> %v %v", s, s.String(), got, err)
		}
	}
	for _, bad := range []string{"x", "key:", "bogus:Foo"} {
		if _, err := ParseSource(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestLabels(t *testing.T) {
	cases := []struct {
		s     Source
		style PadStyle
		want  string
	}{
		{Key("KeyW"), 0, "W"}, {Key("Digit3"), 0, "3"}, {Key("ArrowUp"), 0, "Up"}, {Key("Space"), 0, "Space"},
		{Key("ShiftLeft"), 0, "Left Shift"}, {Key("Numpad7"), 0, "Numpad 7"}, {Key("F5"), 0, "F5"},
		{MouseButton(0), 0, "Left Click"}, {MouseButton(2), 0, "Right Click"},
		{PadButton(0), PadXbox, "A"}, {PadButton(0), PadPlayStation, "Cross"}, {PadButton(0), PadGeneric, "Button 0"},
		{PadButton(9), PadXbox, "Menu"}, {PadButton(40), PadXbox, "Button 40"}, {PadAxis(2), 0, "Right Stick X"},
		{Source{}, 0, "Unbound"},
	}
	for _, c := range cases {
		if got := Label(c.s, c.style); got != c.want {
			t.Errorf("Label(%v,%d) = %q, want %q", c.s, c.style, got, c.want)
		}
	}
	if got := BindingLabel(Binding{Source: PadAxis(0), Scale: -1}, 0); got != "Left Stick Left" {
		t.Error(got)
	}
	if got := BindingLabel(Binding{Source: PadAxis(1)}, 0); got != "Left Stick Down" {
		t.Error(got)
	}
}

func TestRouterLocalMultiplayer(t *testing.T) {
	p1, p2 := NewMap(), NewMap()
	for i, m := range []*Map{p1, p2} {
		key := []string{"KeyW", "ArrowUp"}[i]
		m.Define(Context{Name: "g", Actions: []ActionDef{{Name: "up", Bindings: []Binding{{Source: Key(key)}, {Source: PadButton(12)}}}}})
		m.Push("g")
	}
	r := NewRouter(p1, p2)
	var unassigned []string
	r.OnUnassigned = func(e Event) { unassigned = append(unassigned, e.Device) }
	r.Feed(Event{Device: PadDevice(0), Source: PadButton(12), Value: 1})
	if len(unassigned) != 1 || unassigned[0] != "pad:0" {
		t.Fatalf("unassigned: %v", unassigned)
	}
	if p, ok := r.Join(DeviceKeyboard); !ok || p != 0 {
		t.Fatal("keyboard joins p1", p, ok)
	}
	if p, ok := r.Join(PadDevice(0)); !ok || p != 0 {
		t.Fatal("pad joins first player without a pad", p, ok)
	}
	if p, ok := r.Join(PadDevice(1)); !ok || p != 1 {
		t.Fatal("second pad joins p2", p, ok)
	}
	if _, ok := r.Join(PadDevice(2)); ok {
		t.Fatal("no free player")
	}
	if err := r.Assign(DeviceKeyboard, 1); err != nil { // share the keyboard
		t.Fatal(err)
	}
	if r.Assign("x", 5) != ErrBadPlayer {
		t.Fatal("bad player")
	}
	r.Feed(Event{Device: DeviceKeyboard, Source: Key("ArrowUp"), Value: 1})
	r.Feed(Event{Device: PadDevice(0), Source: PadButton(12), Value: 1})
	r.Update(frame)
	if !p2.Down("up") || !p1.Down("up") {
		t.Fatalf("p1=%v p2=%v", p1.State("up"), p2.State("up"))
	}
	r.Feed(Event{Device: PadDevice(0), Source: PadButton(12)})
	r.Feed(Event{Device: DeviceKeyboard, Source: Key("ArrowUp")})
	r.Update(frame)
	r.Unassign(DeviceKeyboard, -1)
	if len(r.PlayersFor(DeviceKeyboard)) != 0 {
		t.Fatal("unassign")
	}
	if len(r.Devices(1)) != 1 {
		t.Fatalf("devices: %v", r.Devices(1))
	}
	r.Feed(Event{Source: Key("KeyW"), Value: 1}) // broadcast
	r.Update(frame)
	if !p1.Down("up") {
		t.Fatal("broadcast")
	}
	r.ReleaseAll()
}

type fakePads struct{ pads []host.GamepadState }

func (f *fakePads) Gamepads() []host.GamepadState { return f.pads }

func TestPadPollerFeedsAndReleases(t *testing.T) {
	m := NewMap()
	m.Define(Context{Name: "g", Actions: []ActionDef{
		{Name: "jump", Bindings: []Binding{{Source: PadButton(0)}}},
		{Name: "moveX", Kind: Axis, Bindings: []Binding{{Source: PadAxis(0)}}}}})
	m.Push("g")
	src := &fakePads{pads: []host.GamepadState{{Index: 0, ID: "pad", Mapping: "standard", Buttons: []float64{1}, Axes: []float64{0.05, 0.05, 0, 0}}}}
	p := NewPadPoller()
	p.Poll(src, m)
	m.Update(frame)
	if !m.Down("jump") || m.Value("moveX") != 0 {
		t.Fatalf("jump=%v moveX=%v", m.State("jump"), m.Value("moveX"))
	}
	src.pads[0].Axes = []float64{1, 0, 0, 0}
	p.Poll(src, m)
	m.Update(frame)
	if m.Value("moveX") != 1 {
		t.Fatalf("moveX %v", m.Value("moveX"))
	}
	src.pads = nil // unplug while held
	p.Poll(src, m)
	m.Update(frame)
	if m.Down("jump") || m.Value("moveX") != 0 {
		t.Fatal("unplugged pad stays held")
	}
	// Non-standard pads are skipped by default.
	src.pads = []host.GamepadState{{Index: 1, ID: "odd", Buttons: []float64{1}}}
	p.Poll(src, m)
	m.Update(frame)
	if m.Down("jump") {
		t.Fatal("non-standard pad accepted")
	}
}

func TestDeterministic(t *testing.T) {
	run := func() []State {
		m := newMap()
		var out []State
		for i := 0; i < 60; i++ {
			switch i {
			case 3:
				press(m, Key("Space"))
			case 4:
				release(m, Key("Space"))
			case 10:
				press(m, MouseButton(0))
			case 50:
				release(m, MouseButton(0))
			}
			m.Update(frame)
			out = append(out, m.State("jump"), m.State("charge"))
		}
		return out
	}
	if !reflect.DeepEqual(run(), run()) {
		t.Fatal("two identical runs differ")
	}
}

func TestUpdateAndQueriesDoNotAllocate(t *testing.T) {
	m := newMap()
	press(m, Key("KeyW"))
	m.Update(frame)
	src := &fakePads{pads: []host.GamepadState{{Index: 0, ID: "p", Mapping: "standard", Buttons: make([]float64, 16), Axes: make([]float64, 4)}}}
	p := NewPadPoller()
	p.Poll(src, m)
	n := testing.AllocsPerRun(200, func() {
		m.Feed(Event{Source: Key("KeyW"), Value: 1})
		m.Feed(Event{Source: Motion(CodeMouseDX), Value: 1})
		m.Update(frame)
		_ = m.Down("jump")
		_ = m.Pressed("pause")
		_, _ = m.Vec2("moveX", "moveY")
		_ = m.State("charge")
		p.Poll(src, m)
	})
	if n != 0 {
		t.Fatalf("allocs per frame = %v", n)
	}
}
