package netsync_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/game/netsync"
)

// TestBandwidth measures the bytes a room sends and receives per client with
// the roomtest bodies game, for docs/rooms.md. Every body moves every tick, so
// these are busy-scene numbers. Run with -v to see the table.
func TestBandwidth(t *testing.T) {
	var rows []string
	for _, rate := range []int{30, 60} {
		for _, people := range []int{2, 4, 8} {
			for _, props := range []int{0, 32} {
				h, m := newHarness(t, harnessOptions{seats: people, props: props, tickRate: rate,
					net: netsync.SimNet{Seed: 7, Up: 20 * time.Millisecond, Down: 20 * time.Millisecond}})
				clients := make([]*netsync.Client, people)
				for i := range clients {
					clients[i] = h.client(m, scripted, nil)
				}
				h.run(2*time.Second, clients, nil) // countdown and warm-up
				r := roomOf(t, h, clients[0])
				s0 := r.Stats()
				in0, out0 := clients[0].Stats().BytesIn, clients[0].Stats().BytesOut
				const secs = 10
				h.run(secs*time.Second, clients, nil)
				s1 := r.Stats()
				st := clients[0].Stats()
				snapBytes := float64(s1.KeyframeBytes+s1.DeltaBytes-s0.KeyframeBytes-s0.DeltaBytes) / secs
				snaps := float64(s1.Keyframes+s1.Deltas-s0.Keyframes-s0.Deltas) / secs
				keyAvg := float64(s1.KeyframeBytes-s0.KeyframeBytes) / float64(max(s1.Keyframes-s0.Keyframes, 1))
				deltaAvg := float64(s1.DeltaBytes-s0.DeltaBytes) / float64(max(s1.Deltas-s0.Deltas, 1))
				down := float64(st.BytesIn-in0) / secs
				up := float64(st.BytesOut-out0) / secs
				// WebSocket framing: 2 bytes per server frame, 6 per masked client frame.
				downWS := down + 2*snaps
				upWS := up + 6*float64(rate)
				delta := fmt.Sprintf("%.0f", deltaAvg)
				if s1.Deltas == s0.Deltas {
					delta = "keyframes only"
				}
				rows = append(rows, fmt.Sprintf("| %d Hz | %d | %d | %d | %.0f | %s | %.1f | %.1f | %.1f |",
					rate, people, props, 12+20*(people+props), keyAvg, delta, downWS/1024, upWS/1024, snapBytes*float64(people)/1024))
				if deltaAvg >= keyAvg {
					t.Errorf("%d Hz %d people %d props: delta %.0f B not smaller than keyframe %.0f B", rate, people, props, deltaAvg, keyAvg)
				}
			}
		}
	}
	t.Logf("\n| Tick rate | People | Props | State bytes | Keyframe B | Delta B | Down KiB/s per client | Up KiB/s per client | Server out KiB/s per room |\n|---|---|---|---|---|---|---|---|---|\n%s", strings.Join(rows, "\n"))
}
