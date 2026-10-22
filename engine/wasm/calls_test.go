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
	mu       sync.Mutex
	handler  func(json.RawMessage)
	writes   []json.RawMessage
	signals  []string
	initial  json.RawMessage
	declared struct {
		channel string
		timeout time.Duration
		recent  int
		zero    string
	}
}

func (f *fakeCalls) subscribe(_ string, h func(json.RawMessage)) (HandleFunc, error) {
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
	f.writes = append(f.writes, data)
	f.signals = append(f.signals, signal)
	f.mu.Unlock()
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
