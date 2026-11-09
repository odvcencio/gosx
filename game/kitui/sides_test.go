package kitui

import (
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/host"
)

func key(m *SideSelect, code string) {
	m.Feed(actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key(code), Value: 1})
	m.Feed(actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key(code)})
}

func padBtn(m *SideSelect, pad, n int) {
	d := actions.PadDevice(pad)
	m.Feed(actions.Event{Device: d, Source: actions.PadButton(n), Value: 1})
	m.Feed(actions.Event{Device: d, Source: actions.PadButton(n)})
}

func slotOf(t *testing.T, m *SideSelect, id string) Slot {
	t.Helper()
	s, ok := m.Seat(id)
	if !ok {
		t.Fatalf("no seat %q", id)
	}
	return s.Slot
}

func TestSlideAndReady(t *testing.T) {
	var started []Result
	m := NewSideSelect(SideConfig{OnStart: func(r Result) { started = append(started, r) }})
	m.ConnectKeyboard()
	m.Connect("pad:0", "Gamepad 1", SeatGamepad)
	if slotOf(t, m, "keyboard") != SlotMiddle {
		t.Fatal("new seats start in the middle")
	}
	key(m, "ArrowLeft")
	padBtn(m, 0, 15) // d-pad right
	if slotOf(t, m, "keyboard") != SlotA || slotOf(t, m, "pad:0") != SlotB {
		t.Fatalf("%+v", m.Seats())
	}
	key(m, "ArrowLeft") // already at the edge
	if s, _ := m.Seat("keyboard"); s.Slot != SlotA {
		t.Fatal("clamp")
	}
	if s, _ := m.Seat("keyboard"); s.Prev != SlotMiddle {
		t.Fatalf("Prev=%v", s.Prev)
	}
	key(m, "Enter")
	if m.Startable() || len(started) != 0 {
		t.Fatal("one ready seat is not enough")
	}
	padBtn(m, 0, 0) // A button
	if !m.Startable() || len(started) != 1 {
		t.Fatalf("startable=%v started=%d", m.Startable(), len(started))
	}
	r := started[0]
	if !reflect.DeepEqual(r.Sides, [2][]string{{"keyboard"}, {"pad:0"}}) {
		t.Fatalf("%+v", r)
	}
	// Cancel on a ready seat un-readies it and cancels the start.
	padBtn(m, 0, 1)
	if m.Startable() {
		t.Fatal("cancel should stop the start")
	}
	padBtn(m, 0, 0)
	if len(started) != 2 {
		t.Fatalf("a second start should fire: %d", len(started))
	}
	// Moving clears ready.
	key(m, "ArrowRight")
	if s, _ := m.Seat("keyboard"); s.Ready || s.Slot != SlotMiddle {
		t.Fatalf("%+v", s)
	}
}

func TestSpectatorsAndConfirmInMiddle(t *testing.T) {
	m := NewSideSelect(SideConfig{})
	m.ConnectKeyboard()
	m.Connect("pad:0", "Gamepad 1", SeatGamepad)
	m.Connect("pad:1", "Gamepad 2", SeatGamepad)
	key(m, "Enter")
	if s, _ := m.Seat("keyboard"); s.Ready {
		t.Fatal("middle seats cannot ready")
	}
	padBtn(m, 0, 14)
	padBtn(m, 1, 15)
	padBtn(m, 0, 0)
	padBtn(m, 1, 0)
	if !m.Startable() {
		t.Fatal("a spectator must not hold up the start")
	}
	if got := m.Result().Spectators; !reflect.DeepEqual(got, []string{"keyboard"}) {
		t.Fatal(got)
	}
}

func TestEmptySideRules(t *testing.T) {
	m := NewSideSelect(SideConfig{})
	m.ConnectKeyboard()
	key(m, "ArrowLeft")
	key(m, "Enter")
	if m.Startable() {
		t.Fatal("both sides are required by default")
	}
	vs := NewSideSelect(SideConfig{AllowEmptySide: true})
	vs.ConnectKeyboard()
	if vs.Startable() {
		t.Fatal("nobody assigned")
	}
	key(vs, "ArrowLeft")
	key(vs, "Enter")
	if !vs.Startable() {
		t.Fatal("one side alone should start with AllowEmptySide")
	}
}

func TestMaxPerSide(t *testing.T) {
	m := NewSideSelect(SideConfig{MaxPerSide: 1})
	m.ConnectKeyboard()
	m.Connect("pad:0", "Gamepad 1", SeatGamepad)
	key(m, "ArrowLeft")
	padBtn(m, 0, 14)
	if slotOf(t, m, "pad:0") != SlotMiddle || !strings.Contains(m.Status(), "full") {
		t.Fatalf("side A holds one seat: %v %q", slotOf(t, m, "pad:0"), m.Status())
	}
	if m.Set("pad:0", SlotA, false) {
		t.Fatal("Set must respect the limit")
	}
}

func TestStickAndFirstPressJoins(t *testing.T) {
	m := NewSideSelect(SideConfig{})
	ax := func(v float64) { m.Feed(actions.Event{Device: "pad:1", Source: actions.PadAxis(0), Value: v}) }
	ax(0.9) // wakes the device, shows its icon
	if s, ok := m.Seat("pad:1"); !ok || s.Name != "Gamepad 2" || s.Slot != SlotMiddle {
		t.Fatalf("%+v %v", s, ok)
	}
	ax(0)
	ax(-0.9)
	if slotOf(t, m, "pad:1") != SlotA {
		t.Fatal("stick left")
	}
	ax(0)
	ax(0.9) // back to the middle... (A -> middle)
	if slotOf(t, m, "pad:1") != SlotMiddle {
		t.Fatal("stick right")
	}
	ax(0.95) // held: no repeat
	if slotOf(t, m, "pad:1") != SlotMiddle {
		t.Fatal("a held stick must not repeat")
	}
	ax(0.5) // between the thresholds: still held
	ax(0.9)
	if slotOf(t, m, "pad:1") != SlotMiddle {
		t.Fatal("hysteresis: a stick that has not returned to center must not fire again")
	}
	ax(0.1)
	ax(0.9)
	if slotOf(t, m, "pad:1") != SlotB {
		t.Fatal("stick right after release")
	}
	// Mouse and unknown keys do nothing.
	m.Feed(actions.Event{Source: actions.MouseButton(0), Value: 1})
	m.Feed(actions.Event{Device: "keyboard", Source: actions.Key("KeyQ"), Value: 1})
	if len(m.Seats()) != 1 {
		t.Fatal("unmapped input created a seat")
	}
}

type fakePads struct{ pads []host.GamepadState }

func (f *fakePads) Gamepads() []host.GamepadState { return f.pads }

func TestSyncPads(t *testing.T) {
	m := NewSideSelect(SideConfig{})
	m.ConnectKeyboard()
	src := &fakePads{pads: []host.GamepadState{{Index: 0, Mapping: "standard"}, {Index: 2, Mapping: "standard"}, {Index: 1, Mapping: ""}}}
	m.SyncPads(src)
	ids := []string{}
	for _, s := range m.Seats() {
		ids = append(ids, s.ID)
	}
	if !reflect.DeepEqual(ids, []string{"keyboard", "pad:0", "pad:2"}) {
		t.Fatal(ids)
	}
	padBtn(m, 2, 15)
	src.pads = src.pads[:1] // pad 2 unplugged
	m.SyncPads(src)
	if _, ok := m.Seat("pad:2"); ok || len(m.Seats()) != 2 {
		t.Fatalf("%+v", m.Seats())
	}
}

func TestRouterAssignment(t *testing.T) {
	p := []*actions.Map{actions.NewMap(), actions.NewMap(), actions.NewMap(), actions.NewMap()}
	r := actions.NewRouter(p...)
	m := NewSideSelect(SideConfig{Router: r, PlayerSlots: [2][]int{{0, 2}, {1, 3}}})
	m.ConnectKeyboard()
	m.Connect("pad:0", "Gamepad 1", SeatGamepad)
	m.Connect("pad:1", "Gamepad 2", SeatGamepad)
	key(m, "ArrowLeft")
	padBtn(m, 0, 14)
	padBtn(m, 1, 15)
	want := map[string][]int{"keyboard": {0}, "pad:0": {2}, "pad:1": {1}}
	for dev, w := range want {
		if got := r.PlayersFor(dev); !reflect.DeepEqual(got, w) {
			t.Errorf("%s -> %v, want %v", dev, got, w)
		}
	}
	// Back to the middle unassigns; a freed slot is reused.
	key(m, "ArrowRight")
	if len(r.PlayersFor("keyboard")) != 0 {
		t.Fatal("middle seats are unassigned")
	}
	padBtn(m, 1, 14) // pad:1 B -> middle
	padBtn(m, 1, 14) // middle -> A
	// Slots go to seats in seat order: pad:0 holds the first, pad:1 gets the second.
	if got := r.PlayersFor("pad:1"); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("pad:1 on A: %v", got)
	}
	// A third seat on a two-slot side stays unassigned.
	key(m, "ArrowLeft")
	// Three seats on a two-slot side: the last in seat order stays unassigned.
	if got := r.PlayersFor("pad:1"); len(got) != 0 {
		t.Fatalf("no free slot: %v", got)
	}
	if got := r.PlayersFor("keyboard"); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("keyboard: %v", got)
	}
}

func TestFieldsRow(t *testing.T) {
	var changes []Change
	m := NewSideSelect(SideConfig{
		Fields: []Field{
			{ID: "len", Label: "Quarter", Options: []Option{{"2", "2 min"}, {"5", "5 min"}, {"10", "10 min"}}, Index: 1},
			{ID: "diff", Label: "Difficulty", Options: []Option{{"easy", "Easy"}, {"hard", "Hard"}}},
		},
		OnChange: func(c Change) { changes = append(changes, c) },
	})
	m.ConnectKeyboard()
	m.Connect("pad:0", "Gamepad 1", SeatGamepad)
	key(m, "ArrowDown") // into the settings row
	if s, _ := m.Seat("keyboard"); s.Focus != 0 {
		t.Fatalf("focus %d", s.Focus)
	}
	key(m, "ArrowRight")
	key(m, "ArrowRight") // end of options: clamp
	if m.Fields()[0].Value() != "10" {
		t.Fatalf("%+v", m.Fields()[0])
	}
	key(m, "ArrowDown")
	key(m, "ArrowRight")
	key(m, "ArrowDown") // past the last field
	if m.Fields()[1].Value() != "hard" {
		t.Fatal("second field")
	}
	key(m, "ArrowUp")
	key(m, "ArrowUp")
	if s, _ := m.Seat("keyboard"); s.Focus != -1 {
		t.Fatalf("Up from the first field returns to the icons, focus=%d", s.Focus)
	}
	// While the keyboard is on the settings row, its Left/Right do not move its icon.
	key(m, "ArrowDown")
	key(m, "ArrowLeft")
	if slotOf(t, m, "keyboard") != SlotMiddle || m.Fields()[0].Value() != "5" {
		t.Fatal("Left edits the field and must not move the icon")
	}
	key(m, "Escape") // leave the row
	if s, _ := m.Seat("keyboard"); s.Focus != -1 {
		t.Fatal("Cancel leaves the row")
	}
	// A gamepad uses the same row.
	padBtn(m, 0, 13)
	padBtn(m, 0, 14)
	if m.Fields()[0].Value() != "2" {
		t.Fatalf("pad edits: %+v", m.Fields()[0])
	}
	var fieldChanges int
	for _, c := range changes {
		if c.Kind == ChangeField {
			fieldChanges++
		}
	}
	if fieldChanges != 4 {
		t.Fatalf("field changes %d: %+v", fieldChanges, changes)
	}
	if got := m.Result().Fields; got["len"] != "2" || got["diff"] != "hard" {
		t.Fatal(got)
	}
	// Wrap.
	w := NewSideSelect(SideConfig{WrapFields: true, Fields: []Field{{ID: "f", Label: "F", Options: []Option{{"a", "A"}, {"b", "B"}}}}})
	w.ConnectKeyboard()
	key(w, "ArrowDown")
	key(w, "ArrowLeft")
	if w.Fields()[0].Value() != "b" {
		t.Fatal("wrap")
	}
}

func TestOnlineSeatsAndReadOnlyFields(t *testing.T) {
	var started int
	var changes []Change
	m := NewSideSelect(SideConfig{
		OnStart:  func(Result) { started++ },
		OnChange: func(c Change) { changes = append(changes, c) },
		Fields:   []Field{{ID: "len", Label: "Quarter", Options: []Option{{"2", "2"}, {"5", "5"}}}},
	})
	m.ConnectKeyboard()
	m.SetFieldsReadOnly(true)
	m.SetRemote([]Seat{{ID: "r1", Name: "Ana", Slot: SlotB, Ready: true, Host: true}, {ID: "r2", Name: "Bo", Slot: SlotMiddle}})
	key(m, "ArrowLeft")
	key(m, "Enter")
	if started != 1 {
		t.Fatalf("local A ready + remote B ready should start, started=%d", started)
	}
	if s, _ := m.Seat("r1"); s.Kind != SeatRemote || !s.Host {
		t.Fatalf("%+v", s)
	}
	// Remote seats cannot be driven from here.
	m.Command("r1", CmdLeft)
	if slotOf(t, m, "r1") != SlotB {
		t.Fatal("remote seat moved by a local command")
	}
	// Guests cannot change settings; the host's value arrives with SetField.
	key(m, "Escape")
	key(m, "ArrowDown")
	key(m, "ArrowRight")
	if m.Fields()[0].Value() != "2" {
		t.Fatal("read-only field changed")
	}
	if !m.SetField("len", "5") || m.Fields()[0].Value() != "5" || m.SetField("len", "99") || m.SetField("zzz", "5") {
		t.Fatal("SetField")
	}
	// A remote update slides the icon (Prev is kept) and can cancel the start.
	m.SetRemote([]Seat{{ID: "r1", Name: "Ana", Slot: SlotMiddle}, {ID: "r2", Name: "Bo"}})
	if s, _ := m.Seat("r1"); s.Slot != SlotMiddle || s.Prev != SlotB {
		t.Fatalf("%+v", s)
	}
	// Local changes are reported for the network.
	var seatChanges int
	for _, c := range changes {
		if c.Kind == ChangeSeat {
			seatChanges++
		}
	}
	if seatChanges < 2 {
		t.Fatalf("changes %+v", changes)
	}
	// Local seats stay before remote ones.
	if ids := seatIDs(m); ids[0] != "keyboard" {
		t.Fatal(ids)
	}
	m.Connect("pad:0", "Gamepad 1", SeatGamepad)
	if ids := seatIDs(m); !reflect.DeepEqual(ids[:2], []string{"keyboard", "pad:0"}) {
		t.Fatal(ids)
	}
}

func seatIDs(m *SideSelect) []string {
	var ids []string
	for _, s := range m.Seats() {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestBackFromMiddle(t *testing.T) {
	backs := 0
	m := NewSideSelect(SideConfig{OnBack: func() { backs++ }})
	m.ConnectKeyboard()
	key(m, "Escape")
	if backs != 1 {
		t.Fatal("Cancel in the middle should go back")
	}
	key(m, "ArrowLeft")
	key(m, "Escape")
	if backs != 1 || slotOf(t, m, "keyboard") != SlotMiddle {
		t.Fatal("Cancel on a side steps to the middle first")
	}
}

func TestSideSelectPanelMarkup(t *testing.T) {
	m := NewSideSelect(SideConfig{
		SideNames: [2]string{"Home", "Away"},
		Fields:    []Field{{ID: "len", Label: "Quarter", Options: []Option{{"2", "2 min"}, {"5", "5 min"}}, Index: 1}},
	})
	m.ConnectKeyboard()
	m.Connect("pad:0", "Gamepad 1", SeatGamepad)
	m.SetRemote([]Seat{{ID: "r1", Name: "Ana", Slot: SlotB, Ready: true, Host: true}})
	key(m, "ArrowLeft")
	key(m, "Enter")
	html := gosx.RenderHTML(SideSelectPanel(SideSelectProps{Model: m}))
	has(t, html,
		`aria-label="Home"`, `aria-label="Away"`, `data-side="0"`, `data-side="2"`,
		`data-device="keyboard"`, `data-kind="keyboard"`, `data-device="pad:0"`, `data-kind="gamepad"`, `data-kind="remote"`,
		`data-slot="0"`, `data-from-slot="1"`, `data-ready="true"`, `--from:1`, `--slot:0`,
		`aria-label="Keyboard, Home, Ready"`, `aria-label="Gamepad 1, the middle, Spectate"`, `>Host<`,
		`data-field="len"`, `5 min`, `data-kit-action="sides-field"`, `data-kit-value="len:+1"`, `data-kit-value="len:-1"`,
		`role="status"`, `Keyboard is ready`, `aria-live="polite"`, `<svg`)
	if strings.Contains(html, "disabled") {
		t.Error("editable settings must not be disabled")
	}
	m.SetFieldsReadOnly(true)
	ro := gosx.RenderHTML(SideSelectPanel(SideSelectProps{Model: m, Title: "Teams", Labels: map[string]string{"host": "Anfuehrer"}}))
	has(t, ro, "disabled", `data-readonly="true"`, ">Teams<", "Anfuehrer")
	bare := NewSideSelect(SideConfig{})
	if strings.Contains(gosx.RenderHTML(SideSelectPanel(SideSelectProps{Model: bare})), "kit-sides-settings") {
		t.Error("no fields, no settings row")
	}
	css := CSS()
	for _, v := range []string{"kit-slide", "--kit-side-a", ".kit-device", "prefers-reduced-motion"} {
		if !strings.Contains(css, v) {
			t.Errorf("css %s", v)
		}
	}
}

func TestHandleAction(t *testing.T) {
	m := NewSideSelect(SideConfig{Fields: []Field{{ID: "len", Label: "Quarter", Options: []Option{{"2", "2"}, {"5", "5"}, {"9", "9"}}}}})
	if m.HandleAction("resume", "") {
		t.Fatal("not ours")
	}
	if !m.HandleAction("sides-field", "len:+1") || m.Fields()[0].Value() != "5" {
		t.Fatal("mouse step")
	}
	m.HandleAction("sides-field", "len:-1")
	m.HandleAction("sides-field", "len:-1") // at the start: clamp
	m.HandleAction("sides-field", "garbage")
	if m.Fields()[0].Value() != "2" {
		t.Fatal(m.Fields()[0])
	}
}

func TestReleaseAllRearmsKeys(t *testing.T) {
	m := NewSideSelect(SideConfig{})
	m.ConnectKeyboard()
	press := func() { m.Feed(actions.Event{Device: "keyboard", Source: actions.Key("ArrowLeft"), Value: 1}) }
	press()
	press() // held: ignored
	if slotOf(t, m, "keyboard") != SlotA {
		t.Fatal("first press")
	}
	m.ReleaseAll() // window blur: the key-up may never arrive
	m.Feed(actions.Event{Device: "keyboard", Source: actions.Key("ArrowRight"), Value: 1})
	m.Feed(actions.Event{Device: "keyboard", Source: actions.Key("ArrowRight")})
	m.ReleaseAll()
	press()
	if slotOf(t, m, "keyboard") != SlotA {
		t.Fatal("input stuck after ReleaseAll")
	}
}
