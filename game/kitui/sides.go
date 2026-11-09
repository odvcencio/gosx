package kitui

import (
	"fmt"
	"strconv"
	"strings"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/host"
)

// Slot is a column of the side-select screen.
type Slot int8

const (
	// SlotA is the left side.
	SlotA Slot = -1
	// SlotMiddle is the unassigned column. A device here spectates and does
	// not hold up the start.
	SlotMiddle Slot = 0
	// SlotB is the right side.
	SlotB Slot = 1
)

// SeatKind says where a seat's input comes from.
type SeatKind uint8

const (
	SeatKeyboard SeatKind = iota + 1
	SeatGamepad
	// SeatRemote is a player on another machine. The game supplies it with
	// SideSelect.SetRemote; it cannot be moved from this screen.
	SeatRemote
)

// Seat is one device (or one remote player) on the screen.
type Seat struct {
	ID    string
	Name  string
	Kind  SeatKind
	Slot  Slot
	Prev  Slot // where the icon slid from, for the slide animation
	Ready bool
	// Focus is the device's cursor: -1 on the icon row, or the index of the
	// settings field it is editing. Local seats only.
	Focus int
	// Host marks the seat that owns the settings in an online lobby.
	Host bool
}

// Local reports whether the seat is driven from this machine.
func (s Seat) Local() bool { return s.Kind != SeatRemote }

// Field is one choice in the settings row, such as quarter length or difficulty.
// The game supplies the fields; the screen only cycles through the options.
type Field struct {
	ID      string
	Label   string
	Options []Option
	Index   int
}

// Value returns the selected option's value.
func (f Field) Value() string {
	if f.Index < 0 || f.Index >= len(f.Options) {
		return ""
	}
	return f.Options[f.Index].Value
}

// Cmd is a navigation command read from a device.
type Cmd uint8

const (
	CmdLeft Cmd = iota + 1
	CmdRight
	CmdUp
	CmdDown
	CmdConfirm
	CmdCancel
)

// ChangeKind names what changed, for networking.
type ChangeKind uint8

const (
	ChangeSeat ChangeKind = iota + 1
	ChangeField
	ChangeConnect
	ChangeDisconnect
)

// Change reports one local change so an online game can send it.
type Change struct {
	Kind  ChangeKind
	Seat  Seat   // for seat changes
	Field string // for field changes
	Value string
}

// Result is what the game starts the match with.
type Result struct {
	// Sides lists the seat IDs on side A and side B in the order they joined.
	Sides [2][]string
	// Spectators lists the seats left in the middle.
	Spectators []string
	// Fields maps field ID to the chosen option value.
	Fields map[string]string
}

// SideConfig configures a SideSelect.
type SideConfig struct {
	// SideNames label the two sides. Defaults: "Side A", "Side B".
	SideNames [2]string
	// Fields is the settings row. Empty hides it.
	Fields []Field
	// MaxPerSide limits seats on one side. Zero means no limit.
	MaxPerSide int
	// AllowEmptySide lets a match start with nobody on one side (a game with
	// a computer opponent). By default both sides need a ready seat.
	AllowEmptySide bool
	// WrapFields makes Left/Right on a field wrap around the options.
	WrapFields bool
	// Router, if set, gets each local device assigned to a player index as
	// seats move: the first seat on side A gets PlayerSlots[0][0], and so on.
	// Seats in the middle are unassigned.
	Router      *actions.Router
	PlayerSlots [2][]int
	// OnChange receives every local change (and connect/disconnect).
	OnChange func(Change)
	// OnStart runs when the screen becomes startable. It runs again if a
	// change cancels the start and the screen becomes startable again.
	OnStart func(Result)
	// OnBack runs when a device in the middle presses Cancel.
	OnBack func()
	// OnRender runs after any change that should redraw the screen.
	OnRender func()
}

// SideSelect is the model behind the side-select screen. Feed it input
// events (it implements actions.Sink), or call Command for a device. It is
// not safe for concurrent use.
type SideSelect struct {
	cfg      SideConfig
	seats    []*Seat
	fields   []Field
	readOnly bool
	started  bool
	held     map[string]bool
	status   string
}

// NewSideSelect creates the model.
func NewSideSelect(cfg SideConfig) *SideSelect {
	if cfg.SideNames[0] == "" {
		cfg.SideNames[0] = "Side A"
	}
	if cfg.SideNames[1] == "" {
		cfg.SideNames[1] = "Side B"
	}
	s := &SideSelect{cfg: cfg, held: map[string]bool{}}
	s.fields = append([]Field(nil), cfg.Fields...)
	return s
}

// Seats returns a copy of the seats in join order (local seats first, then remote).
func (s *SideSelect) Seats() []Seat {
	out := make([]Seat, len(s.seats))
	for i, st := range s.seats {
		out[i] = *st
	}
	return out
}

// Seat returns one seat by ID.
func (s *SideSelect) Seat(id string) (Seat, bool) {
	if st := s.find(id); st != nil {
		return *st, true
	}
	return Seat{}, false
}

// Fields returns a copy of the settings fields with their current choices.
func (s *SideSelect) Fields() []Field { return append([]Field(nil), s.fields...) }

// Status returns a short sentence describing the last change, for a live region.
func (s *SideSelect) Status() string { return s.status }

// SideNames returns the two side labels.
func (s *SideSelect) SideNames() [2]string { return s.cfg.SideNames }

// FieldsReadOnly reports whether settings are locked (a guest in an online lobby).
func (s *SideSelect) FieldsReadOnly() bool { return s.readOnly }

// SetFieldsReadOnly locks or unlocks the settings row for local devices.
func (s *SideSelect) SetFieldsReadOnly(v bool) { s.readOnly = v; s.render() }

func (s *SideSelect) find(id string) *Seat {
	for _, st := range s.seats {
		if st.ID == id {
			return st
		}
	}
	return nil
}

func (s *SideSelect) render() {
	if s.cfg.OnRender != nil {
		s.cfg.OnRender()
	}
}

func (s *SideSelect) changed(c Change) {
	if s.cfg.OnChange != nil {
		s.cfg.OnChange(c)
	}
}

// ConnectKeyboard adds the keyboard seat.
func (s *SideSelect) ConnectKeyboard() { s.connect(actions.DeviceKeyboard, "Keyboard", SeatKeyboard) }

// Connect adds a local device seat with a display name. Connecting a device
// twice does nothing. Seats start unready in the middle.
func (s *SideSelect) Connect(id, name string, kind SeatKind) { s.connect(id, name, kind) }

func (s *SideSelect) connect(id, name string, kind SeatKind) {
	if id == "" || s.find(id) != nil {
		return
	}
	seat := &Seat{ID: id, Name: name, Kind: kind, Focus: -1}
	// Local seats sit before remote ones.
	at := len(s.seats)
	for i, st := range s.seats {
		if !st.Local() {
			at = i
			break
		}
	}
	s.seats = append(s.seats, nil)
	copy(s.seats[at+1:], s.seats[at:])
	s.seats[at] = seat
	s.status = name + " joined"
	s.changed(Change{Kind: ChangeConnect, Seat: *seat})
	s.afterChange()
}

// Disconnect removes a local device seat, for example an unplugged gamepad.
func (s *SideSelect) Disconnect(id string) {
	for i, st := range s.seats {
		if st.ID == id && st.Local() {
			s.seats = append(s.seats[:i], s.seats[i+1:]...)
			for k := range s.held {
				if strings.HasPrefix(k, id+"|") {
					delete(s.held, k)
				}
			}
			s.status = st.Name + " left"
			s.changed(Change{Kind: ChangeDisconnect, Seat: *st})
			s.afterChange()
			return
		}
	}
}

// SyncPads makes the gamepad seats match the connected gamepads: new pads join,
// unplugged pads leave. Call it each frame or on a timer.
func (s *SideSelect) SyncPads(source host.GamepadSource) {
	if source == nil {
		return
	}
	present := map[string]bool{}
	for _, pad := range source.Gamepads() {
		if pad.Mapping != "standard" {
			continue
		}
		id := actions.PadDevice(pad.Index)
		present[id] = true
		s.connect(id, "Gamepad "+strconv.Itoa(pad.Index+1), SeatGamepad)
	}
	for _, st := range append([]*Seat(nil), s.seats...) {
		if st.Kind == SeatGamepad && !present[st.ID] {
			s.Disconnect(st.ID)
		}
	}
}

// SetRemote replaces the remote seats with seats supplied by the game, for an
// online lobby. Kind is forced to SeatRemote. Moving a remote seat shows the
// slide animation.
func (s *SideSelect) SetRemote(remote []Seat) {
	old := map[string]Slot{}
	var local []*Seat
	for _, st := range s.seats {
		if st.Local() {
			local = append(local, st)
		} else {
			old[st.ID] = st.Slot
		}
	}
	s.seats = local
	for _, r := range remote {
		r.Kind = SeatRemote
		r.Focus = -1
		r.Prev = r.Slot
		if prev, ok := old[r.ID]; ok {
			r.Prev = prev
		}
		cp := r
		s.seats = append(s.seats, &cp)
	}
	s.afterChange()
}

// SetField sets a field from outside, for example the host's choice arriving
// over the network. An unknown ID or value is ignored.
func (s *SideSelect) SetField(id, value string) bool {
	for i := range s.fields {
		if s.fields[i].ID != id {
			continue
		}
		for j, o := range s.fields[i].Options {
			if o.Value == value {
				s.fields[i].Index = j
				s.afterChange()
				return true
			}
		}
	}
	return false
}

// Set puts a local or remote seat in a slot with the given ready flag (ready is
// ignored in the middle). A game uses it to apply a correction from the
// server. It reports false for an unknown seat or a full side.
func (s *SideSelect) Set(id string, slot Slot, ready bool) bool {
	st := s.find(id)
	if st == nil || slot < SlotA || slot > SlotB {
		return false
	}
	if slot != st.Slot && !s.hasRoom(slot, st) {
		return false
	}
	if slot != st.Slot {
		st.Prev = st.Slot
	}
	st.Slot = slot
	st.Ready = ready && slot != SlotMiddle
	s.afterChange()
	return true
}

func (s *SideSelect) hasRoom(slot Slot, except *Seat) bool {
	if slot == SlotMiddle || s.cfg.MaxPerSide <= 0 {
		return true
	}
	n := 0
	for _, st := range s.seats {
		if st != except && st.Slot == slot {
			n++
		}
	}
	return n < s.cfg.MaxPerSide
}

// Command applies a navigation command for a local device.
func (s *SideSelect) Command(id string, c Cmd) {
	st := s.find(id)
	if st == nil || !st.Local() {
		return
	}
	if st.Focus >= 0 && st.Focus < len(s.fields) {
		s.fieldCommand(st, c)
		return
	}
	switch c {
	case CmdLeft, CmdRight:
		next := st.Slot + 1
		if c == CmdLeft {
			next = st.Slot - 1
		}
		if next < SlotA || next > SlotB {
			return
		}
		if !s.hasRoom(next, st) {
			s.status = s.sideName(next) + " is full"
			s.render()
			return
		}
		st.Prev, st.Slot, st.Ready = st.Slot, next, false
		s.status = st.Name + " moved to " + s.sideName(next)
		s.seatChanged(st)
	case CmdConfirm:
		if st.Slot == SlotMiddle {
			return
		}
		st.Ready = !st.Ready
		if st.Ready {
			s.status = st.Name + " is ready"
		} else {
			s.status = st.Name + " is not ready"
		}
		s.seatChanged(st)
	case CmdCancel:
		switch {
		case st.Ready:
			st.Ready = false
			s.status = st.Name + " is not ready"
			s.seatChanged(st)
		case st.Slot != SlotMiddle:
			st.Prev, st.Slot = st.Slot, SlotMiddle
			s.status = st.Name + " moved to the middle"
			s.seatChanged(st)
		default:
			if s.cfg.OnBack != nil {
				s.cfg.OnBack()
			}
		}
	case CmdDown:
		if len(s.fields) > 0 {
			st.Focus = 0
			s.status = s.fields[0].Label
			s.render()
		}
	}
}

func (s *SideSelect) fieldCommand(st *Seat, c Cmd) {
	f := &s.fields[st.Focus]
	switch c {
	case CmdUp:
		st.Focus--
		if st.Focus < 0 {
			s.status = st.Name
		} else {
			s.status = s.fields[st.Focus].Label
		}
		s.render()
	case CmdDown:
		if st.Focus+1 < len(s.fields) {
			st.Focus++
			s.status = s.fields[st.Focus].Label
			s.render()
		}
	case CmdCancel:
		st.Focus = -1
		s.render()
	case CmdLeft, CmdRight:
		if s.readOnly {
			s.status = "Only the host can change " + f.Label
			s.render()
			return
		}
		step := 1
		if c == CmdLeft {
			step = -1
		}
		s.StepField(f.ID, step)
	}
}

// StepField moves a field's choice by step (+1 or -1) and reports the change.
// It does nothing while the settings are read-only.
func (s *SideSelect) StepField(id string, step int) bool {
	if s.readOnly {
		return false
	}
	for i := range s.fields {
		f := &s.fields[i]
		if f.ID != id || len(f.Options) == 0 {
			continue
		}
		n := f.Index + step
		if n < 0 || n >= len(f.Options) {
			if !s.cfg.WrapFields {
				return false
			}
			n = (n + len(f.Options)) % len(f.Options)
		}
		f.Index = n
		s.status = f.Label + ": " + f.Options[n].Label
		s.changed(Change{Kind: ChangeField, Field: f.ID, Value: f.Options[n].Value})
		s.afterChange()
		return true
	}
	return false
}

// HandleAction handles the data-kit-action buttons the panel renders for the
// mouse: "sides-field" with value "<id>:<+1|-1>". It reports whether the
// action was one of its own.
func (s *SideSelect) HandleAction(name, value string) bool {
	if name != "sides-field" {
		return false
	}
	id, step, ok := strings.Cut(value, ":")
	if !ok {
		return true
	}
	if n, err := strconv.Atoi(step); err == nil {
		s.StepField(id, n)
	}
	return true
}

func (s *SideSelect) sideName(sl Slot) string {
	switch sl {
	case SlotA:
		return s.cfg.SideNames[0]
	case SlotB:
		return s.cfg.SideNames[1]
	}
	return "the middle"
}

func (s *SideSelect) seatChanged(st *Seat) {
	s.changed(Change{Kind: ChangeSeat, Seat: *st})
	s.afterChange()
}

func (s *SideSelect) afterChange() {
	s.applyRouter()
	s.checkStart()
	s.render()
}

// Startable reports whether every seat on a side is ready and the side
// requirements are met.
func (s *SideSelect) Startable() bool {
	var count [2]int
	for _, st := range s.seats {
		switch st.Slot {
		case SlotA:
			count[0]++
		case SlotB:
			count[1]++
		default:
			continue
		}
		if !st.Ready {
			return false
		}
	}
	if s.cfg.AllowEmptySide {
		return count[0]+count[1] > 0
	}
	return count[0] > 0 && count[1] > 0
}

func (s *SideSelect) checkStart() {
	ok := s.Startable()
	if ok && !s.started {
		s.started = true
		if s.cfg.OnStart != nil {
			s.cfg.OnStart(s.Result())
		}
	} else if !ok {
		s.started = false
	}
}

// Result returns the current assignment.
func (s *SideSelect) Result() Result {
	r := Result{Fields: map[string]string{}}
	for _, st := range s.seats {
		switch st.Slot {
		case SlotA:
			r.Sides[0] = append(r.Sides[0], st.ID)
		case SlotB:
			r.Sides[1] = append(r.Sides[1], st.ID)
		default:
			r.Spectators = append(r.Spectators, st.ID)
		}
	}
	for _, f := range s.fields {
		r.Fields[f.ID] = f.Value()
	}
	return r
}

func (s *SideSelect) applyRouter() {
	r := s.cfg.Router
	if r == nil {
		return
	}
	var next [2]int
	for _, st := range s.seats {
		if !st.Local() {
			continue
		}
		r.Unassign(st.ID, -1)
		if st.Slot == SlotMiddle {
			continue
		}
		side := 0
		if st.Slot == SlotB {
			side = 1
		}
		if next[side] < len(s.cfg.PlayerSlots[side]) {
			_ = r.Assign(st.ID, s.cfg.PlayerSlots[side][next[side]])
			next[side]++
		}
	}
}

// ReleaseAll clears held-input memory. It implements actions.Sink.
func (s *SideSelect) ReleaseAll() {
	for k := range s.held {
		delete(s.held, k)
	}
}

var keyCmds = map[string]Cmd{
	"ArrowLeft": CmdLeft, "KeyA": CmdLeft, "ArrowRight": CmdRight, "KeyD": CmdRight,
	"ArrowUp": CmdUp, "KeyW": CmdUp, "ArrowDown": CmdDown, "KeyS": CmdDown,
	"Enter": CmdConfirm, "NumpadEnter": CmdConfirm, "Space": CmdConfirm,
	"Escape": CmdCancel, "Backspace": CmdCancel,
}

var padCmds = map[string]Cmd{
	"button14": CmdLeft, "button15": CmdRight, "button12": CmdUp, "button13": CmdDown,
	"button0": CmdConfirm, "button9": CmdConfirm, "button1": CmdCancel,
}

// Feed turns a raw input event into a command for its device. It implements
// actions.Sink, so it can take the place of an actions.Map behind
// actions.Attach and actions.PadPoller. A key or button press from an unknown
// device adds that device. Left stick axes act as the d-pad, with a dead
// zone and hysteresis so one push is one command.
func (s *SideSelect) Feed(ev actions.Event) {
	src := ev.Source
	device := ev.Device
	var cmd Cmd
	var kind SeatKind
	name := ""
	switch src.Kind {
	case actions.KindKey:
		if device == "" {
			device = actions.DeviceKeyboard
		}
		cmd, kind, name = keyCmds[src.Code], SeatKeyboard, "Keyboard"
	case actions.KindPadButton:
		if device == "" {
			device = actions.PadDevice(0)
		}
		cmd, kind = padCmds[src.Code], SeatGamepad
	case actions.KindPadAxis:
		if device == "" {
			device = actions.PadDevice(0)
		}
		kind = SeatGamepad
		key := device + "|" + src.Code
		switch src.Code {
		case "axis0", "axis1":
			neg, pos := CmdLeft, CmdRight
			if src.Code == "axis1" {
				neg, pos = CmdUp, CmdDown
			}
			for dir, c := range map[string]Cmd{"-": neg, "+": pos} {
				v := ev.Value
				if dir == "-" {
					v = -v
				}
				k := key + dir
				if v >= 0.6 && !s.held[k] {
					s.held[k] = true
					s.dispatch(device, kind, name, c)
				} else if v < 0.3 {
					delete(s.held, k)
				}
			}
		}
		return
	default:
		return
	}
	if cmd == 0 {
		return
	}
	k := device + "|" + src.String()
	if ev.Value < 0.5 {
		delete(s.held, k)
		return
	}
	if s.held[k] {
		return
	}
	s.held[k] = true
	s.dispatch(device, kind, name, cmd)
}

func (s *SideSelect) dispatch(device string, kind SeatKind, name string, c Cmd) {
	if s.find(device) == nil {
		if name == "" {
			name = "Gamepad"
			if i := strings.IndexByte(device, ':'); i >= 0 {
				if n, err := strconv.Atoi(device[i+1:]); err == nil {
					name = fmt.Sprintf("Gamepad %d", n+1)
				}
			}
		}
		s.connect(device, name, kind)
		return // the press that wakes a device only makes it appear
	}
	s.Command(device, c)
}

var _ actions.Sink = (*SideSelect)(nil)
