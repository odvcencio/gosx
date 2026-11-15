//go:build !js || !wasm

// Package rooms hosts the /demos/rooms demo: the examples/rooms bodies game,
// served by a room.Server with a bounded public policy, plus the static
// browser client (a js/wasm build of examples/rooms/client) that plays it.
package rooms

import (
	"net/url"
	"os"
	"strings"
	"time"

	"m31labs.dev/gosx/room"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
)

// Server is the room server type mounted at /demos/rooms/play/.
type Server = room.Server

// RoundMode is the one mode the docs site offers: two seats, six drifting
// props, a three second countdown and a 30 second round, so a visitor sees
// lobby, countdown, run and results in under a minute. examples/rooms/client
// defines the same mode under the name "round"; the browser and the server
// must agree on Name, Seats, Props and TickRate because both run the same
// simulation from the room's seed.
func RoundMode() mode.Mode {
	return roomtest.Mode(roomtest.Options{
		Name: "round", Seats: 2, Props: 6,
		Countdown: 3 * time.Second, Duration: 30 * time.Second,
	})
}

// Policy limits for a public demo. Every one is deliberately small: a visitor
// plays alone against a bot, so a handful of rooms is plenty.
const (
	MaxRooms          = 16
	ConnectsPerMinute = 20
	CreatesPerMinute  = 6
	IdleTimeout       = 15 * time.Second
	ReconnectGrace    = 10 * time.Second
	ResultsHold       = 8 * time.Second
)

// Origins returns the browser origins allowed to connect: the site's own
// public origin, and the loopback origins on the same port when the site runs
// over plain HTTP (a local `gosx dev`).
func Origins(publicBase string) []string {
	base := strings.TrimRight(strings.TrimSpace(publicBase), "/")
	if base == "" {
		return nil
	}
	origins := []string{base}
	if u, err := url.Parse(base); err == nil && u.Scheme == "http" {
		port := u.Port()
		for _, host := range []string{"localhost", "127.0.0.1"} {
			origin := "http://" + host
			if port != "" {
				origin += ":" + port
			}
			if origin != base {
				origins = append(origins, origin)
			}
		}
	}
	return origins
}

// Config is the public-site room policy: an explicit origin allowlist that
// requires an Origin header, a room cap, per-address rate limits, no
// spectators, and short idle, reconnect and results timers. Bots fill empty
// seats (the mode's NewBot), so one visitor can play. trustForwardedFor keys
// rate limits by the X-Forwarded-For address the single proxy in front of the
// site appends; leave it off unless that is true.
func Config(publicBase string, trustForwardedFor bool) room.Config {
	return room.Config{
		Modes:             []mode.Mode{RoundMode()},
		AllowedOrigins:    Origins(publicBase),
		RequireOrigin:     true,
		MaxRooms:          MaxRooms,
		MaxSpectators:     -1,
		ReconnectGrace:    ReconnectGrace,
		IdleTimeout:       IdleTimeout,
		ResultsHold:       ResultsHold,
		ConnectsPerMinute: ConnectsPerMinute,
		CreatesPerMinute:  CreatesPerMinute,
		TrustForwardedFor: trustForwardedFor,
	}
}

// NewServer builds the demo's room server for a site at publicBase. Set
// GOSX_DOCS_TRUST_FORWARDED_FOR=1 when exactly one proxy fronts the site and
// appends the peer address to X-Forwarded-For.
func NewServer(publicBase string) (*Server, error) {
	return room.NewServer(Config(publicBase, os.Getenv("GOSX_DOCS_TRUST_FORWARDED_FOR") == "1"))
}
