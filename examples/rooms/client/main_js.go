//go:build js && wasm

// Command client is the browser client of the rooms example: it connects to a
// room server with netsync, predicts the local body, interpolates the others,
// and draws everything on a 2D canvas. Without a reachable server it plays
// offline against bots.
package main

import (
	"fmt"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/game/netsync"
	"m31labs.dev/gosx/game/storage"
	"m31labs.dev/gosx/room/roomtest"
	"m31labs.dev/gosx/sim"
)

func main() {
	params := js.Global().Get("URLSearchParams").New(js.Global().Get("location").Get("search"))
	get := func(k, def string) string {
		if v := params.Call("get", k); !v.IsNull() && v.String() != "" {
			return v.String()
		}
		return def
	}
	server := get("server", "ws://localhost:8090/play/ws")
	duration := func(k string) time.Duration { d, _ := time.ParseDuration(get(k, "")); return d }
	modeName := get("mode", "duel")
	m := roomtest.Mode(roomtest.Options{Name: "duel", Seats: 2, Props: 6, Duration: 2 * time.Minute})
	if modeName == "party" {
		m = roomtest.Mode(roomtest.Options{Name: "party", Seats: 6, Props: 12, Duration: 3 * time.Minute, LobbyWait: 5 * time.Second})
	}

	var held byte
	keys := map[string]byte{"ArrowLeft": roomtest.Left, "a": roomtest.Left, "ArrowRight": roomtest.Right, "d": roomtest.Right,
		"ArrowUp": roomtest.Up, "w": roomtest.Up, "ArrowDown": roomtest.Down, "s": roomtest.Down, "Shift": roomtest.Boost}
	key := func(down bool) js.Func {
		return js.FuncOf(func(_ js.Value, args []js.Value) any {
			if bit, ok := keys[args[0].Get("key").String()]; ok {
				if down {
					held |= bit
				} else {
					held &^= bit
				}
			}
			return nil
		})
	}
	js.Global().Call("addEventListener", "keydown", key(true))
	js.Global().Call("addEventListener", "keyup", key(false))

	// Remote bodies render from the interpolation buffer, two ticks behind
	// the server; the local body renders from the prediction.
	ip := netsync.NewInterpolator(2, 32)
	client, err := netsync.New(netsync.Config{
		URL:   server,
		Mode:  m,
		Join:  netsync.Join{Code: get("code", ""), Private: get("private", "") == "1", Name: get("name", "")},
		Input: func(_ uint64, dst []byte) []byte { return append(dst, held) },
		Store: storage.New("rooms-example", storage.Session()), // per tab: two tabs are two players
		// Optional tuning from the page URL, for example ?reconnectTimeout=3s.
		ConnectTimeout:   duration("connectTimeout"),
		ReconnectTimeout: duration("reconnectTimeout"),
		StallTimeout:     duration("stallTimeout"),
		OnSnapshot: func(tick uint64, s sim.Simulation) {
			ip.Begin(tick)
			for i, b := range roomtest.Of(s).World.Bodies {
				ip.Add(uint32(i), []float32{float32(b.X), float32(b.Y)})
			}
		},
	})
	if err != nil {
		panic(err)
	}

	// window.gosxRoomsState() reports the client for overlays and tests.
	js.Global().Set("gosxRoomsState", js.FuncOf(func(js.Value, []js.Value) any {
		st, room := client.Stats(), client.Room()
		kinds := make([]any, len(room.Seats))
		for i, seat := range room.Seats {
			kinds[i] = seat.Kind.String()
		}
		return js.ValueOf(map[string]any{
			"status": client.Status().String(), "reason": client.Reason(), "local": client.Local(),
			"offline": room.Offline, "seat": client.Seat(), "code": room.Code, "phase": room.Phase.String(),
			"tick": float64(client.Tick()), "authTick": float64(st.AuthTick), "lead": st.Lead,
			"rttMs": float64(st.RTT.Milliseconds()), "dials": float64(st.Dials), "seats": kinds,
		})
	}))

	doc := js.Global().Get("document")
	canvas := doc.Call("getElementById", "stage")
	status := doc.Call("getElementById", "status")
	ctx := canvas.Call("getContext", "2d")
	w, h := canvas.Get("width").Float(), canvas.Get("height").Float()
	scale := w / (2 * roomtest.Arena)
	toScreen := func(x, y float32) (float64, float64) {
		return float64(x+roomtest.Arena) * scale, float64(y+roomtest.Arena) * scale * (h / w)
	}
	colors := []string{"#4cc9f0", "#f72585", "#ffd166", "#06d6a0", "#ef476f", "#b388ff"}
	dot := func(x, y float32, r float64, color string) {
		sx, sy := toScreen(x, y)
		ctx.Set("fillStyle", color)
		ctx.Call("beginPath")
		ctx.Call("arc", sx, sy, r, 0, 6.2832)
		ctx.Call("fill")
	}

	var last float64
	var frame js.Func
	frame = js.FuncOf(func(_ js.Value, args []js.Value) any {
		now := args[0].Float()
		delta := time.Duration(0)
		if last > 0 {
			delta = time.Duration((now - last) * float64(time.Millisecond))
		}
		last = now
		client.Frame(delta)

		ctx.Set("fillStyle", "#0b0f17")
		ctx.Call("fillRect", 0, 0, w, h)
		g := roomtest.Of(client.Sim())
		seat := client.Seat()
		if g != nil {
			seats := g.World.Seats
			if client.Status() == netsync.StatusOffline {
				for i, b := range g.World.Bodies { // offline: everything is local
					color := "#3a4252"
					if i < seats {
						color = colors[i%len(colors)]
					}
					dot(float32(b.X), float32(b.Y), 10, color)
				}
			} else {
				ip.Sample(client.ServerTick()-2, func(id uint32, v []float32) {
					if int(id) == seat {
						return
					}
					color := "#3a4252"
					if int(id) < seats {
						color = colors[int(id)%len(colors)]
					}
					dot(v[0], v[1], 10, color)
				})
				if seat >= 0 && seat < len(g.World.Bodies) {
					b := g.World.Bodies[seat]
					dot(float32(b.X), float32(b.Y), 13, colors[seat%len(colors)])
				}
			}
		}
		st := client.Stats()
		room := client.Room()
		status.Set("textContent", fmt.Sprintf("%s · room %s · %s · seat %d · rtt %v · ahead %d · lead %d",
			client.Status(), room.Code, room.Phase, seat, st.RTT.Round(time.Millisecond), st.Ahead, st.Lead))
		js.Global().Call("requestAnimationFrame", frame)
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
	select {}
}
