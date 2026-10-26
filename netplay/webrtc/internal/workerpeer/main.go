//go:build js && wasm

// Command workerpeer is the Go WASM player the worker real-browser netplay test
// runs inside a GoSX engine worker (engine.Config Thread: worker). It is
// browserpeer's match, but as an engine: the engine's props carry the settings
// (a worker has no page URL), the match runs on the worker's own thread, and
// the result goes to the page as an engine event. It must not import pion.
package main

import (
	"encoding/json"
	"strconv"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/engine/wasm"
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
	InWorker     bool              `json:"inWorker"`
	Route        string            `json:"route"`
	DatagramMode string            `json:"datagramMode"`
	MaxTickGapMS float64           `json:"maxTickGapMs"`
}

func main() {
	err := wasm.Register("NetplayPeer", func(ctx wasm.Context) (wasm.Handle, error) {
		var props struct {
			Player     int    `json:"player"`
			Frames     int64  `json:"frames"`
			FallbackMs int    `json:"fallbackMs"`
			Hub        string `json:"hub"`
		}
		if err := ctx.DecodeProps(&props); err != nil {
			return nil, err
		}
		go play(ctx, props.Player, props.Frames, props.FallbackMs, props.Hub)
		return wasm.HandleFunc(func() {}), nil
	})
	if err != nil {
		js.Global().Get("console").Call("error", err.Error())
	}
	select {}
}

func play(ctx wasm.Context, player int, frames int64, fallbackMs int, hubURL string) {
	res := &result{Player: player, Sums: map[string]uint64{}, InWorker: js.Global().Get("document").IsUndefined()}
	finish := func(err error) {
		if err != nil {
			res.Err = err.Error()
		}
		b, _ := json.Marshal(res)
		_ = ctx.Emit("netplay-result", map[string]any{"json": string(b)})
	}
	conn, err := client.Dial(client.Options{
		HubURL: hubURL, Session: "browser" + strconv.Itoa(player),
		FallbackAfter: time.Duration(fallbackMs) * time.Millisecond,
		OnMode:        func(m client.Mode) { res.Modes = append(res.Modes, m.String()) },
	})
	if err != nil {
		finish(err)
		return
	}
	game := duel.NewGame()
	sess, err := netplay.NewSession(netplay.Config[duel.Pad]{
		Game: game, Codec: duel.Codec{}, Transport: conn, LocalPlayer: player, InputDelay: 2, Relay: true,
		OnChecksum: func(f int64, s uint64) { res.Sums[strconv.FormatInt(f, 10)] = s },
	})
	if err != nil {
		finish(err)
		return
	}
	start := time.Now()
	tick := time.NewTicker(time.Second / 60)
	var maxRTT time.Duration
	var lastTick time.Time
	for range tick.C {
		now := time.Since(start)
		if t := time.Now(); !lastTick.IsZero() && float64(t.Sub(lastTick))/float64(time.Millisecond) > res.MaxTickGapMS {
			res.MaxTickGapMS = float64(t.Sub(lastTick)) / float64(time.Millisecond)
		}
		lastTick = time.Now()
		sess.Advance(now, duel.Input(7, player, sess.Frame()))
		if rtt := sess.RTT(); rtt > maxRTT {
			maxRTT = rtt
		}
		if sess.Err() != nil {
			finish(sess.Err())
			return
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
	// Which route the datagrams took: the worker shim reports transfer or relay.
	if api := js.Global().Get("__gosx").Get("netplay"); api.Truthy() && api.Get("mode").Type() == js.TypeFunction {
		res.DatagramMode = "see page"
	}
	finish(nil)
	_ = conn.Close()
}
