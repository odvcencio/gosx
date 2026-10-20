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
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/telemetry/schema"
)

func TestActivityWorkerExitReleasesOwnedState(t *testing.T) {
	for _, mode := range []string{"normal", "worker-clock", "close-clock", "expired-deadline"} {
		t.Run(mode, func(t *testing.T) {
			tel, clock := lifecycleOwner(t)
			// Session-link checks cover a configuration Enable does not allow yet.
			tel.opts.Sessions.Enabled = true
			tel.opts.Metrics.DisableRequests = false
			if err := tel.initializeRequests(); err != nil {
				t.Fatal(err)
			}
			tel.admitRequestCatalog(nil)
			var forbidden atomic.Bool
			codec := DomainCodec[int]{Name: "score", Version: 1, Fields: []FieldDefinition{{Name: "score", Type: FieldInt}}, Encode: func(f *FieldSet, v int) error {
				if forbidden.Load() {
					panic("shutdown called codec")
				}
				return f.Int("score", int64(v))
			}}
			var kinds [2]*ActivityKind[int, int, int]
			for i, name := range []string{"match", "round"} {
				var err error
				kinds[i], err = newActivityKind(tel, name, ActivityKindOptions{Outcomes: []string{"won"}, Reasons: []string{"complete"}, Roles: []string{"player"}, Events: []string{"round"}}, ActivityCodecs[int, int, int]{Activity: codec, Participant: codec, Event: codec})
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
			queue := tel.activities.events
			queueBytes := queue.used.Load()
			if queueBytes != recordQueueMetadataBytes(len(queue.slots)) {
				t.Fatal("empty queue did not reserve its backing headers")
			}
			parent, err := kinds[0].begin(ActivityStart[int]{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parent.End(ActivityEnd[int]{Outcome: "won", Reason: "complete"}); err != nil {
				t.Fatal(err)
			}
			tel.collectActivityReceipts()
			if tel.activities.nextKnown == 0 {
				t.Fatal("fixture did not populate completed parent IDs")
			}
			// No worker exists yet; discard only this fixture's completed wake.
			<-tel.wake
			h := hub.New("fixture")
			detach, err := group.Attach(h)
			if err != nil {
				t.Fatal(err)
			}
			defer detach()
			attachment := tel.hubs.attached[h]
			attachment.ClientConnected(h, nil, nil)
			var activities [3]*Activity[int, int, int]
			var participants [3]*Participant[int]
			for i := range activities {
				activities[i], err = kinds[i%2].begin(ActivityStart[int]{Fields: i + 1, Loop: loops[i], ParentID: parent.ID()})
				if err != nil {
					t.Fatal(err)
				}
				participants[i], err = activities[i].Participant(ParticipantStart[int]{Seat: 0, Role: "player", Human: true, Fields: i + 10})
				if err != nil {
					t.Fatal(err)
				}
				if err := participants[i].Joined(participantRef(tel, byte(i+1), true)); err != nil {
					t.Fatal(err)
				}
			}
			if err := clock.Advance(10 * time.Second); err != nil {
				t.Fatal(err)
			}
			if err := participants[0].Left(participantRef(tel, 1, true), "complete"); err != nil {
				t.Fatal(err)
			}
			observer := requestObserver{owner: tel}
			for range 3 {
				observer.ObserveRequestStart()
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
			started := mode != "close-clock"
			if started {
				go tel.run()
			}
			var release sync.Once
			unblock := func() { release.Do(func() { close(c.release) }) }
			defer func() {
				if !started {
					go tel.run()
				}
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
			for i, a := range activities {
				if err := a.Event("round", i+20); err != nil {
					t.Fatal(err)
				}
			}
			if queue.count != 3 || queue.used.Load() <= queueBytes {
				t.Fatal("fixture did not retain accepted event payloads")
			}
			final, err := activities[2].End(ActivityEnd[int]{Outcome: "won", Reason: "complete", Fields: 9})
			if err != nil {
				t.Fatal(err)
			}
			activities[0].entity.mu.Lock()
			unfinished, _ := activities[0].entity.record.Activity()
			activities[0].entity.mu.Unlock()
			if unfinished.Participants[0].SeatPresenceMS != 10000 {
				t.Fatal("fixture did not accumulate seat presence")
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
				go tel.run()
				started = true
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
			if s.known != [256]string{} || s.nextKnown != 0 {
				t.Error("worker exit retained completed parent IDs")
			}
			for _, a := range activities {
				a.entity.mu.Lock()
				presence := len(a.entity.presence)
				a.entity.mu.Unlock()
				if presence != 0 {
					t.Error("worker exit retained participant connection tokens")
				}
			}
			queue.mu.Lock()
			if queue.count != 0 || queue.inFlight != 0 || queue.used.Load() != queueBytes {
				t.Errorf("worker exit retained event payloads: queued=%d in_flight=%d bytes=%d want=%d", queue.count, queue.inFlight, queue.used.Load(), queueBytes)
			}
			for _, record := range queue.slots {
				if record != (schema.Record{}) {
					t.Error("worker exit retained a record in the event ring")
				}
			}
			queue.mu.Unlock()
			tel.requests.liveMu.Lock()
			liveRequests := tel.requests.live
			tel.requests.liveMu.Unlock()
			if liveRequests != 0 || hubSample(t, tel, "gosx_http_requests_in_flight").Gauge != 0 {
				t.Error("worker exit retained HTTP in-flight contributions", liveRequests)
			}
			// Late starts/completions and participant callbacks cannot revive state.
			observer.ObserveRequestStart()
			observer.Observe(server.RequestEvent{Kind: "runtime", Method: "GET", Status: 200})
			for i, p := range participants {
				if err := p.Set(i); !errors.Is(err, ErrClosed) {
					t.Error("closed participant accepted Set", err)
				}
				if err := p.Joined(participantRef(tel, byte(i+1), true)); !errors.Is(err, ErrClosed) {
					t.Error("closed participant accepted Joined", err)
				}
				if err := activities[i].Event("round", i); !errors.Is(err, ErrClosed) {
					t.Error("closed activity accepted an event", err)
				}
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
			memory := usage.Bytes + tel.ownerBytes + tel.adapterBytes.Load() + activityBytes + hubBytes + tel.loops.bytes.Load() + queueBytes
			if hubSample(t, tel, "gosx_telemetry_memory_bytes").Gauge != float64(memory) {
				t.Errorf("memory gauge retained released activity or attachment charges: got=%v want=%d queue_headers=%d", hubSample(t, tel, "gosx_telemetry_memory_bytes").Gauge, memory, queueBytes)
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
			if mode != "normal" {
				activities[0].entity.mu.Lock()
				isFinal := activities[0].entity.final
				last, _ := activities[0].entity.record.Activity()
				activities[0].entity.mu.Unlock()
				if isFinal {
					t.Fatal("fixture did not leave an unfinished activity")
				}
				if err := clock.Advance(10 * time.Second); err != nil {
					t.Fatal(err)
				}
				view, err := activities[0].Snapshot()
				if err != nil {
					t.Fatal("unfinished snapshot became unreadable after exit", err)
				}
				if len(view.Participants) != 1 || view.Participants[0].SeatPresenceMS != 10000 {
					t.Error("cleanup changed accumulated seat presence", view.Participants)
				}
				if view.ElapsedMS != last.ElapsedMS || !view.UpdatedAt.Equal(last.UpdatedAt) {
					t.Error("unfinished snapshot advanced after exit", view.ElapsedMS, last.ElapsedMS)
				}
			}
			view, err := activities[2].Snapshot()
			if err != nil || view.Outcome != "won" || len(view.Fields) != 1 || view.Fields[0].Int != 9 {
				t.Error("cleanup changed the caller's accepted final", view, err)
			}
			if len(view.Participants) != 1 || len(view.Participants[0].Fields) != 1 || view.Participants[0].Fields[0].Int != 12 || len(view.Participants[0].Sessions) != 1 || view.Participants[0].Client == nil || view.EventsAccepted != 1 {
				t.Error("cleanup changed copied participant or event history", view)
			}
		})
	}
}
