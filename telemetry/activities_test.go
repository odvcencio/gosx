//go:build !js || !wasm

package telemetry

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/telemetry/schema"
	"m31labs.dev/gosx/telemetry/telemetrytest"
)

func lifecycleOwner(t testing.TB) (*Telemetry, *telemetrytest.FakeClock) {
	t.Helper()
	tel := activityDeclarationOwner(t)
	clock := telemetrytest.NewClock(time.Unix(100, 0))
	tel.opts.Clock = clock
	tel.wake = make(chan struct{}, 1)
	return tel, clock
}
func emptyActivityKind(t *testing.T, tel *Telemetry) *ActivityKind[NoFields, NoFields, NoFields] {
	t.Helper()
	k, err := newActivityKind(tel, "match", ActivityKindOptions{Outcomes: []string{"won", "lost"}, Reasons: []string{"complete"}}, ActivityCodecs[NoFields, NoFields, NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func TestActivityFinalReceiptAndReservedSlot(t *testing.T) {
	tel, clock := lifecycleOwner(t)
	tel.opts.Activities.MaxOpen = 1
	k := emptyActivityKind(t, tel)
	a, err := k.begin(ActivityStart[NoFields]{})
	if err != nil || len(a.ID()) != 32 {
		t.Fatal(a, err)
	}
	if _, err := k.begin(ActivityStart[NoFields]{}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second)
	end := ActivityEnd[NoFields]{Outcome: "won", Reason: "complete"}
	receipt, err := a.End(end)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := a.End(end)
	if err != nil || duplicate.state != receipt.state {
		t.Fatal("duplicate final operation", err)
	}
	if _, err := a.End(ActivityEnd[NoFields]{Outcome: "lost", Reason: "complete"}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := receipt.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if receipt.ready() || len(tel.activities.live) != 1 {
		t.Fatal("wait cancelled admitted work")
	}
	if _, err := k.begin(ActivityStart[NoFields]{}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	tel.collectActivityReceipts()
	if err := receipt.Wait(context.Background()); err != nil || receipt.Persistence() != schema.PersistenceMemory {
		t.Fatal(err, receipt.Persistence())
	}
	if len(tel.activities.live) != 0 {
		t.Fatal("ack did not release reserved slot")
	}
	final, err := a.Snapshot()
	if err != nil || final.ElapsedMS != 2000 || final.Outcome != "won" {
		t.Fatal(final, err)
	}
	if _, err := k.begin(ActivityStart[NoFields]{}); err != nil {
		t.Fatal("slot not reusable", err)
	}
	if tel.activities.bytes.Load() > hubMiscBytes+activitySlotBytes {
		t.Fatal("unbounded live record state")
	}
}
func TestActivityProjectionRollbackAndConcurrentRevision(t *testing.T) {
	tel, _ := lifecycleOwner(t)
	cause := errors.New("private-canary")
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	codec := DomainCodec[int]{Name: "score", Version: 1, Fields: []FieldDefinition{{Name: "score", Type: FieldInt}}, Encode: func(f *FieldSet, v int) error {
		if err := f.Int("score", int64(v)); err != nil {
			return err
		}
		if v == -1 {
			return cause
		}
		if v == -2 {
			panic("private-canary")
		}
		if v > 0 {
			entered <- struct{}{}
			<-release
		}
		return nil
	}}
	k, err := newActivityKind(tel, "match", ActivityKindOptions{}, ActivityCodecs[int, NoFields, NoFields]{Activity: codec})
	if err != nil {
		t.Fatal(err)
	}
	a, err := k.begin(ActivityStart[int]{Fields: 0})
	if err != nil {
		t.Fatal(err)
	}
	before := a.entity.record
	for _, v := range []int{-1, -2} {
		err := a.Set(v)
		if err == nil || strings.Contains(err.Error(), "private-canary") {
			t.Fatal(err)
		}
		if v == -1 && !errors.Is(err, cause) {
			t.Fatal("lost encoder cause")
		}
		if a.entity.record.Envelope() != before.Envelope() {
			t.Fatal("failed encoding changed revision")
		}
	}
	results := make(chan error, 2)
	for _, v := range []int{1, 2} {
		go func(v int) { results <- a.Set(v) }(v)
	}
	<-entered
	<-entered
	close(release)
	var success, conflict int
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	copied, _ := a.Snapshot()
	copied.Fields[0].Int = 999
	current, _ := a.Snapshot()
	if current.Fields[0].Int == 999 {
		t.Fatal("mutable snapshot")
	}
}
func TestActivityCheckpointDirtyAndCopiedRevision(t *testing.T) {
	tel, clock := lifecycleOwner(t)
	k := emptyActivityKind(t, tel)
	a, err := k.begin(ActivityStart[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	first, err := a.Checkpoint()
	if err != nil || !first.ready() {
		t.Fatal(err)
	}
	revision := a.entity.record.Envelope().Revision
	v, _ := a.entity.record.Activity()
	if v.ElapsedMS != 1000 || a.entity.record.Envelope().State != schema.StateCheckpoint {
		t.Fatal(v)
	}
	clock.Advance(time.Second)
	second, err := a.Checkpoint()
	if err != nil || first.state != second.state || a.entity.record.Envelope().Revision != revision {
		t.Fatal("clean checkpoint wrote again", err)
	}
	if err = a.Set(NoFields{}); err != nil {
		t.Fatal(err)
	}
	third, err := a.Checkpoint()
	if err != nil || third.state == first.state {
		t.Fatal("dirty checkpoint was not admitted", err)
	}
	if err = third.Wait(nil); err == nil {
		t.Fatal("nil context")
	}
	if third.Persistence() != schema.PersistenceMemory {
		t.Fatal(third.Persistence())
	}
}
func TestActivityLoopHealthAndWallRollback(t *testing.T) {
	tel, clock := lifecycleOwner(t)
	k := emptyActivityKind(t, tel)
	loopKind, err := tel.NewLoopKind("simulation", LoopOptions{Budget: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	loop, err := loopKind.Instance()
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err = loop.Observe(1950*time.Microsecond, TickInfo{}); err != nil {
		t.Fatal(err)
	}
	a, err := k.begin(ActivityStart[NoFields]{Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.begin(ActivityStart[NoFields]{Loop: loop}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	clock.Advance(7 * time.Millisecond)
	clock.JumpWall(-time.Hour)
	receipt, err := a.End(ActivityEnd[NoFields]{Outcome: "won", Reason: "complete"})
	if err != nil {
		t.Fatal(err)
	}
	tel.collectActivityReceipts()
	if err = receipt.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	final, _ := a.Snapshot()
	if final.ElapsedMS != 7 || !final.ClockAdjusted || !final.EndedAt.Equal(final.StartedAt) || final.TickHealth == nil || final.TickHealth.P99MS != 2 || final.TickHealth.MaxMS != 1.95 {
		t.Fatal(final)
	}
	loop.Observe(time.Second, TickInfo{})
	again, _ := a.Snapshot()
	if again.TickHealth.Samples != 1 {
		t.Fatal("terminal health changed")
	}
}
func TestActivityShutdownDoesNotRunCodec(t *testing.T) {
	tel, clock := lifecycleOwner(t)
	var forbidden atomic.Bool
	codec := DomainCodec[int]{Name: "score", Version: 1, Fields: []FieldDefinition{{Name: "score", Type: FieldInt}}, Encode: func(f *FieldSet, v int) error {
		if forbidden.Load() {
			panic("shutdown called codec")
		}
		return f.Int("score", int64(v))
	}}
	k, err := newActivityKind(tel, "match", ActivityKindOptions{}, ActivityCodecs[int, NoFields, NoFields]{Activity: codec})
	if err != nil {
		t.Fatal(err)
	}
	a, err := k.begin(ActivityStart[int]{Fields: 5})
	if err != nil {
		t.Fatal(err)
	}
	forbidden.Store(true)
	clock.Advance(time.Second)
	if err = tel.stopActivities(context.Background()); err != nil {
		t.Fatal(err)
	}
	final, _ := a.Snapshot()
	if final.Outcome != "interrupted" || final.Reason != "server_shutdown" || final.Fields[0].Int != 5 || final.ElapsedMS != 1000 {
		t.Fatal(final)
	}
	if _, err = k.begin(ActivityStart[int]{Fields: 0}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
func TestActivityConcurrentIdenticalEnd(t *testing.T) {
	tel, _ := lifecycleOwner(t)
	k := emptyActivityKind(t, tel)
	a, err := k.begin(ActivityStart[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan Receipt, 4)
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := a.End(ActivityEnd[NoFields]{Outcome: "won", Reason: "complete"})
			results <- r
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var identity *receiptState
	for r := range results {
		if identity == nil {
			identity = r.state
		}
		if r.state != identity {
			t.Fatal("duplicate receipt identity")
		}
	}
	if k.core.open != 0 {
		t.Fatal("logical finish repeated")
	}
}
func TestActivityCapsAndEntropyCollisionRollback(t *testing.T) {
	tel, _ := lifecycleOwner(t)
	k := emptyActivityKind(t, tel)
	tel.opts.Activities.MaxRecordBytes = 300
	if _, err := k.begin(ActivityStart[NoFields]{}); !errors.Is(err, ErrFieldBudget) {
		t.Fatal(err)
	}
	if len(tel.activities.live) != 0 || k.core.open != 0 {
		t.Fatal("invalid record started a game")
	}
	tel.opts.Activities.MaxRecordBytes = 16 << 10
	tel.opts.Entropy = bytes.NewReader(make([]byte, 32))
	a, err := k.begin(ActivityStart[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.begin(ActivityStart[NoFields]{}); err == nil {
		t.Fatal("entropy collision overwrote live record")
	}
	if len(tel.activities.live) != 1 || tel.activities.live[string(a.ID())] != a.entity {
		t.Fatal("collision changed live admission")
	}
}

func TestActivityWorkerSignalThenDrainAndFlush(t *testing.T) {
	tel, clock := lifecycleOwner(t)
	k := emptyActivityKind(t, tel)
	tel.done = make(chan struct{})
	tel.ticker = clock.NewTicker(time.Second)
	tel.ticks = tel.ticker.C()
	tel.start = clock.Now()
	go tel.run()
	defer tel.Close(context.Background())
	a, err := k.begin(ActivityStart[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	tel.prepareShutdown(context.Background())
	if _, err = k.begin(ActivityStart[NoFields]{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err = a.Set(NoFields{}); err != nil {
		t.Fatal("source drain lost an admitted projection", err)
	}
	receipt, err := a.End(ActivityEnd[NoFields]{Outcome: "won", Reason: "complete"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = receipt.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tel.done:
		t.Fatal("activity wake finished shutdown before Flush")
	default:
	}
	if err = tel.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

type blockedActivityClock struct {
	Clock
	Ticker
	entered, release chan struct{}
	failed           atomic.Bool
}

func (c *blockedActivityClock) Now() Instant {
	if c.failed.Load() {
		panic("clock-test-canary")
	}
	return c.Clock.Now()
}
func (c *blockedActivityClock) Stamp(time.Time) Instant {
	close(c.entered)
	<-c.release
	return c.Now()
}

func TestActivityFinalReceiptCompletedAfterWorkerClockFailure(t *testing.T) {
	tel, clock := lifecycleOwner(t)
	k := emptyActivityKind(t, tel)
	loopKind, err := tel.NewLoopKind("simulation", LoopOptions{Budget: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	loop, err := loopKind.Instance()
	if err != nil {
		t.Fatal(err)
	}
	baseline := tel.activities.bytes.Load()
	a, err := k.begin(ActivityStart[NoFields]{Loop: loop})
	if err != nil {
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
	if err := clock.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.entered:
	case <-ctx.Done():
		t.Fatal("worker did not enter clock callback")
	}
	// The worker cannot collect the final's wake while its clock is blocked.
	receipt, err := a.End(ActivityEnd[NoFields]{Outcome: "won", Reason: "complete"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ready() {
		t.Fatal("receipt completed before worker collection")
	}
	c.failed.Store(true)
	unblock()
	select {
	case <-tel.done:
	case <-ctx.Done():
		t.Fatal("worker did not finish after clock failure")
	}
	if !receipt.ready() {
		t.Error("worker completion left the accepted final receipt pending")
	} else if err := receipt.Wait(context.Background()); err != nil {
		t.Error("accepted final lost memory persistence", err)
	}
	s := tel.activities
	s.mu.Lock()
	live, attached := len(s.live), len(s.attached)
	s.mu.Unlock()
	if live != 0 || attached != 0 || s.bytes.Load() != baseline {
		t.Errorf("accepted final retained resources: live=%d attached=%d bytes=%d want=%d", live, attached, s.bytes.Load(), baseline)
	}
	if !s.stopping.Load() || tel.Enabled() {
		t.Error("worker completion left activity admission open")
	}
	if err := tel.Close(context.Background()); !errors.Is(err, ErrInvalidOptions) {
		t.Error("worker clock failure was not preserved", err)
	}
}

func TestActivityIdleCheckpointAndTouch(t *testing.T) {
	tel, clock := lifecycleOwner(t)
	tel.opts.Activities.IdleTimeout = time.Minute
	k := emptyActivityKind(t, tel)
	a, err := k.begin(ActivityStart[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(50 * time.Second)
	a.Touch()
	tel.maintainActivities(clock.Now())
	clock.Advance(50 * time.Second)
	tel.maintainActivities(clock.Now())
	v, _ := a.Snapshot()
	if v.Outcome != "" {
		t.Fatal("Touch did not reset idle timeout")
	}
	clock.Advance(10 * time.Second)
	tel.maintainActivities(clock.Now())
	final, _ := a.Snapshot()
	if final.Outcome != "interrupted" || final.Reason != "idle" || final.ElapsedMS != 110000 {
		t.Fatal(final)
	}
	if len(tel.activities.live) != 0 {
		t.Fatal("idle final was not acknowledged")
	}
}

func BenchmarkMemoryReceiptWait(b *testing.B) {
	receipt := newMemoryReceipt()
	receipt.complete(nil)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := receipt.Wait(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
