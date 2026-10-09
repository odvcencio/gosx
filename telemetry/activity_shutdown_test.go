//go:build !js || !wasm

package telemetry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/internal/telemetryauthority"
)

func TestActivityWorkerExitReleasesOwnedState(t *testing.T) {
	for _, mode := range []string{"normal", "worker-clock", "close-clock", "expired-deadline"} {
		t.Run(mode, func(t *testing.T) {
			tel, clock := lifecycleOwner(t)
			var forbidden atomic.Bool
			codec := DomainCodec[int]{Name: "score", Version: 1, Fields: []FieldDefinition{{Name: "score", Type: FieldInt}}, Encode: func(f *FieldSet, v int) error {
				if forbidden.Load() {
					panic("shutdown called codec")
				}
				return f.Int("score", int64(v))
			}}
			var kinds [2]*ActivityKind[int, NoFields, NoFields]
			for i, name := range []string{"match", "round"} {
				var err error
				kinds[i], err = newActivityKind(tel, name, ActivityKindOptions{Outcomes: []string{"won"}, Reasons: []string{"complete"}}, ActivityCodecs[int, NoFields, NoFields]{Activity: codec})
				if err != nil {
					t.Fatal(err)
				}
			}
			loopKind, err := tel.NewLoopKind("simulation", LoopOptions{Budget: time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			var loops [3]*Loop
			for i := range loops {
				loops[i], err = loopKind.Instance()
				if err != nil {
					t.Fatal(err)
				}
				loops[i].Begin() // Pending tokens and health also need exit cleanup.
				if err := loops[i].Observe(time.Millisecond, TickInfo{}); err != nil {
					t.Fatal(err)
				}
			}
			group, err := tel.NewHubGroup("room", HubOptions{})
			if err != nil {
				t.Fatal(err)
			}
			activityBytes, hubBytes, miscBytes := tel.activities.bytes.Load(), tel.hubs.bytes.Load(), tel.miscBytes.Load()
			usage := tel.registry.Usage()
			h := hub.New("fixture")
			detach, err := group.Attach(h)
			if err != nil {
				t.Fatal(err)
			}
			defer detach()
			attachment := tel.hubs.attached[h]
			attachment.ClientConnected(h, nil, nil)
			var activities [3]*Activity[int, NoFields, NoFields]
			for i := range activities {
				activities[i], err = kinds[i%2].begin(ActivityStart[int]{Fields: i + 1, Loop: loops[i]})
				if err != nil {
					t.Fatal(err)
				}
			}
			checkpoint, err := activities[0].Checkpoint()
			if err != nil {
				t.Fatal(err)
			}
			if err := activities[0].Set(5); err != nil {
				t.Fatal(err)
			}
			c := &blockedActivityClock{Clock: clock, Ticker: clock.NewTicker(time.Second), entered: make(chan struct{}), release: make(chan struct{})}
			tel.opts.Clock = c
			tel.ticker, tel.ticks, tel.done = c, c.C(), make(chan struct{})
			tel.start = clock.Now()
			go tel.run()
			var release sync.Once
			unblock := func() { release.Do(func() { close(c.release) }) }
			defer func() {
				unblock()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = tel.Close(ctx)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if mode != "close-clock" {
				if err := clock.Advance(time.Second); err != nil {
					t.Fatal(err)
				}
				select {
				case <-c.entered:
				case <-ctx.Done():
					t.Fatal("worker did not enter clock callback")
				}
			}
			final, err := activities[2].End(ActivityEnd[int]{Outcome: "won", Reason: "complete", Fields: 9})
			if err != nil {
				t.Fatal(err)
			}
			tel.updateCore(clock.Now()) // Publish live charges before exit.
			forbidden.Store(true)
			var wantError error
			switch mode {
			case "normal":
				tel.signal(context.Background())
			case "worker-clock":
				c.failed.Store(true)
				wantError = ErrInvalidOptions
			case "close-clock":
				c.failed.Store(true)
				tel.signal(context.Background())
				wantError = ErrInvalidOptions
			case "expired-deadline":
				expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer stop()
				if err := tel.Close(expired); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
				wantError = context.DeadlineExceeded
			}
			unblock()
			select {
			case <-tel.done:
			case <-ctx.Done():
				t.Fatal("worker did not finish")
			}
			if err := tel.Close(context.Background()); !errors.Is(err, wantError) {
				t.Error("worker completion changed the close error", err)
			}
			for _, receipt := range []Receipt{checkpoint, final} {
				if !receipt.ready() {
					t.Error("worker exit left an accepted receipt pending")
				} else if err := receipt.Wait(context.Background()); err != nil {
					t.Error("accepted memory receipt lost its result", err)
				}
			}
			s := tel.activities
			if len(s.live) != 0 || len(s.attached) != 0 || s.bytes.Load() != activityBytes {
				t.Errorf("retained activities: live=%d attached=%d bytes=%d want=%d", len(s.live), len(s.attached), s.bytes.Load(), activityBytes)
			}
			for _, kind := range kinds {
				if kind.core.open != 0 || hubSample(t, tel, "gosx_activities_open", "kind", kind.core.name).Gauge != 0 {
					t.Error("worker exit retained an open-count contribution", kind.core.name, kind.core.open)
				}
			}
			if s.transactions.Load() != 0 || s.pool.mask.Load() != 0 {
				t.Error("completed operations retained transaction or codec leases")
			}
			for i := range s.pool.leases {
				lease := &s.pool.leases[i]
				if lease.used != 0 || lease.err != nil || lease.generation.Load()%2 != 0 || lease.values != [4096]byte{} || lease.scratch != [4096]byte{} || lease.cells != [64]stagedCell{} {
					t.Error("completed codec retained projection scratch")
				}
			}
			if len(tel.hubs.attached) != 0 || group.instances != 0 || group.clients != 0 || attachment.alive.Load() || attachment.clients != 0 || attachment.detach != nil || attachment.h != nil {
				t.Error("worker exit retained hub attachment state")
			}
			if tel.hubs.bytes.Load() != hubBytes || tel.miscBytes.Load() != miscBytes {
				t.Error("worker exit retained hub attachment reservations")
			}
			if group.metrics.instances == nil || hubSample(t, tel, "gosx_hub_instances", "hub", "room").Gauge != 0 || hubSample(t, tel, "gosx_hub_clients", "hub", "room").Gauge != 0 {
				t.Error("worker exit retained hub gauge contributions")
			}
			if !tel.loops.closed.Load() || loopKind.instances != 0 || hubSample(t, tel, "gosx_loop_instances", "loop", "simulation").Gauge != 0 {
				t.Error("worker exit retained loop instance contributions")
			}
			for i := range tel.loops.slots {
				slot := &tel.loops.slots[i]
				if slot.kind != nil || slot.pending != 0 || slot.data != (loopData{}) {
					t.Error("worker exit retained a loop token or health bins")
				}
			}
			if after := tel.registry.Usage(); after != usage {
				t.Error("exit changed fixed metric reservations", usage, after)
			}
			memory := usage.Bytes + tel.ownerBytes + tel.adapterBytes.Load() + activityBytes + hubBytes + tel.loops.bytes.Load()
			if hubSample(t, tel, "gosx_telemetry_memory_bytes").Gauge != float64(memory) {
				t.Error("memory gauge retained released activity or attachment charges")
			}
			if len(tel.wake) != 0 || !s.stopping.Load() || tel.Enabled() {
				t.Error("worker exit retained notifications or activity admission")
			}
			// The external hub must no longer retain the telemetry subscriber.
			detachProbe, err := h.UseTelemetryObserver(hub.NoopObserver{}, 64, telemetryauthority.New())
			if err != nil {
				t.Error("worker exit left the hub subscription reserved", err)
			} else {
				detachProbe()
			}
			view, err := activities[2].Snapshot()
			if err != nil || view.Outcome != "won" || len(view.Fields) != 1 || view.Fields[0].Int != 9 {
				t.Error("cleanup changed the caller's accepted final", view, err)
			}
		})
	}
}
