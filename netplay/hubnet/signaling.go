// Package hubnet carries netplay signaling and fallback datagrams over a GoSX
// hub WebSocket. It uses no dependency outside the core module.
package hubnet

import (
	"encoding/json"
	"errors"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay/signal"
)

// Signaling routes netplay signaling events between a hub and a handler.
type Signaling struct {
	h *hub.Hub
}

// AttachSignaling registers the netplay signaling events on h. fn receives
// each valid inbound message with the hub client ID that sent it. Invalid
// messages (bad size, session id, SDP or candidate) are dropped before fn runs.
//
// fn runs on the connection's read goroutine, so it must return quickly. It
// does not replace the hub's "join" or "leave" handlers; call the cleanup the
// signaling consumer offers from your own leave handler.
func AttachSignaling(h *hub.Hub, fn func(clientID string, m signal.Message)) *Signaling {
	s := &Signaling{h: h}
	for _, ev := range []string{signal.EventHello, signal.EventOffer, signal.EventCandidate} {
		ev := ev
		h.On(ev, func(ctx *hub.Context) {
			if ctx.Client == nil {
				return
			}
			m, err := signal.Parse(ev, ctx.Data)
			if err != nil {
				return
			}
			fn(ctx.Client.ID, m)
		})
	}
	return s
}

// Send validates m and delivers it to one hub client.
func (s *Signaling) Send(clientID string, m signal.Message) error {
	ev, data, err := signal.Marshal(m)
	if err != nil {
		return err
	}
	if s.h == nil {
		return errors.New("netplay/hubnet: no hub")
	}
	s.h.Send(clientID, ev, json.RawMessage(data))
	return nil
}
