package wasm

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"m31labs.dev/gosx/engine"
)

// wireCall is what the client writes for one call.
type wireCall = engine.Call[json.RawMessage]

type sent struct {
	signal string
	call   wireCall
}

func newTestCaller() (*Caller, *[]sent) {
	var log []sent
	c := NewCaller("bv.cmd", func(signal string, v any) error {
		log = append(log, sent{signal, v.(wireCall)})
		return nil
	})
	return c, &log
}

// result builds the value of a result signal. recent lists IDs that the Recent
// list holds without a value.
func result(id uint64, errMsg string, value string, recent ...uint64) []byte {
	m := map[string]any{"ID": id, "Error": errMsg}
	if value != "" {
		m["Value"] = json.RawMessage(value)
	}
	var rs []map[string]any
	for _, r := range recent {
		rs = append(rs, map[string]any{"ID": r, "Error": ""})
	}
	m["Recent"] = rs
	b, _ := json.Marshal(m)
	return b
}

// The tests from here to TestCallerAFailedWriteDoesNotBlockTheChannel are the
// ones Bon Voyage wrote for its own client of the call channels, run against
// this one.

func TestCallerNumbersCallsFromOneAndKeepsOneInFlight(t *testing.T) {
	c, log := newTestCaller()
	c.Call("order", map[string]int{"kind": 22}, "a")
	c.Call("order", map[string]int{"kind": 23}, "b")
	if len(*log) != 1 || (*log)[0].call.ID != 1 || (*log)[0].signal != "$bv.cmd" || string((*log)[0].call.Args) != `{"kind":22}` {
		t.Fatalf("sent = %+v", *log)
	}
	if c.Pending() != 2 {
		t.Errorf("pending = %d", c.Pending())
	}
	reps := c.Result(result(1, "", `{"accepted":true}`))
	if len(reps) != 1 || reps[0].Tag != "a" || string(reps[0].Value) != `{"accepted":true}` || reps[0].Err != "" {
		t.Fatalf("reply = %+v", reps)
	}
	if len(*log) != 2 || (*log)[1].call.ID != 2 {
		t.Fatalf("the queued call was not sent: %+v", *log)
	}
	reps = c.Result(result(2, "", `{}`))
	if len(reps) != 1 || reps[0].Tag != "b" || c.Pending() != 0 {
		t.Fatalf("reply = %+v pending %d", reps, c.Pending())
	}
}

func TestCallerReportsTransportErrors(t *testing.T) {
	c, _ := newTestCaller()
	c.Call("order", 1, "a")
	reps := c.Result(result(1, "timeout", ""))
	if len(reps) != 1 || reps[0].Err != "timeout" || reps[0].Value != nil {
		t.Fatalf("reply = %+v", reps)
	}
}

func TestCallerIgnoresStaleAndRepeatedResults(t *testing.T) {
	c, _ := newTestCaller()
	c.Call("x", 1, "a")
	c.Call("x", 1, "b")
	if reps := c.Result(result(1, "", `1`)); len(reps) != 1 {
		t.Fatal("first result lost")
	}
	if reps := c.Result(result(1, "", `1`)); len(reps) != 0 {
		t.Error("a repeated result answered the second call")
	}
	if reps := c.Result([]byte("not json")); len(reps) != 0 {
		t.Error("garbage answered a call")
	}
	if reps := c.Result(result(2, "", `2`)); len(reps) != 1 || reps[0].Tag != "b" {
		t.Errorf("second reply = %+v", reps)
	}
}

func TestCallerTreatsANewerResultAsALostReply(t *testing.T) {
	c, _ := newTestCaller()
	c.Call("x", 1, "a")
	reps := c.Result(result(5, "", `5`, 1))
	if len(reps) != 1 || reps[0].Tag != "a" || reps[0].Err != "reply-lost" {
		t.Fatalf("reply = %+v", reps)
	}
}

func TestCallerFailAnswersEverythingPending(t *testing.T) {
	c, _ := newTestCaller()
	c.Call("x", 1, "a")
	c.Call("x", 1, "b")
	c.Call("x", 1, "c")
	reps := c.Fail("worker-crashed")
	if len(reps) != 3 || reps[0].Tag != "a" || reps[2].Tag != "c" || reps[1].Err != "worker-crashed" || c.Pending() != 0 {
		t.Fatalf("replies = %+v", reps)
	}
}

func TestCallerRejectsUnmarshalableArgs(t *testing.T) {
	c, log := newTestCaller()
	if _, err := c.Call("x", make(chan int), "a"); err == nil || len(*log) != 0 {
		t.Error("a channel was marshalled")
	}
}

func TestCallerAFailedWriteDoesNotBlockTheChannel(t *testing.T) {
	fail := true
	var sentIDs []uint64
	c := NewCaller("bv.cmd", func(signal string, v any) error {
		if fail {
			return errors.New("signal write refused")
		}
		sentIDs = append(sentIDs, v.(wireCall).ID)
		return nil
	})
	reps, err := c.Call("order", 1, "a")
	if err != nil || len(reps) != 1 || reps[0].Tag != "a" || reps[0].Err != "signal write refused" {
		t.Fatalf("a refused write = %+v, %v", reps, err)
	}
	if c.Pending() != 0 {
		t.Fatalf("the refused call still holds the channel: pending %d", c.Pending())
	}
	// Two calls queue behind a failure: the failure is answered and the next one still goes out.
	fail = false
	c.Call("order", 2, "b")
	c.Call("order", 3, "c")
	if len(sentIDs) != 1 || c.Pending() != 2 {
		t.Fatalf("after recovery: sent %v pending %d", sentIDs, c.Pending())
	}
	fail = true
	reps = c.Result(result(sentIDs[0], "", `1`))
	if len(reps) != 2 || reps[0].Tag != "b" || reps[1].Tag != "c" || reps[1].Err == "" {
		t.Fatalf("a failure while pumping the queue = %+v", reps)
	}
	if c.Pending() != 0 {
		t.Errorf("pending = %d", c.Pending())
	}
}

// What the client adds.

func TestCallerReadsTheValueOfALostReplyFromRecent(t *testing.T) {
	c, _ := newTestCaller()
	c.Call("x", 1, "a")
	newer := []byte(`{"ID":3,"Name":"x","Error":"","Value":{"n":3},"Recent":[{"ID":1,"Error":"","Value":{"n":1}},{"ID":2,"Error":"boom"},{"ID":3,"Error":"","Value":{"n":3}}]}`)
	reps := c.Result(newer)
	if len(reps) != 1 || reps[0].Err != "" || string(reps[0].Value) != `{"n":1}` {
		t.Fatalf("a reply that is still in Recent keeps its value: %+v", reps)
	}
	c.Call("x", 1, "b") // ID 2
	reps = c.Result(newer)
	if len(reps) != 1 || reps[0].Err != "boom" || reps[0].Value != nil {
		t.Fatalf("an error in Recent is the reply's error: %+v", reps)
	}
}

func TestCallerSayingNothingOfARecentReplyIsALostReply(t *testing.T) {
	c, _ := newTestCaller()
	c.Call("x", 1, "a")
	// The reply is in Recent, but its value did not fit the engine's budget.
	reps := c.Result([]byte(`{"ID":2,"Recent":[{"ID":1,"Error":""},{"ID":2,"Error":""}]}`))
	if len(reps) != 1 || reps[0].Err != "reply-lost" || reps[0].Value != nil {
		t.Fatalf("reply = %+v", reps)
	}
}

func TestCallerNormalizesTheChannelName(t *testing.T) {
	for _, name := range []string{"bv.cmd", "$bv.cmd", " $bv.cmd "} {
		c := NewCaller(name, nil)
		if c.CallSignal() != "$bv.cmd" || c.ResultSignal() != "$bv.cmd.result" {
			t.Errorf("%q gives %q and %q", name, c.CallSignal(), c.ResultSignal())
		}
	}
	reps, err := NewCaller("c", nil).Call("x", 1, "a")
	if err != nil || len(reps) != 1 || reps[0].Err != "no-writer" {
		t.Errorf("a caller with no writer answers at once: %+v, %v", reps, err)
	}
}

func TestReplyDecodesItsValueOrReturnsItsError(t *testing.T) {
	type total struct{ Total int }
	got, err := DecodeReply[total](Reply{Value: []byte(`{"Total":5}`)})
	if err != nil || got.Total != 5 {
		t.Fatalf("decode = %+v, %v", got, err)
	}
	if _, err := DecodeReply[total](Reply{Err: "timeout"}); err == nil || err.Error() != "timeout" {
		t.Fatalf("a failed reply decodes to its error: %v", err)
	}
	if _, err := DecodeReply[total](Reply{Value: []byte(`"x"`)}); err == nil {
		t.Fatal("a value of the wrong type must not decode")
	}
	if got, err := DecodeReply[total](Reply{}); err != nil || got != (total{}) {
		t.Fatalf("a reply with no value decodes to the zero value: %+v, %v", got, err)
	}
}

// The engine ignores any ID at or below the last one it handled, so a client
// that starts after an engine which kept running must number from that ID.
func TestCallerNumbersCallsAfterTheGivenID(t *testing.T) {
	var ids []uint64
	c := NewCallerWith("c", func(_ string, v any) error {
		ids = append(ids, v.(wireCall).ID)
		return nil
	}, CallerOptions{After: 41})
	c.Call("x", 1, "a")
	c.Call("x", 1, "b")
	if reps := c.Result(result(42, "", `1`)); len(reps) != 1 || reps[0].Tag != "a" {
		t.Fatalf("reply to the first call = %+v", reps)
	}
	if !reflect.DeepEqual(ids, []uint64{42, 43}) {
		t.Fatalf("call IDs = %v, want [42 43]", ids)
	}
	if reps := c.Result(result(41, "", `1`)); len(reps) != 0 {
		t.Errorf("a result at the starting ID answered a call: %+v", reps)
	}
}

// Result.Seen is the highest call ID that the engine or the page has seen. A
// client that meets a result with a higher Seen than its own count (another
// writer used the channel, or the engine ran before the client) numbers its
// next call above it, because the engine ignores any ID at or below the highest
// it has seen.
func TestCallerNumbersAboveTheSeenOfAResult(t *testing.T) {
	c, log := newTestCaller()
	// No call is written yet, so the result answers nothing, but it still tells
	// the client where the numbering stands.
	if reps := c.Result([]byte(`{"ID":3,"Name":"x","Error":"","Value":null,"Recent":[],"Seen":7}`)); len(reps) != 0 {
		t.Fatalf("a result with no written call answered %+v", reps)
	}
	c.Call("x", 1, "a")
	if len(*log) != 1 || (*log)[0].call.ID != 8 {
		t.Fatalf("the first call is numbered %+v, want 8", *log)
	}
	// A result for the written call that reports a higher Seen moves the next call too.
	c.Call("x", 1, "b")
	if reps := c.Result([]byte(`{"ID":8,"Name":"x","Error":"","Value":1,"Recent":[],"Seen":20}`)); len(reps) != 1 || reps[0].Tag != "a" {
		t.Fatalf("reply = %+v", reps)
	}
	if len(*log) != 2 || (*log)[1].call.ID != 21 {
		t.Fatalf("the second call is numbered %+v, want 21", *log)
	}
}

// A result that reports a lower Seen than the client's count changes nothing.
func TestCallerKeepsItsCountWhenAResultReportsALowerSeen(t *testing.T) {
	c, log := newTestCaller()
	c.Call("x", 1, "a")
	c.Result([]byte(`{"ID":1,"Name":"x","Error":"","Value":1,"Recent":[],"Seen":0}`))
	c.Call("x", 1, "b")
	if len(*log) != 2 || (*log)[1].call.ID != 2 {
		t.Fatalf("calls = %+v, want IDs 1 and 2", *log)
	}
}

// A window larger than one writes the unanswered calls as a mailbox, so they
// reach the engine together and it need not wait for each reply.
func TestCallerWithAWindowWritesAMailboxOfEveryUnansweredCall(t *testing.T) {
	var writes [][]wireCall
	c := NewCallerWith("bv.cmd", func(signal string, v any) error {
		if signal != "$bv.cmd" {
			t.Errorf("signal = %q", signal)
		}
		writes = append(writes, v.([]wireCall))
		return nil
	}, CallerOptions{Window: 3})
	for i, tag := range []string{"a", "b", "c", "d", "e"} {
		if _, err := c.Call("x", i, tag); err != nil {
			t.Fatal(err)
		}
	}
	// a, b and c fill the window: three writes, each listing all calls so far.
	if len(writes) != 3 || len(writes[0]) != 1 || len(writes[2]) != 3 || writes[2][2].ID != 3 {
		t.Fatalf("writes = %+v", writes)
	}
	if c.Pending() != 5 {
		t.Fatalf("pending = %d", c.Pending())
	}
	// One result completes calls 1 and 2 at once; d and e take their places.
	reps := c.Result(result(2, "", `"two"`, 1, 2))
	tags := []string{}
	for _, r := range reps {
		tags = append(tags, r.Tag)
	}
	if !reflect.DeepEqual(tags, []string{"a", "b"}) || reps[1].Err != "" || string(reps[1].Value) != `"two"` || reps[0].Err != "reply-lost" {
		t.Fatalf("replies = %+v", reps)
	}
	last := writes[len(writes)-1]
	ids := []uint64{}
	for _, call := range last {
		ids = append(ids, call.ID)
	}
	if !reflect.DeepEqual(ids, []uint64{3, 4, 5}) {
		t.Fatalf("after the replies the mailbox lists the unanswered calls: %v", ids)
	}
}

func TestCallerWithAWindowFailsEveryCallOfAFailedWrite(t *testing.T) {
	refuse := false
	c := NewCallerWith("c", func(string, any) error {
		if refuse {
			return errors.New("refused")
		}
		return nil
	}, CallerOptions{Window: 2})
	c.Call("x", 1, "a")
	refuse = true
	reps, _ := c.Call("x", 1, "b")
	if len(reps) != 1 || reps[0].Tag != "b" || reps[0].Err != "refused" || c.Pending() != 1 {
		t.Fatalf("a refused write fails its calls only: %+v pending %d", reps, c.Pending())
	}
}

// A write to a shared signal notifies the engine's subscribers inside the
// write, so the result of a call can reach Result before Call returns. The
// client must hold no lock then and must not lose or repeat a reply.
func TestCallerAResultThatArrivesInsideTheWriteIsAnsweredOnce(t *testing.T) {
	var c *Caller
	var got []Reply
	var mu sync.Mutex
	c = NewCaller("c", func(signal string, v any) error {
		call := v.(wireCall)
		reps := c.Result(result(call.ID, "", `"ok"`)) // the engine answered synchronously
		mu.Lock()
		got = append(got, reps...)
		mu.Unlock()
		return nil
	})
	direct, err := c.Call("x", 1, "a")
	if err != nil {
		t.Fatal(err)
	}
	more, _ := c.Call("x", 2, "b")
	mu.Lock()
	defer mu.Unlock()
	if len(direct) != 0 || len(more) != 0 || len(got) != 2 || got[0].Tag != "a" || got[1].Tag != "b" || c.Pending() != 0 {
		t.Fatalf("direct=%+v more=%+v nested=%+v pending=%d", direct, more, got, c.Pending())
	}
}

func TestCallerIsSafeForConcurrentUse(t *testing.T) {
	var wmu sync.Mutex
	var written []uint64
	c := NewCallerWith("c", func(_ string, v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		for _, call := range v.([]wireCall) {
			if len(written) == 0 || call.ID > written[len(written)-1] {
				written = append(written, call.ID)
			}
		}
		return nil
	}, CallerOptions{Window: 4})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := c.Call("x", j, strings.Repeat("t", j)); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	wmu.Lock()
	top := written[len(written)-1]
	wmu.Unlock()
	if top > 160 {
		t.Fatalf("numbered %d calls, made 160", top)
	}
	// Answer everything that was written; every call gets exactly one reply.
	replies := 0
	for id := uint64(1); id <= 160; id++ {
		replies += len(c.Result(result(id, "", `1`)))
	}
	if replies != 160 || c.Pending() != 0 {
		t.Fatalf("replies = %d pending = %d", replies, c.Pending())
	}
}
