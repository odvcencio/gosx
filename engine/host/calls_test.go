package host

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
	writes  []json.RawMessage
	marks   []json.RawMessage // writes to the started signal
	signals []string
	initial json.RawMessage
	// resultInitial is the result signal's value when the engine subscribes.
	resultInitial json.RawMessage
	// onWrite runs after each write, outside the lock, as a store's
	// notification of a subscriber would.
	onWrite  func()
	declared struct {
		channel string
		timeout time.Duration
		recent  int
		zero    string
	}
}

func (f *fakeCalls) Subscribe(signal string, h func(json.RawMessage)) (HandleFunc, error) {
	if strings.HasSuffix(signal, ".result") {
		f.results = h
		if f.resultInitial != nil {
			h(f.resultInitial)
		}
		return func() { f.results = nil }, nil
	}
	f.handler = h
	if f.initial != nil {
		h(f.initial)
	}
	return func() { f.handler = nil }, nil
}

func (f *fakeCalls) Write(signal string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.mu.Lock()
	if strings.HasSuffix(signal, ".started") {
		// The mark of a call whose handler is starting (ServeCallsAsync). It is not
		// a result, so the tests that count results do not see it.
		f.marks = append(f.marks, data)
		f.mu.Unlock()
		return nil
	}
	f.writes = append(f.writes, data)
	f.signals = append(f.signals, signal)
	f.mu.Unlock()
	if f.onWrite != nil {
		f.onWrite()
	}
	return nil
}

func (f *fakeCalls) Declare(channel string, timeout time.Duration, recent int, zero string) {
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
	if _, err := ServeCallsOn(f, "bv.cmd", CallOptions{Timeout: 3 * time.Second, Recent: 3}, adder); err != nil {
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
	_, _ = ServeCallsOn(f, "$c", CallOptions{}, adder)
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
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
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
	_, _ = ServeCallsOn(f, "c", CallOptions{}, func(string, addArgs) (sum, error) { panic("oops") })
	f.send(t, `{"ID":1,"Name":"x"}`)
	if r := f.result(t, 0); !strings.Contains(r.Error, "handler panic: oops") {
		t.Fatalf("result = %+v", r)
	}
}

func TestServeCallsKeepsRecentSoOverlappingCallsAreVisible(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{Recent: 2}, adder)
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
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	if len(f.writes) != 1 || f.result(t, 0).Value.Total != 8 {
		t.Fatalf("replayed writes = %s", f.writes)
	}
}

func TestServeCallsDisposeStopsReplies(t *testing.T) {
	f := &fakeCalls{}
	off, _ := ServeCallsOn(f, "c", CallOptions{}, adder)
	off.Dispose()
	if f.handler != nil {
		t.Fatal("dispose must unsubscribe")
	}
}

func TestServeCallsRequiresChannelAndHandler(t *testing.T) {
	f := &fakeCalls{}
	if _, err := ServeCallsOn(f, " $ ", CallOptions{}, adder); err == nil {
		t.Fatal("empty channel must fail")
	}
	if _, err := ServeCallsOn[addArgs, sum](f, "c", CallOptions{}, nil); err == nil {
		t.Fatal("nil handler must fail")
	}
}

// Shared signals outlive an engine instance. A remounted engine finds call 3
// and result 3 in the store and must not run call 3 again.
func TestServeCallsRemountDoesNotRunAnAnsweredCallAgain(t *testing.T) {
	runs := 0
	h := func(name string, a addArgs) (sum, error) { runs++; return sum{Total: a.A + a.B}, nil }
	f1 := &fakeCalls{}
	off, _ := ServeCallsOn(f1, "c", CallOptions{}, h)
	f1.send(t, `{"ID":3,"Name":"fire","Args":{"A":1,"B":1}}`)
	off.Dispose()
	if f1.handler != nil || f1.results != nil {
		t.Fatal("dispose must release both subscriptions")
	}
	f2 := &fakeCalls{
		initial:       json.RawMessage(`{"ID":3,"Name":"fire","Args":{"A":1,"B":1}}`),
		resultInitial: json.RawMessage(`{"ID":3,"Name":"fire","Value":{"Total":2}}`),
	}
	_, _ = ServeCallsOn(f2, "c", CallOptions{}, h)
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
	_, _ = ServeCallsOn(f, "c", CallOptions{}, func(string, panicArgs) (sum, error) { return sum{}, nil })
	f.send(t, `{"ID":1,"Name":"x","Args":{}}`)
	if r := f.result(t, 0); r.ID != 1 || !strings.HasPrefix(r.Error, "bad-args:") {
		t.Fatalf("result = %+v", r)
	}
}

func TestServeCallsWronglyTypedCallGetsBadCall(t *testing.T) {
	f := &fakeCalls{}
	runs := 0
	_, _ = ServeCallsOn(f, "c", CallOptions{}, func(string, addArgs) (sum, error) { runs++; return sum{}, nil })
	f.send(t, `{"ID":5,"Name":42,"Args":{}}`)
	f.send(t, `{"ID":"six","Name":"x"}`) // no usable ID: nothing to answer
	if len(f.writes) != 1 || runs != 0 {
		t.Fatalf("writes = %d, runs = %d", len(f.writes), runs)
	}
	if r := f.result(t, 0); r.ID != 5 || !strings.HasPrefix(r.Error, "bad-call:") {
		t.Fatalf("result = %+v", r)
	}
}
