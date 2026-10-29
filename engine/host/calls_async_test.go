package host

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// within fails the test unless cond becomes true soon.
func within(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *fakeCalls) writeCount() int { return len(f.writesCopy()) }

// notBlocked runs fn and fails the test if it does not return soon: the
// delivery of a call must never wait for the handler.
func notBlocked(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s blocked", what)
	}
}

// ServeCalls runs the handler inside the callback that delivers the call, so a
// handler that waits for the event loop deadlocks it. ServeCallsAsync returns
// from the delivery at once and runs the handler on a goroutine.
func TestServeCallsAsyncDeliveryDoesNotWaitForTheHandler(t *testing.T) {
	f := &fakeCalls{}
	release := make(chan struct{})
	entered := make(chan struct{})
	_, err := ServeCallsAsyncOn(f, "c", CallOptions{}, func(name string, a addArgs) (sum, error) {
		close(entered)
		<-release
		return sum{Total: a.A + a.B}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	notBlocked(t, "delivering a call", func() { f.send(t, `{"ID":1,"Name":"add","Args":{"A":2,"B":3}}`) })
	<-entered
	if n := f.writeCount(); n != 0 {
		t.Fatalf("a result was written before the handler returned: %d", n)
	}
	close(release)
	within(t, "the result", func() bool { return f.writeCount() == 1 })
	if r := f.allResults(t)[0]; r.ID != 1 || r.Value.Total != 5 || r.Error != "" {
		t.Fatalf("result = %+v", r)
	}
}

// Calls run one at a time, in ID order, even when one mailbox lists them out of
// order, and each result is written before the next handler starts.
func TestServeCallsAsyncRunsOneCallAtATimeInOrder(t *testing.T) {
	f := &fakeCalls{}
	var active, peak atomic.Int32
	var mu sync.Mutex
	var order []string
	var writtenWhenStarted []int
	_, _ = ServeCallsAsyncOn(f, "c", CallOptions{}, func(name string, _ addArgs) (sum, error) {
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		mu.Lock()
		order = append(order, name)
		writtenWhenStarted = append(writtenWhenStarted, f.writeCount())
		mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		active.Add(-1)
		return sum{}, nil
	})
	notBlocked(t, "delivering a mailbox", func() {
		f.send(t, `[{"ID":3,"Name":"c"},{"ID":1,"Name":"a"},{"ID":2,"Name":"b"}]`)
	})
	within(t, "three results", func() bool { return f.writeCount() == 3 })
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(order, []string{"a", "b", "c"}) || peak.Load() != 1 {
		t.Fatalf("handlers ran %v with %d at once", order, peak.Load())
	}
	if !reflect.DeepEqual(writtenWhenStarted, []int{0, 1, 2}) {
		t.Fatalf("results written when each handler started = %v, want [0 1 2]", writtenWhenStarted)
	}
	if got := ids(f.allResults(t)); !reflect.DeepEqual(got, []uint64{1, 2, 3}) {
		t.Fatalf("result IDs = %v", got)
	}
}

func TestServeCallsAsyncErrorsAndPanicsBecomeResults(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsAsyncOn(f, "c", CallOptions{}, func(name string, a addArgs) (panicky, error) {
		switch name {
		case "panic":
			panic("oops")
		case "unencodable":
			return panicky{Bad: true}, nil
		case "boom":
			return panicky{}, errString("no such thing")
		}
		return panicky{Total: a.A + a.B}, nil
	})
	f.send(t, `[{"ID":1,"Name":"boom"},{"ID":2,"Name":"panic"},{"ID":3,"Name":"add","Args":{"A":"x"}},{"ID":4,"Name":"unencodable"},{"ID":5,"Name":"add","Args":{"A":1,"B":1}}]`)
	within(t, "five results", func() bool { return f.writeCount() == 5 })
	var rs []struct {
		ID    uint64
		Error string
		Value panicky
	}
	for _, w := range f.writesCopy() {
		var r struct {
			ID    uint64
			Error string
			Value panicky
		}
		if err := json.Unmarshal(w, &r); err != nil {
			t.Fatal(err)
		}
		rs = append(rs, r)
	}
	if rs[0].Error != "no such thing" || !strings.Contains(rs[1].Error, "handler panic: oops") ||
		!strings.HasPrefix(rs[2].Error, "bad-args:") || !strings.HasPrefix(rs[3].Error, "reply-failed: encoder panic:") ||
		rs[4].Error != "" || rs[4].Value.Total != 2 {
		t.Fatalf("results = %+v", rs)
	}
}

// panicky is a reply type whose encoder panics when Bad is set.
type panicky struct {
	Total int
	Bad   bool `json:"-"`
}

func (p panicky) MarshalJSON() ([]byte, error) {
	if p.Bad {
		panic("cannot encode")
	}
	return json.Marshal(struct{ Total int }{p.Total})
}

// A reply type whose encoder panics is reported as an error, with ServeCalls
// as well as ServeCallsAsync, and the next call still runs.
func TestServeCallsAnEncoderPanicIsAnErrorResult(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, func(name string, _ addArgs) (panicky, error) {
		return panicky{Total: 1, Bad: name == "bad"}, nil
	})
	f.send(t, `[{"ID":1,"Name":"bad"},{"ID":2,"Name":"good"}]`)
	rs := f.allResults(t)
	if len(rs) != 2 || !strings.HasPrefix(rs[0].Error, "reply-failed: encoder panic:") || rs[1].Error != "" {
		t.Fatalf("results = %+v", rs)
	}
}

// Disposing drops calls that have not started and the reply of the call that is
// running; nothing is written afterwards.
func TestServeCallsAsyncDisposeDropsQueuedCallsAndLateReplies(t *testing.T) {
	f := &fakeCalls{}
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	var ran atomic.Int32
	off, _ := ServeCallsAsyncOn(f, "c", CallOptions{}, func(string, addArgs) (sum, error) {
		ran.Add(1)
		entered <- struct{}{}
		<-release
		return sum{}, nil
	})
	f.send(t, `[{"ID":1,"Name":"a"},{"ID":2,"Name":"b"}]`)
	<-entered
	off.Dispose()
	if f.handler != nil {
		t.Fatal("dispose must unsubscribe")
	}
	close(release)
	time.Sleep(30 * time.Millisecond)
	if n := f.writeCount(); n != 0 || ran.Load() != 1 {
		t.Fatalf("after dispose: %d results written, %d handlers run; want 0 and 1", n, ran.Load())
	}
}

// The result write may call back into the engine before it returns, on the
// same stack, as a notification of the page store does on the main thread. The
// engine must hold no lock then.
func TestServeCallsAsyncAWriteThatReentersDoesNotDeadlock(t *testing.T) {
	f := &fakeCalls{}
	var second atomic.Bool
	f.onWrite = func() {
		if second.CompareAndSwap(false, true) {
			f.handler(json.RawMessage(`{"ID":2,"Name":"add","Args":{"A":1,"B":1}}`))
		}
	}
	_, _ = ServeCallsAsyncOn(f, "c", CallOptions{}, adder)
	f.send(t, `{"ID":1,"Name":"add","Args":{"A":1,"B":2}}`)
	within(t, "both results", func() bool { return f.writeCount() == 2 })
	if got := ids(f.allResults(t)); !reflect.DeepEqual(got, []uint64{1, 2}) {
		t.Fatalf("result IDs = %v", got)
	}
}

// A call that arrives while others are queued joins the queue after them.
func TestServeCallsAsyncLaterDeliveriesQueueBehindEarlierOnes(t *testing.T) {
	f := &fakeCalls{}
	gate := make(chan struct{})
	var mu sync.Mutex
	var order []string
	_, _ = ServeCallsAsyncOn(f, "c", CallOptions{}, func(name string, _ addArgs) (sum, error) {
		if name == "a" {
			<-gate
		}
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
		return sum{}, nil
	})
	f.send(t, `{"ID":1,"Name":"a"}`)
	f.send(t, `{"ID":2,"Name":"b"}`)
	f.send(t, `[{"ID":2,"Name":"b"},{"ID":3,"Name":"c"}]`)
	close(gate)
	within(t, "three results", func() bool { return f.writeCount() == 3 })
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(order, []string{"a", "b", "c"}) {
		t.Fatalf("order = %v", order)
	}
}

// The queue goes idle when it empties and starts again for the next delivery,
// however many times that happens.
func TestServeCallsAsyncRestartsAfterTheQueueEmpties(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsAsyncOn(f, "c", CallOptions{}, adder)
	for id := 1; id <= 20; id++ {
		f.send(t, `{"ID":`+itoa(id)+`,"Name":"add","Args":{"A":1,"B":1}}`)
		n := id
		within(t, "a result", func() bool { return f.writeCount() == n })
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestServeCallsAsyncRequiresChannelAndHandler(t *testing.T) {
	f := &fakeCalls{}
	if _, err := ServeCallsAsyncOn(f, " $ ", CallOptions{}, adder); err == nil {
		t.Fatal("empty channel must fail")
	}
	if _, err := ServeCallsAsyncOn[addArgs, sum](f, "c", CallOptions{}, nil); err == nil {
		t.Fatal("nil handler must fail")
	}
}

// A remounted engine finds the old result and does not run an answered call
// again, with the async server too.
func TestServeCallsAsyncRemountDoesNotRunAnAnsweredCallAgain(t *testing.T) {
	var runs atomic.Int32
	f := &fakeCalls{
		initial:       json.RawMessage(`[{"ID":3,"Name":"fire"},{"ID":4,"Name":"fire"}]`),
		resultInitial: json.RawMessage(`{"ID":3,"Name":"fire","Value":{"Total":2}}`),
	}
	_, _ = ServeCallsAsyncOn(f, "c", CallOptions{}, func(string, addArgs) (sum, error) { runs.Add(1); return sum{}, nil })
	within(t, "call 4", func() bool { return f.writeCount() == 1 })
	if runs.Load() != 1 {
		t.Fatalf("handler runs = %d, want 1", runs.Load())
	}
}
