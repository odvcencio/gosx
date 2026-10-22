//go:build js && wasm

// Command browserpeer is the Go WASM player used by the real-browser netplay
// test. It dials the relay with netplay/client, plays a duel match and reports
// the result in window.__netplayResult as JSON. It must not import pion.
package main

import (
	"encoding/json"
	"net/url"
	"strconv"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/client"
	"m31labs.dev/gosx/netplay/webrtc/internal/duel"
)

type result struct {
	Player       int               `json:"player"`
	Frames       int64             `json:"frames"`
	Mode         string            `json:"mode"`
	Modes        []string          `json:"modes"`
	Degraded     bool              `json:"degraded"`
	RTTMillis    float64           `json:"rttMs"`
	MaxRTTMillis float64           `json:"maxRttMs"`
	Rollbacks    int               `json:"rollbacks"`
	Sums         map[string]uint64 `json:"sums"`
	Err          string            `json:"err,omitempty"`
}

func main() {
	q, _ := url.ParseQuery(js.Global().Get("location").Get("search").String()[1:])
	player, _ := strconv.Atoi(q.Get("player"))
	frames, _ := strconv.ParseInt(q.Get("frames"), 10, 64)
	fallbackMs, _ := strconv.Atoi(q.Get("fallbackMs"))
	hubURL := q.Get("hub")

	res := &result{Player: player, Sums: map[string]uint64{}}
	finish := func(err error) {
		if err != nil {
			res.Err = err.Error()
		}
		b, _ := json.Marshal(res)
		js.Global().Set("__netplayResult", string(b))
	}

	conn, err := client.Dial(client.Options{
		HubURL: hubURL, Session: "browser" + strconv.Itoa(player),
		FallbackAfter: time.Duration(fallbackMs) * time.Millisecond,
		OnMode:        func(m client.Mode) { res.Modes = append(res.Modes, m.String()) },
	})
	if err != nil {
		finish(err)
		select {}
	}
	game := duel.NewGame()
	sess, err := netplay.NewSession(netplay.Config[duel.Pad]{
		Game: game, Codec: duel.Codec{}, Transport: conn, LocalPlayer: player, InputDelay: 2, Relay: true,
		OnChecksum: func(f int64, s uint64) { res.Sums[strconv.FormatInt(f, 10)] = s },
	})
	if err != nil {
		finish(err)
		select {}
	}

	start := time.Now()
	tick := time.NewTicker(time.Second / 60)
	var maxRTT time.Duration
	for range tick.C {
		now := time.Since(start)
		sess.Advance(now, duel.Input(7, player, sess.Frame()))
		if rtt := sess.RTT(); rtt > maxRTT {
			maxRTT = rtt
		}
		if sess.Err() != nil {
			finish(sess.Err())
			select {}
		}
		if sess.Frame() >= frames || now > 60*time.Second {
			break
		}
	}
	tick.Stop()
	res.Frames = sess.Frame()
	res.Mode = conn.Mode().String()
	res.Degraded = conn.Degraded()
	res.RTTMillis = float64(sess.RTT()) / float64(time.Millisecond)
	res.MaxRTTMillis = float64(maxRTT) / float64(time.Millisecond)
	res.Rollbacks = sess.Stats().Rollbacks
	finish(nil)
	_ = conn.Close()
	select {}
}
