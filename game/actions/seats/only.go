package seats

import (
	"slices"

	"m31labs.dev/gosx/game/actions"
)

// Only is a device filter: it feeds sink only the events of the device that
// holds one seat. Use it where one player owns a screen that the others must
// not reach, such as the captain of a co-op team, whose device drives the
// play-call sheet and the menus while the partner's device drives only his
// own seat. A press counts for the seat whose device made it, never for
// whoever used a device last.
//
// seat names the seat at the moment of each event, so a change of owner
// takes effect on the next event. Feed the Seats before Only (an actions.Tee
// with the Seats first), so a claim made by an event already counts for that
// event. The keyboard's seat also gets the mouse (Device.Routes). A seat that
// holds no device, or waits for a lost one, feeds nothing; whether the seat
// is armed does not matter (a claimed Start may close the pause menu).
//
// When the device behind the seat changes (a claim, a loss, a swap, another
// owner), Only releases everything on sink before the next event, so a key
// or a button the old device held does not stay held.
func Only(s *Seats, seat func() int, sink actions.Sink) actions.Sink {
	return &only{seats: s, seat: seat, sink: sink}
}

type only struct {
	seats *Seats
	seat  func() int
	sink  actions.Sink
	// fed is the device whose events sink gets now, "" for none.
	fed string
}

// Feed implements actions.Sink.
func (o *only) Feed(ev actions.Event) {
	d, ok := o.seats.Device(o.seat())
	name := ""
	if ok {
		name = d.Name
	}
	if name != o.fed {
		if o.fed != "" {
			o.sink.ReleaseAll()
		}
		o.fed = name
	}
	if ok && slices.Contains(d.Routes(), ev.Device) {
		o.sink.Feed(ev)
	}
}

// ReleaseAll implements actions.Sink: everything on sink lets go.
func (o *only) ReleaseAll() { o.sink.ReleaseAll() }
