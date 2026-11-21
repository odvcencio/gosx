// Package seats runs the seats of a local multiplayer game: each player
// claims a device by pressing a button on it, and from then on that
// device's input reaches only that player's actions.Map.
//
// The rules:
//
//  1. A fresh press of a claim control on a device no seat holds claims a
//     seat for it: by default Enter, Numpad Enter or Space on the keyboard
//     (with the mouse beside it) and button A (0) on a standard-mapping
//     gamepad. A held key, a browser's key repeat, a stick, a trigger and
//     the mouse claim nothing, and nothing claims while a modal is up.
//  2. The device goes to the seat Config.Prefer names when it is free,
//     else to the next free seat.
//  3. A gamepad is lost when its slot disappears from the poll, or shows
//     another id. Its seat waits for a device; while any seat waits only a
//     waiting seat can be claimed, the first one first, except that a pad
//     whose id matches one waiting seat's lost pad alone goes to that seat.
//     Two pads of the same model have the same id, so this is a hint, not
//     recognition. The keyboard is never lost.
//  4. Every change of who holds what (a claim, a loss, a swap, a modal
//     opening or closing, the window losing the focus, leaving) disarms
//     every seat: a seat's held input is withdrawn, and the seat arms again
//     only on a frame in which its device was at rest and sent nothing. A
//     button held through the change does nothing until it is let go and
//     pressed again. A disarmed seat's Map still reads presses (a claimed
//     Start can close the pause menu); a game reads gameplay from armed
//     seats only.
//
// Use it once per drawn frame, in this order: read the pads once, give the
// list to SyncPads (and to a hand tracker), feed the pad poller's events
// and the keyboard's through an actions.Tee that includes the Seats, then
// call Update. Seats reads no clock and no browser, so a native test or
// the pad test adapter drives it frame by frame.
package seats

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/actions/hand"
	"m31labs.dev/gosx/game/host"
)

// Device is the device a seat holds.
type Device struct {
	// Name is the device events arrive on: actions.DeviceKeyboard, or
	// actions.PadDevice(slot) for a gamepad.
	Name  string
	Class actions.Class
	// Pad is the gamepad's slot, -1 for the keyboard.
	Pad int
	// ID is what the browser's id says about the gamepad.
	ID actions.PadIdentity
}

// Keyboard reports whether the device is the keyboard (with the mouse).
func (d Device) Keyboard() bool { return d.Class == actions.ClassKeyboardMouse }

// Routes are the device names whose events reach the seat: the keyboard
// and the mouse together, or one gamepad.
func (d Device) Routes() []string {
	if d.Keyboard() {
		return []string{actions.DeviceKeyboard, actions.DeviceMouse}
	}
	return []string{d.Name}
}

// Hand is the device as a hand.Hand, for writing the seat's prompts.
func (d Device) Hand() hand.Hand {
	if d.Keyboard() {
		return hand.Hand{}
	}
	return hand.Hand{Device: hand.Pad, Slot: d.Pad, ID: d.ID}
}

// Config sets up a Seats.
type Config struct {
	// Seats is the number of seats. Zero selects 2.
	Seats int
	// NewMap returns a fresh Map for one seat, with its contexts defined
	// and pushed. Required.
	NewMap func() *actions.Map
	// Claim, if set, says whether a fresh press claims: it is asked only
	// about a key going down on the keyboard or a button going down on a
	// connected standard gamepad. Nil selects DefaultClaim.
	Claim func(ev actions.Event) bool
	// Prefer, if set, is the seat a free device goes to first. Nil, or an
	// answer out of range, selects seat 0.
	Prefer func(d Device) int
	// Rest says when a seat's device counts as at rest for arming: a pad
	// level below Rest.Button or a stick inside Rest.Axis. The zero Rest
	// means exactly 0, after Deadzone.
	Rest actions.Rest
	// Deadzone is the radial stick deadzone the pad poller uses, so a
	// resting stick in a poll reads as at rest. Zero selects 0.15.
	Deadzone float64
	// Ticks, if set, gives each seat a tick buffer on its Map, cleared at
	// each change of who holds what.
	Ticks *actions.TickOptions
}

// DefaultClaim claims with Enter, Numpad Enter or Space on the keyboard and
// with button A (0) on a gamepad.
func DefaultClaim(ev actions.Event) bool {
	switch ev.Source.Kind {
	case actions.KindKey:
		return ev.Source.Code == "Enter" || ev.Source.Code == "NumpadEnter" || ev.Source.Code == "Space"
	case actions.KindPadButton:
		return ev.Source.Code == "button0"
	}
	return false
}

// ErrConfig is returned by New for a Config it cannot use.
var ErrConfig = errors.New("seats: Config needs NewMap and at least one seat")

type routeSrc struct {
	route string
	src   actions.Source
}

// pad is one connected standard gamepad in the latest poll.
type pad struct {
	slot int
	raw  string
	rest bool
}

// Seats is the seats of one local session. It is not safe for concurrent use.
type Seats struct {
	cfg     Config
	router  *actions.Router
	maps    []*actions.Map
	ticks   []*actions.Ticks
	seat    []*Device
	lost    []*Device
	armed   []bool
	touched []bool
	// levels are the physical levels from the events fed, by route and
	// source, for every route, claimed or not.
	levels  map[routeSrc]float64
	pads    []pad
	modal   bool
	gen     uint64
	changed bool
}

// New creates the seats.
func New(cfg Config) (*Seats, error) {
	if cfg.Seats == 0 {
		cfg.Seats = 2
	}
	if cfg.NewMap == nil || cfg.Seats < 1 {
		return nil, ErrConfig
	}
	if cfg.Claim == nil {
		cfg.Claim = DefaultClaim
	}
	if cfg.Deadzone <= 0 {
		cfg.Deadzone = .15
	}
	s := &Seats{cfg: cfg, levels: map[routeSrc]float64{}}
	n := cfg.Seats
	s.maps = make([]*actions.Map, n)
	for i := range s.maps {
		m := cfg.NewMap()
		if m == nil || slices.Contains(s.maps[:i], m) {
			return nil, ErrConfig
		}
		s.maps[i] = m
	}
	s.router = actions.NewRouter(s.maps...)
	if cfg.Ticks != nil {
		s.ticks = make([]*actions.Ticks, n)
		for i, m := range s.maps {
			s.ticks[i] = actions.NewTicks(m, *cfg.Ticks)
		}
	}
	s.seat, s.lost = make([]*Device, n), make([]*Device, n)
	s.armed, s.touched = make([]bool, n), make([]bool, n)
	return s, nil
}

func (s *Seats) valid(seat int) bool { return seat >= 0 && seat < len(s.seat) }

// Len is the number of seats.
func (s *Seats) Len() int { return len(s.seat) }

// Map is a seat's Map, or nil for a seat out of range.
func (s *Seats) Map(seat int) *actions.Map {
	if !s.valid(seat) {
		return nil
	}
	return s.maps[seat]
}

// Ticks is a seat's tick buffer, or nil without Config.Ticks.
func (s *Seats) Ticks(seat int) *actions.Ticks {
	if s.ticks == nil || !s.valid(seat) {
		return nil
	}
	return s.ticks[seat]
}

// Device is the device a seat holds.
func (s *Seats) Device(seat int) (Device, bool) {
	if !s.valid(seat) || s.seat[seat] == nil {
		return Device{}, false
	}
	return *s.seat[seat], true
}

// Lost is the device a waiting seat lost.
func (s *Seats) Lost(seat int) (Device, bool) {
	if !s.valid(seat) || s.lost[seat] == nil {
		return Device{}, false
	}
	return *s.lost[seat], true
}

// Hand is the hand a seat's prompts are written for: its device's. A seat
// with no device has none.
func (s *Seats) Hand(seat int) (hand.Hand, bool) {
	d, ok := s.Device(seat)
	if !ok {
		return hand.Hand{}, false
	}
	return d.Hand(), true
}

// SeatFor is the seat a route (a device name) feeds.
func (s *Seats) SeatFor(route string) (int, bool) {
	for i, d := range s.seat {
		if d != nil && slices.Contains(d.Routes(), route) {
			return i, true
		}
	}
	return 0, false
}

// Armed reports whether a seat may act in the game now.
func (s *Seats) Armed(seat int) bool { return s.valid(seat) && s.armed[seat] }

// Ready reports whether every seat holds a device and none waits.
func (s *Seats) Ready() bool {
	for i := range s.seat {
		if s.seat[i] == nil || s.lost[i] != nil {
			return false
		}
	}
	return true
}

// AnyLost reports whether a seat waits for a lost device.
func (s *Seats) AnyLost() bool {
	for _, d := range s.lost {
		if d != nil {
			return true
		}
	}
	return false
}

// Generation counts the changes of who holds what. A game that remembers
// presses across frames (a receiver's key went down, so its release
// throws) forgets them when it changes.
func (s *Seats) Generation() uint64 { return s.gen }

// TakeChanged reports, once, whether who holds what changed since the
// last call.
func (s *Seats) TakeChanged() bool {
	c := s.changed
	s.changed = false
	return c
}

// Pads are the connected standard gamepads of the latest poll, as devices.
func (s *Seats) Pads() []Device {
	out := make([]Device, 0, len(s.pads))
	for _, p := range s.pads {
		out = append(out, padDevice(p.slot, p.raw))
	}
	return out
}

func padDevice(slot int, raw string) Device {
	return Device{Name: actions.PadDevice(slot), Class: actions.ClassGamepad, Pad: slot, ID: actions.ParsePadID(raw)}
}

// Pressed reports a press of the named action on any claimed seat, armed or
// not: menus and pause read it, so an unclaimed device drives nothing.
func (s *Seats) Pressed(name string) bool {
	for i, m := range s.maps {
		if s.seat[i] != nil && m.Pressed(name) {
			return true
		}
	}
	return false
}

// SyncPads takes one poll's gamepads, before any of the poll's events are
// fed. A seat whose pad left, or whose slot now shows another id, loses its
// device: its routes are cut, its held input is released as canceled and
// it waits. It returns the seats lost by this call.
func (s *Seats) SyncPads(states []host.GamepadState) (lost []int) {
	s.pads = s.pads[:0]
	for _, p := range states {
		if p.Mapping != "standard" {
			continue
		}
		s.pads = append(s.pads, pad{slot: p.Index, raw: p.ID, rest: s.padAtRest(p)})
	}
	for i, d := range s.seat {
		if d == nil || d.Keyboard() {
			continue
		}
		if !slices.ContainsFunc(s.pads, func(p pad) bool { return p.slot == d.Pad && p.raw == d.ID.Raw }) {
			lost = append(lost, i)
			s.unroute(i)
			s.lost[i], s.seat[i] = d, nil
			s.maps[i].ReleaseAll()
		}
	}
	if len(lost) > 0 {
		s.transition()
	}
	return lost
}

// padAtRest reads a poll's pad: every button below Rest.Button and each
// stick, after the deadzone, inside Rest.Axis.
func (s *Seats) padAtRest(p host.GamepadState) bool {
	for _, v := range p.Buttons {
		if v != 0 && math.Abs(v) >= s.cfg.Rest.Button {
			return false
		}
	}
	for i := 0; i < len(p.Axes); i += 2 {
		m := math.Abs(p.Axes[i])
		if i+1 < len(p.Axes) {
			m = math.Hypot(p.Axes[i], p.Axes[i+1])
		}
		if m < s.cfg.Deadzone {
			continue
		}
		if m := (math.Min(m, 1) - s.cfg.Deadzone) / (1 - s.cfg.Deadzone); m >= s.cfg.Rest.Axis {
			return false
		}
	}
	return true
}

func (s *Seats) unroute(seat int) {
	if d := s.seat[seat]; d != nil {
		for _, r := range d.Routes() {
			s.router.Unassign(r, -1)
		}
	}
}

func (s *Seats) route(seat int) {
	for _, r := range s.seat[seat].Routes() {
		_ = s.router.Assign(r, seat)
	}
}

// transition disarms every seat after a change of who holds what.
func (s *Seats) transition() {
	s.gen++
	s.changed = true
	s.Disarm()
}

// Disarm withdraws every seat's held input and drops its tick buffer; each
// seat arms again on a frame its device rests through.
func (s *Seats) Disarm() {
	for i, m := range s.maps {
		s.armed[i] = false
		m.SuppressHeld()
		if s.ticks != nil {
			s.ticks[i].Clear()
		}
	}
}

// Feed implements actions.Sink.
func (s *Seats) Feed(ev actions.Event) {
	if ev.Device == "" {
		// An event with no device cannot claim or be told apart.
		s.router.Feed(ev)
		return
	}
	fresh := false
	if ev.Source.Kind != actions.KindMouseMotion && !ev.Source.Zero() {
		k := routeSrc{ev.Device, ev.Source}
		was := s.levels[k]
		if ev.Value == 0 {
			delete(s.levels, k)
		} else {
			s.levels[k] = ev.Value
		}
		fresh = math.Abs(was) < actions.DefaultThreshold && math.Abs(ev.Value) >= actions.DefaultThreshold
		if seat, ok := s.SeatFor(ev.Device); ok {
			s.touched[seat] = true
		}
	}
	if _, held := s.SeatFor(ev.Device); held {
		s.router.Feed(ev)
		return
	}
	if fresh && !s.modal {
		s.claim(ev)
	}
}

// claim gives a seat to the device of a fresh press, by the rules.
func (s *Seats) claim(ev actions.Event) {
	var d Device
	switch {
	case ev.Device == actions.DeviceKeyboard && ev.Source.Kind == actions.KindKey:
		d = Device{Name: actions.DeviceKeyboard, Class: actions.ClassKeyboardMouse, Pad: -1}
	case actions.IsPadDevice(ev.Device) && ev.Source.Kind == actions.KindPadButton:
		slot, err := strconv.Atoi(strings.TrimPrefix(ev.Device, "pad:"))
		if err != nil {
			return
		}
		i := slices.IndexFunc(s.pads, func(p pad) bool { return p.slot == slot })
		if i < 0 {
			return // not a connected standard pad
		}
		d = padDevice(slot, s.pads[i].raw)
	default:
		return
	}
	if !s.cfg.Claim(ev) {
		return
	}
	seat := -1
	if s.AnyLost() {
		var match []int
		for i, l := range s.lost {
			if l != nil && seat < 0 {
				seat = i
			}
			if l != nil && !d.Keyboard() && d.ID.Raw != "" && l.ID.Raw == d.ID.Raw {
				match = append(match, i)
			}
		}
		if len(match) == 1 {
			seat = match[0]
		}
	} else {
		first := 0
		if s.cfg.Prefer != nil {
			if p := s.cfg.Prefer(d); s.valid(p) {
				first = p
			}
		}
		for k := 0; k < len(s.seat); k++ {
			if i := (first + k) % len(s.seat); s.seat[i] == nil {
				seat = i
				break
			}
		}
	}
	if seat < 0 {
		return
	}
	s.seat[seat], s.lost[seat] = &d, nil
	s.route(seat)
	s.transition()
}

// ReleaseAll implements actions.Sink: the window lost the focus. Keys and
// the mouse count as up; gamepads keep their levels from the next poll.
// Every seat disarms.
func (s *Seats) ReleaseAll() {
	for k := range s.levels {
		if !actions.IsPadDevice(k.route) {
			delete(s.levels, k)
		}
	}
	s.router.ReleaseAll()
	s.gen++
	s.Disarm()
}

// SetModal tells the seats a dialog, the pause menu or a hidden page is up,
// at the moment it opens or closes. No press claims while it is up, and
// every seat disarms when it opens and when it closes.
func (s *Seats) SetModal(open bool) {
	if open != s.modal {
		s.modal = open
		s.gen++
		s.Disarm()
	}
}

// Update advances every seat's Map by dt, then arms each claimed seat whose
// device was at rest through the frame: nothing held, nothing sent.
func (s *Seats) Update(dt time.Duration) {
	s.router.Update(dt)
	for i, d := range s.seat {
		if !s.armed[i] && !s.modal && d != nil && !s.touched[i] && s.atRest(*d) {
			s.armed[i] = true
		}
		s.touched[i] = false
	}
}

// atRest reports whether a device holds nothing: no level from its events,
// and for a gamepad a resting poll.
func (s *Seats) atRest(d Device) bool {
	routes := d.Routes()
	for k, v := range s.levels {
		if !slices.Contains(routes, k.route) {
			continue
		}
		switch k.src.Kind {
		case actions.KindPadAxis, actions.KindTouchAxis:
			if math.Abs(v) >= s.cfg.Rest.Axis {
				return false
			}
		default:
			if math.Abs(v) >= s.cfg.Rest.Button {
				return false
			}
		}
	}
	if !d.Keyboard() {
		i := slices.IndexFunc(s.pads, func(p pad) bool { return p.slot == d.Pad })
		return i >= 0 && s.pads[i].rest
	}
	return true
}

// Swap exchanges two seats' devices, waiting or held, and disarms every
// seat. It reports false for a seat out of range.
func (s *Seats) Swap(a, b int) bool {
	if !s.valid(a) || !s.valid(b) {
		return false
	}
	if a == b {
		return true
	}
	s.unroute(a)
	s.unroute(b)
	s.maps[a].ReleaseAll()
	s.maps[b].ReleaseAll()
	s.seat[a], s.seat[b] = s.seat[b], s.seat[a]
	s.lost[a], s.lost[b] = s.lost[b], s.lost[a]
	for _, i := range []int{a, b} {
		if s.seat[i] != nil {
			s.route(i)
		}
	}
	s.transition()
	return true
}

// Release gives up one seat's device, as a player leaving the setup screen.
func (s *Seats) Release(seat int) {
	if !s.valid(seat) || (s.seat[seat] == nil && s.lost[seat] == nil) {
		return
	}
	s.unroute(seat)
	s.maps[seat].ReleaseAll()
	s.seat[seat], s.lost[seat] = nil, nil
	s.transition()
}

// Leave ends the session: no claims, no routes, nothing waiting.
func (s *Seats) Leave() {
	for i := range s.seat {
		s.unroute(i)
		s.maps[i].ReleaseAll()
		s.seat[i], s.lost[i] = nil, nil
	}
	s.transition()
}
