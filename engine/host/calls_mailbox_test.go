package host

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

// writesCopy returns the results written so far.
func (f *fakeCalls) writesCopy() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]json.RawMessage(nil), f.writes...)
}

func (f *fakeCalls) allResults(t *testing.T) []engine.Result[sum] {
	t.Helper()
	var out []engine.Result[sum]
	for _, w := range f.writesCopy() {
		var r engine.Result[sum]
		if err := json.Unmarshal(w, &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func ids(rs []engine.Result[sum]) []uint64 {
	out := make([]uint64, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

// An island handler that writes two calls delivers a mailbox. The engine runs
// both, in ID order, and writes both replies.
func TestServeCallsRunsEveryCallOfAMailboxInIDOrder(t *testing.T) {
	f := &fakeCalls{}
	var order []string
	_, _ = ServeCallsOn(f, "c", CallOptions{}, func(name string, a addArgs) (sum, error) {
		order = append(order, name)
		return sum{Total: a.A + a.B}, nil
	})
	f.send(t, `[{"ID":2,"Name":"b","Args":{"A":3,"B":4}},{"ID":1,"Name":"a","Args":{"A":1,"B":2}}]`)
	if !reflect.DeepEqual(order, []string{"a", "b"}) {
		t.Fatalf("handlers ran %v, want [a b]", order)
	}
	rs := f.allResults(t)
	if !reflect.DeepEqual(ids(rs), []uint64{1, 2}) || rs[0].Value.Total != 3 || rs[1].Value.Total != 7 {
		t.Fatalf("results = %+v", rs)
	}
}

// The newest result must let a client find the reply of an earlier call, with
// its value, even though only the newest result's own Value is a typed field.
func TestServeCallsRecentCarriesTheValuesOfEarlierReplies(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{Recent: 3}, func(name string, a addArgs) (sum, error) {
		if name == "boom" {
			return sum{}, errString("no such thing")
		}
		return sum{Total: a.A + a.B}, nil
	})
	f.send(t, `[{"ID":1,"Name":"a","Args":{"A":1,"B":2}},{"ID":2,"Name":"boom"},{"ID":3,"Name":"c","Args":{"A":3,"B":4}}]`)
	var last struct {
		ID     uint64
		Recent []struct {
			ID    uint64
			Error string
			Value *sum
		}
	}
	writes := f.writesCopy()
	if err := json.Unmarshal(writes[len(writes)-1], &last); err != nil {
		t.Fatal(err)
	}
	if len(last.Recent) != 3 || last.ID != 3 {
		t.Fatalf("last = %+v", last)
	}
	if last.Recent[0].Value == nil || last.Recent[0].Value.Total != 3 || last.Recent[0].Error != "" {
		t.Errorf("reply 1 in Recent = %+v", last.Recent[0])
	}
	if last.Recent[1].Error != "no such thing" || last.Recent[1].Value != nil {
		t.Errorf("an error reply carries its error and no value: %+v", last.Recent[1])
	}
	if last.Recent[2].Value == nil || last.Recent[2].Value.Total != 7 {
		t.Errorf("the newest reply is in Recent too: %+v", last.Recent[2])
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// Reply values in Recent stay inside a byte budget, newest first, so a result
// does not grow with the size of the replies. The newest reply's own Value is
// never affected.
func TestServeCallsRecentValuesStayInsideTheByteBudget(t *testing.T) {
	type big struct{ S string }
	run := func(budget int) (valueKeys []bool, top big) {
		f := &fakeCalls{}
		_, _ = ServeCallsOn(f, "c", CallOptions{Recent: 4, RecentBytes: budget}, func(string, addArgs) (big, error) {
			return big{S: strings.Repeat("x", 20)}, nil // {"S":"xxxxxxxxxxxxxxxxxxxx"} is 28 bytes
		})
		for i := 1; i <= 4; i++ {
			f.send(t, `{"ID":`+string(rune('0'+i))+`,"Name":"a"}`)
		}
		writes := f.writesCopy()
		var r struct {
			Value  big
			Recent []map[string]json.RawMessage
		}
		if err := json.Unmarshal(writes[len(writes)-1], &r); err != nil {
			t.Fatal(err)
		}
		for _, ref := range r.Recent {
			_, has := ref["Value"]
			valueKeys = append(valueKeys, has)
		}
		return valueKeys, r.Value
	}
	if keys, top := run(60); !reflect.DeepEqual(keys, []bool{false, false, true, true}) || len(top.S) != 20 {
		t.Errorf("budget 60 (two 28-byte values fit): has Value = %v, top = %+v", keys, top)
	}
	if keys, top := run(10); !reflect.DeepEqual(keys, []bool{false, false, false, false}) || len(top.S) != 20 {
		t.Errorf("budget 10 (none fits): has Value = %v, top = %+v", keys, top)
	}
	if keys, _ := run(-1); !reflect.DeepEqual(keys, []bool{false, false, false, false}) {
		t.Errorf("a negative budget keeps no values: %v", keys)
	}
	if keys, _ := run(0); !reflect.DeepEqual(keys, []bool{true, true, true, true}) {
		t.Errorf("the default budget keeps small values: %v", keys)
	}
}

func TestServeCallsMailboxSkipsHandledCallsAndRepeatedIDs(t *testing.T) {
	f := &fakeCalls{}
	runs := map[string]int{}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, func(name string, _ addArgs) (sum, error) { runs[name]++; return sum{}, nil })
	f.send(t, `[{"ID":1,"Name":"a"},{"ID":2,"Name":"b"}]`)
	// The writer rewrote the list: the old calls come again with a new one, and
	// one ID is listed twice.
	f.send(t, `[{"ID":1,"Name":"a"},{"ID":2,"Name":"b"},{"ID":3,"Name":"c"},{"ID":3,"Name":"c"}]`)
	f.send(t, `[]`)
	f.send(t, `[null,42,{"ID":"x"},{"ID":0}]`)
	want := map[string]int{"a": 1, "b": 1, "c": 1}
	if !reflect.DeepEqual(runs, want) {
		t.Fatalf("runs = %v, want %v", runs, want)
	}
	if got := ids(f.allResults(t)); !reflect.DeepEqual(got, []uint64{1, 2, 3}) {
		t.Fatalf("result IDs = %v", got)
	}
}

func TestServeCallsMailboxAnswersABadCallAndRunsTheRest(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	f.send(t, `[{"ID":1,"Name":42},{"ID":2,"Name":"add","Args":{"A":"x"}},{"ID":3,"Name":"add","Args":{"A":2,"B":2}}]`)
	rs := f.allResults(t)
	if len(rs) != 3 || !strings.HasPrefix(rs[0].Error, "bad-call:") || !strings.HasPrefix(rs[1].Error, "bad-args:") || rs[2].Error != "" || rs[2].Value.Total != 4 {
		t.Fatalf("results = %+v", rs)
	}
}

// A mailbox that was in the store before the engine subscribed replays whole.
func TestServeCallsReplaysAMailboxWrittenBeforeSubscribe(t *testing.T) {
	f := &fakeCalls{initial: json.RawMessage(`[{"ID":3,"Name":"add","Args":{"A":1,"B":1}},{"ID":4,"Name":"add","Args":{"A":2,"B":2}},{"ID":5,"Name":"add","Args":{"A":3,"B":3}}]`),
		resultInitial: json.RawMessage(`{"ID":3}`)}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	rs := f.allResults(t)
	if !reflect.DeepEqual(ids(rs), []uint64{4, 5}) || rs[1].Value.Total != 6 {
		t.Fatalf("a remount runs the calls above the answered ID once: %+v", rs)
	}
}

type trimRecorder struct {
	*fakeCalls
	trims []uint64
}

func (r *trimRecorder) TrimCalls(signal string, through uint64) error {
	if signal != "$c" {
		panic("trimmed " + signal)
	}
	r.trims = append(r.trims, through)
	return nil
}

// After each reply the engine asks a transport that can trim to drop the
// answered calls from the mailbox.
func TestServeCallsTrimsTheMailboxAfterEachReply(t *testing.T) {
	tr := &trimRecorder{fakeCalls: &fakeCalls{}}
	_, _ = ServeCallsOn(tr, "c", CallOptions{}, adder)
	tr.send(t, `[{"ID":1,"Name":"add"},{"ID":2,"Name":"add"}]`)
	tr.send(t, `{"ID":3,"Name":"add"}`)
	if !reflect.DeepEqual(tr.trims, []uint64{1, 2, 3}) {
		t.Fatalf("trims = %v", tr.trims)
	}
}

// A channel whose writer sends one call at a time never holds a mailbox, so the
// engine leaves it alone and the page store is not read or written for it.
func TestServeCallsNeverTrimsAChannelThatHoldsSingleCalls(t *testing.T) {
	tr := &trimRecorder{fakeCalls: &fakeCalls{}}
	_, _ = ServeCallsOn(tr, "c", CallOptions{}, adder)
	tr.send(t, `{"ID":1,"Name":"add"}`)
	tr.send(t, `{"ID":2,"Name":"add"}`)
	if len(tr.trims) != 0 {
		t.Fatalf("trims = %v, want none", tr.trims)
	}
}

// A write that reaches the engine's own subscription before it returns (on the
// main thread, a result notifies the subscribers of the page store in the same
// call stack) must not run a later call ahead of an earlier one.
func TestServeCallsNestedDeliveryKeepsResultsInOrder(t *testing.T) {
	f := &fakeCalls{}
	var order []uint64
	_, _ = ServeCallsOn(f, "c", CallOptions{}, func(name string, _ addArgs) (sum, error) {
		order = append(order, uint64(len(name)))
		return sum{}, nil
	})
	nested := false
	f.onWrite = func() {
		if !nested {
			nested = true
			// The page's store notified a subscriber that wrote the next call.
			f.handler(json.RawMessage(`{"ID":9,"Name":"nine"}`))
		}
	}
	f.send(t, `[{"ID":5,"Name":"five"},{"ID":6,"Name":"six"}]`)
	rs := f.allResults(t)
	if !reflect.DeepEqual(ids(rs), []uint64{5, 6, 9}) {
		t.Fatalf("result IDs = %v, want [5 6 9]", ids(rs))
	}
	if !reflect.DeepEqual(order, []uint64{4, 3, 4}) {
		t.Fatalf("handler order = %v", order)
	}
}
