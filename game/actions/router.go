package actions

import (
	"errors"
	"time"
)

// ErrBadPlayer means a player index is outside the Router.
var ErrBadPlayer = errors.New("actions: player index out of range")

// Router sends each device's events to the players it is assigned to, for
// local multiplayer. Player i owns maps[i]. One device may serve several
// players (two players can share a keyboard with different key bindings in
// their Maps); a player may have several devices.
type Router struct {
	maps    []*Map
	devices map[string][]int
	// OnUnassigned, if set, receives each press from a device that is not
	// assigned to any player. A lobby uses it to let a player join by pressing
	// a button.
	OnUnassigned func(Event)
}

// NewRouter creates a Router with one entry per Map.
func NewRouter(players ...*Map) *Router {
	return &Router{maps: players, devices: map[string][]int{}}
}

// Players returns the number of players.
func (r *Router) Players() int { return len(r.maps) }

// Player returns the Map for player i, or nil.
func (r *Router) Player(i int) *Map {
	if i < 0 || i >= len(r.maps) {
		return nil
	}
	return r.maps[i]
}

// Assign routes device to player. Assigning twice is a no-op.
func (r *Router) Assign(device string, player int) error {
	if player < 0 || player >= len(r.maps) {
		return ErrBadPlayer
	}
	for _, p := range r.devices[device] {
		if p == player {
			return nil
		}
	}
	r.devices[device] = append(r.devices[device], player)
	return nil
}

// Unassign stops routing device to player; player -1 removes every assignment
// of the device.
func (r *Router) Unassign(device string, player int) {
	list := r.devices[device]
	out := list[:0]
	for _, p := range list {
		if player >= 0 && p != player {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		delete(r.devices, device)
		return
	}
	r.devices[device] = out
	if m := r.Player(player); m != nil {
		m.ReleaseAll()
	}
}

// Devices returns the devices assigned to player, in no particular order.
func (r *Router) Devices(player int) []string {
	var out []string
	for dev, list := range r.devices {
		for _, p := range list {
			if p == player {
				out = append(out, dev)
			}
		}
	}
	return out
}

// PlayersFor returns the players a device is routed to.
func (r *Router) PlayersFor(device string) []int {
	return append([]int(nil), r.devices[device]...)
}

// Join assigns device to the first player that has no device of the same
// family (keyboard and mouse, or gamepad) and returns that player.
// It reports false when every player already has one.
func (r *Router) Join(device string) (int, bool) {
	if ps := r.devices[device]; len(ps) > 0 {
		return ps[0], true
	}
	pad := IsPadDevice(device)
	for i := range r.maps {
		has := false
		for dev, list := range r.devices {
			if IsPadDevice(dev) != pad {
				continue
			}
			for _, p := range list {
				if p == i {
					has = true
				}
			}
		}
		if !has {
			_ = r.Assign(device, i)
			return i, true
		}
	}
	return 0, false
}

// Feed delivers ev to the players its device is assigned to. An event with an
// empty Device goes to every player.
func (r *Router) Feed(ev Event) {
	if ev.Device == "" {
		for _, m := range r.maps {
			m.Feed(ev)
		}
		return
	}
	players := r.devices[ev.Device]
	if len(players) == 0 {
		if r.OnUnassigned != nil && ev.Value >= DefaultThreshold {
			r.OnUnassigned(ev)
		}
		return
	}
	for _, p := range players {
		r.maps[p].Feed(ev)
	}
}

// ReleaseAll releases every held input of every player.
func (r *Router) ReleaseAll() {
	for _, m := range r.maps {
		m.ReleaseAll()
	}
}

// Update advances every player's Map.
func (r *Router) Update(dt time.Duration) {
	for _, m := range r.maps {
		m.Update(dt)
	}
}

var (
	_ Sink = (*Map)(nil)
	_ Sink = (*Router)(nil)
)
