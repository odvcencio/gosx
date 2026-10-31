package host

import (
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests model the page between an engine and the shared signals when the
// engine does not run on the page's thread: a Web Worker, or a native engine in
// a desktop host.
//
// The engine's Write only posts a message; it does not wait for the page. The
// page decides later what to do with the post. Once the page has started to
// dispose the engine's record it applies none of the engine's writes and
// forwards nothing more to it, but the engine runs on until it handles the
// "dispose" message. A native host differs in one way: its Write fails once the
// page has attached another instance.
//
// The window between those two moments is the gap. A call whose handler starts
// in the gap must not run again in the engine that mounts next.

// postedView is one engine instance's connection to the page's store.
type postedView struct {
	page *pageStore
	// refuse is what Write does for a revoked engine: a worker's post succeeds and
	// the page drops it; a native host's write fails.
	refuse bool

	mu      sync.Mutex
	revoked bool
	dropped []string // "signal=value" for each write the page dropped
	held    []func() // messages to the engine that wait for flush
	holding string   // messages about signals with this suffix wait for flush
	lose    int      // the next messages about a started signal are lost
	inbox   chan func()
	gate    sync.Mutex // held by hold: the engine's event loop is busy
	done    chan struct{}
}

func newPostedView(t *testing.T, page *pageStore, refuse bool) *postedView {
	v := &postedView{page: page, refuse: refuse, inbox: make(chan func(), 1024), done: make(chan struct{})}
	go func() {
		for {
			select {
			case fn := <-v.inbox:
				v.gate.Lock()
				fn()
				v.gate.Unlock()
			case <-v.done:
				return
			}
		}
	}()
	t.Cleanup(func() { close(v.done) })
	return v
}

// Subscribe registers the engine with the page. Each notification, and the
// replay of the current value, reaches the engine as a message on its event
// loop, later than the write that caused it. A revoked engine hears nothing.
func (v *postedView) Subscribe(signal string, h func(json.RawMessage)) (HandleFunc, error) {
	return v.page.Subscribe(signal, func(data json.RawMessage) {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.revoked {
			return
		}
		deliver := func() { h(data) }
		if v.lose > 0 && strings.HasSuffix(signal, ".started") {
			v.lose--
			return
		}
		if v.holding != "" && strings.HasSuffix(signal, v.holding) {
			v.held = append(v.held, deliver)
			return
		}
		v.inbox <- deliver
	})
}

func (v *postedView) Write(signal string, value any) error {
	v.mu.Lock()
	revoked := v.revoked
	if revoked {
		data, _ := json.Marshal(value)
		v.dropped = append(v.dropped, signal+"="+string(data))
	}
	v.mu.Unlock()
	if revoked {
		if v.refuse {
			return errors.New("engine attachment is no longer current")
		}
		return nil
	}
	return v.page.Write(signal, value)
}

func (v *postedView) Declare(string, time.Duration, int, string) {}

// revoke is the page starting to dispose the engine.
func (v *postedView) revoke() { v.mu.Lock(); v.revoked = true; v.mu.Unlock() }

// holdBack keeps every message about a signal that ends in suffix from the
// engine, as a page does that has not yet reported the change.
func (v *postedView) holdBack(suffix string) { v.mu.Lock(); v.holding = suffix; v.mu.Unlock() }

// loseNext drops the next n messages to the engine about a started signal, as a
// bridge does that fails to deliver them. The page still applied the writes.
func (v *postedView) loseNext(n int) { v.mu.Lock(); v.lose = n; v.mu.Unlock() }

// flush hands the engine the messages that holdBack kept.
func (v *postedView) flush() {
	v.mu.Lock()
	held := v.held
	v.held, v.holding = nil, ""
	v.mu.Unlock()
	for _, fn := range held {
		v.inbox <- fn
	}
}

func (v *postedView) droppedWrites() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.dropped...)
}

func (v *postedView) didDrop(want string) bool {
	for _, w := range v.droppedWrites() {
		if w == want {
			return true
		}
	}
	return false
}

// notStarted fails the test if a handler enters within d.
func notStarted(t *testing.T, entered <-chan string, d time.Duration, why string) {
	t.Helper()
	select {
	case name := <-entered:
		t.Fatalf("the handler of %q started: %s", name, why)
	case <-time.After(d):
	}
}

// A page that disposes a worker engine drops the mark and the reply of whatever
// the engine does before it handles "dispose". The engine is in a handler when
// the page revokes it; the handler returns inside the gap and the next call is
// queued. If the engine marked that call and ran it at once, the page would
// drop the mark, and the engine that mounts next would find a call with no
// result and no mark, and run it again. A save or an order would happen twice.
//
// The engine therefore starts a handler only after the page has taken the
// call's mark, which it learns from the page's report of the change on its own
// subscription. A page that has revoked the engine takes nothing.
func TestServeCallsAsyncRemountDuringAnInFlightCallRunsEachCallOnce(t *testing.T) {
	for _, mode := range []struct {
		name   string
		refuse bool
	}{
		{"worker: the page drops the post", false},
		{"native: the write fails", true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			page := newPageStore()
			var mu sync.Mutex
			runs := map[string]int{}
			entered := make(chan string, 8)
			release := make(chan struct{})
			handler := func(name string, a addArgs) (sum, error) {
				mu.Lock()
				runs[name]++
				mu.Unlock()
				entered <- name
				if name == "save" {
					<-release // a save that waits for storage
				}
				return sum{Total: a.A + a.B}, nil
			}

			a := newPostedView(t, page, mode.refuse)
			offA, err := ServeCallsAsyncOn(a, "bv.save", CallOptions{}, handler)
			if err != nil {
				t.Fatal(err)
			}
			// One island handler writes two calls. Call 1 starts; call 2 waits behind it.
			_ = page.Write("$bv.save", []map[string]any{
				{"ID": 1, "Name": "save"},
				{"ID": 2, "Name": "order", "Args": map[string]int{"A": 20, "B": 22}},
			})
			if got := <-entered; got != "save" {
				t.Fatalf("first handler = %q", got)
			}

			// The page starts to dispose engine A: from now on it drops A's writes. A has
			// not handled "dispose" yet, so its handler returns and its server goes on.
			a.revoke()
			close(release)
			within(t, "the page to drop the reply of call 1", func() bool {
				for _, w := range a.droppedWrites() {
					if strings.HasPrefix(w, "$bv.save.result=") {
						return true
					}
				}
				return false
			})
			within(t, "engine A to try to mark call 2", func() bool { return a.didDrop("$bv.save.started=2") })
			notStarted(t, entered, 60*time.Millisecond, "the page took no mark for call 2, so the next engine will run call 2 again")

			offA.Dispose() // A handles the "dispose" message

			b := newPostedView(t, page, false)
			offB, err := ServeCallsAsyncOn(b, "bv.save", CallOptions{}, handler)
			if err != nil {
				t.Fatal(err)
			}
			defer offB.Dispose()
			within(t, "engine B to answer call 2", func() bool { return page.result(t).ID == 2 })
			time.Sleep(30 * time.Millisecond) // a second run, if any, would show by now

			mu.Lock()
			defer mu.Unlock()
			if runs["save"] != 1 || runs["order"] != 1 {
				t.Fatalf("handlers ran %v; each call must run exactly once across the two engines", runs)
			}
			if r := page.result(t); r.Error != "" || r.Value.Total != 42 {
				t.Fatalf("result of call 2 = %+v", r)
			}
			// The client hears of call 1 once, as interrupted: it started in A and A's
			// reply was dropped.
			var answers []struct {
				ID    uint64
				Error string
			}
			page.mu.Lock()
			for _, raw := range page.results {
				var r struct {
					ID    uint64
					Error string
				}
				if json.Unmarshal(raw, &r) == nil {
					answers = append(answers, r)
				}
			}
			page.mu.Unlock()
			if len(answers) != 2 || answers[0].ID != 1 || answers[0].Error != "interrupted" || answers[1].ID != 2 || answers[1].Error != "" {
				t.Fatalf("results written = %+v, want call 1 interrupted, then call 2 answered", answers)
			}
		})
	}
}

// The page took the mark of call 2 just before it revoked engine A, and the
// report of the mark is still on its way. The next engine finds the mark and
// answers call 2 interrupted. Whether A still starts the handler depends on
// which message the worker handles first, but the handler never runs twice.
func TestServeCallsAsyncAMarkTheEngineLearnsOfAfterTheRevokeStillRunsOnce(t *testing.T) {
	page := newPageStore()
	var mu sync.Mutex
	runs := map[string]int{}
	entered := make(chan string, 8)
	handler := func(name string, a addArgs) (sum, error) {
		mu.Lock()
		runs[name]++
		mu.Unlock()
		entered <- name
		return sum{Total: a.A + a.B}, nil
	}
	a := newPostedView(t, page, false)
	offA, _ := ServeCallsAsyncOn(a, "bv.save", CallOptions{}, handler)
	a.holdBack(".started") // the page took the mark, and has not reported it yet
	_ = page.Write("$bv.save", map[string]any{"ID": 1, "Name": "order", "Args": map[string]int{"A": 20, "B": 22}})
	within(t, "the page to take the mark", func() bool { return page.wrote("$bv.save.started") >= 1 })
	notStarted(t, entered, 40*time.Millisecond, "the page had not reported the mark yet")
	a.revoke() // the page starts to dispose A
	a.flush()  // the report was already on its way: A hears of it first, then "dispose"
	within(t, "the handler", func() bool { mu.Lock(); defer mu.Unlock(); return runs["order"] == 1 })
	offA.Dispose()

	b := newPostedView(t, page, false)
	offB, _ := ServeCallsAsyncOn(b, "bv.save", CallOptions{}, handler)
	defer offB.Dispose()
	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if runs["order"] != 1 {
		t.Fatalf("handlers ran %v; the call must run exactly once", runs)
	}
}

// A handler never starts before the page has taken the call's mark. A page that
// has not reported the mark yet holds the call back.
func TestServeCallsAsyncWaitsForThePageToReportTheMarkBeforeItRunsAHandler(t *testing.T) {
	page := newPageStore()
	entered := make(chan string, 8)
	v := newPostedView(t, page, false)
	off, _ := ServeCallsAsyncOn(v, "bv.save", CallOptions{}, func(name string, a addArgs) (sum, error) {
		entered <- name
		return sum{Total: a.A + a.B}, nil
	})
	defer off.Dispose()
	v.holdBack(".started")
	_ = page.Write("$bv.save", []map[string]any{{"ID": 1, "Name": "a", "Args": map[string]int{"A": 1, "B": 2}}, {"ID": 2, "Name": "b"}})
	within(t, "the mark of call 1", func() bool { return page.wrote("$bv.save.started") >= 1 })
	notStarted(t, entered, 60*time.Millisecond, "the page has not reported the mark of call 1")
	if page.wrote("$bv.save.started") != 1 {
		t.Fatalf("call 2 was marked before call 1 finished: %d marks", page.wrote("$bv.save.started"))
	}
	v.flush()
	if got := <-entered; got != "a" {
		t.Fatalf("first handler = %q", got)
	}
	within(t, "both results", func() bool { return page.result(t).ID == 2 })
}

// An engine that is disposed while it waits for the page lets go of the call:
// the handler does not run, and the goroutine that waited ends.
func TestServeCallsAsyncDisposeEndsTheWaitForTheMark(t *testing.T) {
	page := newPageStore()
	entered := make(chan string, 8)
	v := newPostedView(t, page, false)
	off, _ := ServeCallsAsyncOn(v, "bv.save", CallOptions{}, func(name string, _ addArgs) (sum, error) {
		entered <- name
		return sum{}, nil
	})
	v.holdBack(".started")
	_ = page.Write("$bv.save", map[string]any{"ID": 1, "Name": "save"})
	within(t, "the mark", func() bool { return page.wrote("$bv.save.started") >= 1 })
	within(t, "the engine to wait", func() bool { return goroutineIn("markStarted") })
	off.Dispose()
	within(t, "the waiting goroutine to end", func() bool { return !goroutineIn("markStarted") })
	v.flush() // the report arrives after the engine is gone
	notStarted(t, entered, 40*time.Millisecond, "the engine was disposed before the page reported the mark")
}

func goroutineIn(function string) bool {
	buf := make([]byte, 1<<20)
	return strings.Contains(string(buf[:runtime.Stack(buf, true)]), function)
}

// A write the page refuses (a native host that has attached another instance)
// means the mark is not in the page's store. The call must not start, because
// the next instance would run it again; the client is told it did not start.
func TestServeCallsAsyncAFailedMarkWriteNeverStartsTheCall(t *testing.T) {
	page := newPageStore()
	entered := make(chan string, 8)
	tr := &refusingStarted{postedView: newPostedView(t, page, false)}
	off, err := ServeCallsAsyncOn(tr, "bv.save", CallOptions{}, func(name string, _ addArgs) (sum, error) {
		entered <- name
		return sum{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer off.Dispose()
	_ = page.Write("$bv.save", map[string]any{"ID": 1, "Name": "save"})
	within(t, "the answer to call 1", func() bool { return page.result(t).ID == 1 })
	notStarted(t, entered, 40*time.Millisecond, "its mark could not be written")
	if r := page.result(t); r.Name != "save" || !strings.HasPrefix(r.Error, "not-started: ") {
		t.Fatalf("result = %+v, want the error not-started for call 1", r)
	}
}

// refusingStarted fails every write to a started signal and takes the rest.
type refusingStarted struct{ *postedView }

func (r *refusingStarted) Write(signal string, value any) error {
	if strings.HasSuffix(signal, ".started") {
		return errors.New("engine attachment is no longer current")
	}
	return r.postedView.Write(signal, value)
}

// A page that never reports a mark (the bridge dropped the message, or the
// transport does not tell a writer about its own writes) must not hold up the
// channel for good. The wait ends at the mark timeout, the call is answered as
// not started, and the next call runs. The call timeout is left at its default,
// ten seconds: it is not what ends the wait.
func TestServeCallsAsyncAMarkThePageNeverReportsFailsOnlyThatCall(t *testing.T) {
	page := newPageStore()
	entered := make(chan string, 8)
	v := newPostedView(t, page, false)
	off, _ := ServeCallsAsyncOn(v, "bv.save", CallOptions{MarkTimeout: 60 * time.Millisecond}, func(name string, a addArgs) (sum, error) {
		entered <- name
		return sum{Total: a.A + a.B}, nil
	})
	defer off.Dispose()
	v.loseNext(1)
	_ = page.Write("$bv.save", []map[string]any{{"ID": 1, "Name": "a"}, {"ID": 2, "Name": "b", "Args": map[string]int{"A": 20, "B": 22}}})
	within(t, "the answer to call 2", func() bool { return page.result(t).ID == 2 })
	if got := <-entered; got != "b" {
		t.Fatalf("first handler = %q, want b: call 1 must not start without its mark", got)
	}
	notStarted(t, entered, 30*time.Millisecond, "call 1 was answered as not started")
	if r := page.result(t); r.Error != "" || r.Value.Total != 42 {
		t.Fatalf("result of call 2 = %+v", r)
	}
	var first struct {
		ID    uint64
		Name  string
		Error string
	}
	page.mu.Lock()
	raw := page.results[0]
	page.mu.Unlock()
	if err := json.Unmarshal(raw, &first); err != nil || first.ID != 1 || first.Name != "a" || !strings.HasPrefix(first.Error, "not-started: the page did not report the mark") {
		t.Fatalf("first result = %s (%v), want call 1 not started", raw, err)
	}
}

// A page that is only slow is not a page that is silent. Its main thread is busy
// for longer than CallOptions.Timeout, the deadline the page keeps for the
// client, and only then takes the mark and reports it. The engine must wait for
// that report, run the call late, and let the real reply replace the page's
// "timeout". Before the engine waited for the page the call ran at once and its
// reply came late; a wait that ended at CallOptions.Timeout would cancel it.
func TestServeCallsAsyncAPageSlowerThanTheCallTimeoutStillRunsTheCall(t *testing.T) {
	page := newPageStore()
	entered := make(chan string, 8)
	var mu sync.Mutex
	runs := 0
	v := newPostedView(t, page, false)
	off, err := ServeCallsAsyncOn(v, "bv.save", CallOptions{Timeout: 40 * time.Millisecond}, func(name string, a addArgs) (sum, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		entered <- name
		return sum{Total: a.A + a.B}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer off.Dispose()

	v.holdBack(".started") // the page is busy: it reports the mark later
	_ = page.Write("$bv.save", map[string]any{"ID": 1, "Name": "order", "Args": map[string]int{"A": 20, "B": 22}})
	within(t, "the mark of call 1", func() bool { return page.wrote("$bv.save.started") >= 1 })
	// Three times the page's deadline for the client passes, and the page still
	// has not reported the mark. The engine goes on waiting.
	notStarted(t, entered, 120*time.Millisecond, "the page has not reported the mark yet")
	if r := page.result(t); r.ID != 0 {
		t.Fatalf("the engine answered call 1 (%+v) while the page was only slow; the wait must not end at CallOptions.Timeout", r)
	}

	v.flush() // the page is free again and reports the mark
	select {
	case got := <-entered:
		if got != "order" {
			t.Fatalf("handler = %q, want order", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never ran after the page reported the mark")
	}
	within(t, "the reply to call 1", func() bool { return page.result(t).ID == 1 })
	time.Sleep(30 * time.Millisecond) // a second run, if any, would show by now
	if r := page.result(t); r.Error != "" || r.Value.Total != 42 || r.Name != "order" {
		t.Fatalf("result = %+v, want the real reply of call 1 (42)", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Fatalf("the handler ran %d times, want once", runs)
	}
}
