package wasm

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/engine"
)

type fakeCalls struct {
	mu      sync.Mutex
	handler func(json.RawMessage)
	results func(json.RawMessage)
	// started is the engine's subscription to the started signal. A write to the
	// signal reaches it, as a write to a shared signal reaches its subscribers.
	started func(json.RawMessage)
	writes  []json.RawMessage
	marks   []json.RawMessage // writes to the started signal
	signals []string
	initial json.RawMessage
	// resultInitial is the result signal's value when the engine subscribes.
	resultInitial json.RawMessage
	declared      struct {
		channel string
		timeout time.Duration
		recent  int
		zero    string
	}
}

func (f *fakeCalls) subscribe(signal string, h func(json.RawMessage)) (HandleFunc, error) {
	if strings.HasSuffix(signal, ".result") {
		f.results = h
		if f.resultInitial != nil {
			h(f.resultInitial)
		}
		return func() { f.results = nil }, nil
	}
	if strings.HasSuffix(signal, ".started") {
		f.mu.Lock()
		f.started = h
		f.mu.Unlock()
		return func() { f.mu.Lock(); f.started = nil; f.mu.Unlock() }, nil
	}
	f.handler = h
	if f.initial != nil {
		h(f.initial)
	}
	return func() { f.handler = nil }, nil
}

func (f *fakeCalls) write(signal string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.mu.Lock()
	if strings.HasSuffix(signal, ".started") {
		// The mark of a call whose handler is starting (ServeCallsAsync); not a
		// result. The page tells the engine's subscription about it before write
		// returns.
		f.marks = append(f.marks, data)
		started := f.started
		f.mu.Unlock()
		if started != nil {
			started(data)
		}
		return nil
	}
	defer f.mu.Unlock()
	f.writes = append(f.writes, data)
	f.signals = append(f.signals, signal)
	return nil
}

func (f *fakeCalls) declare(channel string, timeout time.Duration, recent int, zero string) {
	f.declared.channel, f.declared.timeout, f.declared.recent, f.declared.zero = channel, timeout, recent, zero
}

type addArgs struct{ A, B int }
type sum struct{ Total int }

func (f *fakeCalls) send(t *testing.T, raw string) { t.Helper(); f.handler(json.RawMessage(raw)) }

func (f *fakeCalls) result(t *testing.T, i int) engine.Result[sum] {
	t.Helper()
	var r engine.Result[sum]
	if err := json.Unmarshal(f.writes[i], &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func adder(name string, a addArgs) (sum, error) {
	if name == "boom" {
		return sum{}, errors.New("no such thing")
	}
	return sum{Total: a.A + a.B}, nil
}

func TestServeCallsRepliesOnResultSignal(t *testing.T) {
	f := &fakeCalls{}
	if _, err := serveCalls(f, "bv.cmd", CallOptions{Timeout: 3 * time.Second, Recent: 3}, adder); err != nil {
		t.Fatal(err)
	}
	if f.declared.channel != "$bv.cmd" || f.declared.timeout != 3*time.Second || f.declared.recent != 3 || !strings.Contains(f.declared.zero, `"Recent"`) {
		t.Fatalf("declared = %+v", f.declared)
	}
	f.send(t, `{"ID":1,"Name":"add","Args":{"A":2,"B":3}}`)
	if f.signals[0] != "$bv.cmd.result" {
		t.Fatalf("result signal = %q", f.signals[0])
	}
	if r := f.result(t, 0); r.ID != 1 || r.Name != "add" || r.Error != "" || r.Value.Total != 5 {
		t.Fatalf("result = %+v", r)
	}
}

func TestServeCallsIgnoresRepeatedAndOlderIDs(t *testing.T) {
	f := &fakeCalls{}
	_, _ = serveCalls(f, "$c", CallOptions{}, adder)
	f.send(t, `{"ID":2,"Name":"add","Args":{"A":1,"B":1}}`)
	f.send(t, `{"ID":2,"Name":"add","Args":{"A":1,"B":1}}`) // batch flush repeat
	f.send(t, `{"ID":1,"Name":"add","Args":{"A":9,"B":9}}`) // older
	f.send(t, `{"ID":0,"Name":"add"}`)
	f.send(t, `null`)
	if len(f.writes) != 1 {
		t.Fatalf("writes = %d, want 1: %s", len(f.writes), f.writes)
	}
}

func TestServeCallsBadArgsAndHandlerErrorsReply(t *testing.T) {
	f := &fakeCalls{}
	_, _ = serveCalls(f, "c", CallOptions{}, adder)
	f.send(t, `{"ID":1,"Name":"add","Args":{"A":"x"}}`)
	f.send(t, `{"ID":2,"Name":"boom","Args":{}}`)
	if r := f.result(t, 0); r.ID != 1 || !strings.HasPrefix(r.Error, "bad-args:") {
		t.Fatalf("bad args result = %+v", r)
	}
	if r := f.result(t, 1); r.ID != 2 || r.Error != "no such thing" {
		t.Fatalf("handler error result = %+v", r)
	}
}

func TestServeCallsPanicBecomesError(t *testing.T) {
	f := &fakeCalls{}
	_, _ = serveCalls(f, "c", CallOptions{}, func(string, addArgs) (sum, error) { panic("oops") })
	f.send(t, `{"ID":1,"Name":"x"}`)
	if r := f.result(t, 0); !strings.Contains(r.Error, "handler panic: oops") {
		t.Fatalf("result = %+v", r)
	}
}

func TestServeCallsKeepsRecentSoOverlappingCallsAreVisible(t *testing.T) {
	f := &fakeCalls{}
	_, _ = serveCalls(f, "c", CallOptions{Recent: 2}, adder)
	f.send(t, `{"ID":1,"Name":"add","Args":{"A":1,"B":1}}`)
	f.send(t, `{"ID":2,"Name":"boom"}`)
	f.send(t, `{"ID":3,"Name":"add","Args":{"A":1,"B":2}}`)
	r := f.result(t, 2)
	if len(r.Recent) != 2 || r.Recent[0].ID != 2 || r.Recent[0].Error != "no such thing" || r.Recent[1].ID != 3 {
		t.Fatalf("recent = %+v", r.Recent)
	}
}

func TestServeCallsReplaysCallWrittenBeforeSubscribe(t *testing.T) {
	f := &fakeCalls{initial: json.RawMessage(`{"ID":4,"Name":"add","Args":{"A":4,"B":4}}`)}
	_, _ = serveCalls(f, "c", CallOptions{}, adder)
	if len(f.writes) != 1 || f.result(t, 0).Value.Total != 8 {
		t.Fatalf("replayed writes = %s", f.writes)
	}
}

func TestServeCallsDisposeStopsReplies(t *testing.T) {
	f := &fakeCalls{}
	off, _ := serveCalls(f, "c", CallOptions{}, adder)
	off.Dispose()
	if f.handler != nil {
		t.Fatal("dispose must unsubscribe")
	}
}

func TestServeCallsRequiresChannelAndHandler(t *testing.T) {
	f := &fakeCalls{}
	if _, err := serveCalls(f, " $ ", CallOptions{}, adder); err == nil {
		t.Fatal("empty channel must fail")
	}
	if _, err := serveCalls[addArgs, sum](f, "c", CallOptions{}, nil); err == nil {
		t.Fatal("nil handler must fail")
	}
}

// Shared signals outlive an engine instance. A remounted engine finds call 3
// and result 3 in the store and must not run call 3 again.
func TestServeCallsRemountDoesNotRunAnAnsweredCallAgain(t *testing.T) {
	runs := 0
	h := func(name string, a addArgs) (sum, error) { runs++; return sum{Total: a.A + a.B}, nil }
	f1 := &fakeCalls{}
	off, _ := serveCalls(f1, "c", CallOptions{}, h)
	f1.send(t, `{"ID":3,"Name":"fire","Args":{"A":1,"B":1}}`)
	off.Dispose()
	if f1.handler != nil || f1.results != nil {
		t.Fatal("dispose must release both subscriptions")
	}
	f2 := &fakeCalls{
		initial:       json.RawMessage(`{"ID":3,"Name":"fire","Args":{"A":1,"B":1}}`),
		resultInitial: json.RawMessage(`{"ID":3,"Name":"fire","Value":{"Total":2}}`),
	}
	_, _ = serveCalls(f2, "c", CallOptions{}, h)
	if runs != 1 || len(f2.writes) != 0 {
		t.Fatalf("handler runs = %d (want 1), writes on the second instance = %d (want 0)", runs, len(f2.writes))
	}
	f2.send(t, `{"ID":4,"Name":"fire","Args":{"A":1,"B":1}}`)
	if runs != 2 {
		t.Fatalf("a new call after the remount must run, runs = %d", runs)
	}
}

type panicArgs struct{}

func (*panicArgs) UnmarshalJSON([]byte) error { panic("decoder blew up") }

func TestServeCallsDecoderPanicIsBadArgs(t *testing.T) {
	f := &fakeCalls{}
	_, _ = serveCalls(f, "c", CallOptions{}, func(string, panicArgs) (sum, error) { return sum{}, nil })
	f.send(t, `{"ID":1,"Name":"x","Args":{}}`)
	if r := f.result(t, 0); r.ID != 1 || !strings.HasPrefix(r.Error, "bad-args:") {
		t.Fatalf("result = %+v", r)
	}
}

func TestServeCallsWronglyTypedCallGetsBadCall(t *testing.T) {
	f := &fakeCalls{}
	runs := 0
	_, _ = serveCalls(f, "c", CallOptions{}, func(string, addArgs) (sum, error) { runs++; return sum{}, nil })
	f.send(t, `{"ID":5,"Name":42,"Args":{}}`)
	f.send(t, `{"ID":"six","Name":"x"}`) // no usable ID: nothing to answer
	if len(f.writes) != 1 || runs != 0 {
		t.Fatalf("writes = %d, runs = %d", len(f.writes), runs)
	}
	if r := f.result(t, 0); r.ID != 5 || !strings.HasPrefix(r.Error, "bad-call:") {
		t.Fatalf("result = %+v", r)
	}
}

// A mailbox written by an island handler reaches the handler as separate calls.
func TestServeCallsRunsEveryCallOfAMailbox(t *testing.T) {
	f := &fakeCalls{}
	_, _ = serveCalls(f, "c", CallOptions{Recent: 4}, adder)
	f.send(t, `[{"ID":1,"Name":"add","Args":{"A":1,"B":2}},{"ID":2,"Name":"add","Args":{"A":3,"B":4}}]`)
	if len(f.writes) != 2 || f.result(t, 0).Value.Total != 3 || f.result(t, 1).Value.Total != 7 {
		t.Fatalf("writes = %s", f.writes)
	}
	var last struct {
		Recent []struct {
			ID    uint64
			Value *sum
		}
	}
	if err := json.Unmarshal(f.writes[1], &last); err != nil || len(last.Recent) != 2 || last.Recent[0].Value == nil || last.Recent[0].Value.Total != 3 {
		t.Fatalf("Recent must carry the first reply's value: %s (%v)", f.writes[1], err)
	}
}

// ServeCallsAsync only queues the call on the delivery stack. The handler runs
// on a goroutine of its own and the result follows when it returns.
func TestServeCallsAsyncAnswersOffTheDeliveryStack(t *testing.T) {
	f := &fakeCalls{}
	release := make(chan struct{})
	_, err := serveCallsAsync(f, "bv.cmd", CallOptions{Timeout: 3 * time.Second}, func(name string, a addArgs) (sum, error) {
		<-release
		return sum{Total: a.A + a.B}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.declared.channel != "$bv.cmd" || f.declared.timeout != 3*time.Second {
		t.Fatalf("declared = %+v", f.declared)
	}
	f.send(t, `{"ID":1,"Name":"add","Args":{"A":2,"B":3}}`) // returns while the handler waits
	f.mu.Lock()
	n := len(f.writes)
	f.mu.Unlock()
	if n != 0 {
		t.Fatalf("a result was written before the handler returned")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		n = len(f.writes)
		f.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if n != 1 || f.result(t, 0).Value.Total != 5 {
		t.Fatalf("writes = %s", f.writes)
	}
}

type trimmingCalls struct {
	*fakeCalls
	trims [][2]any
}

func (c *trimmingCalls) trimCalls(signal string, through uint64) error {
	c.trims = append(c.trims, [2]any{signal, through})
	return nil
}

// A transport that can read and rewrite the page's store trims the answered
// calls; one that cannot is left alone.
func TestServeCallsTrimsThroughATransportThatCan(t *testing.T) {
	plain := &fakeCalls{}
	_, _ = serveCalls(plain, "c", CallOptions{}, adder)
	plain.send(t, `{"ID":1,"Name":"add"}`) // must not fail for want of a trimmer

	tr := &trimmingCalls{fakeCalls: &fakeCalls{}}
	_, _ = serveCalls(tr, "c", CallOptions{}, adder)
	tr.send(t, `[{"ID":1,"Name":"add"},{"ID":2,"Name":"add"}]`)
	if len(tr.trims) != 2 || tr.trims[0] != [2]any{"$c", uint64(1)} || tr.trims[1] != [2]any{"$c", uint64(2)} {
		t.Fatalf("trims = %v", tr.trims)
	}
}
