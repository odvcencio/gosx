package host

import (
	"encoding/json"
	"testing"
)

// Result.Seen is the number a writer counts its next call from. Result.ID is
// not: a reply that comes late, after the page timed out a newer call, puts ID
// back to an earlier call.

// A result carries the highest call ID the engine has accepted, also when the
// call it answers has a lower ID.
func TestResultSeenIsTheHighestCallIDTheEngineAccepted(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	f.send(t, `[{"ID":1,"Name":"add"},{"ID":3,"Name":"add"}]`)
	rs := f.allResults(t)
	if len(rs) != 2 {
		t.Fatalf("results = %+v", rs)
	}
	for _, r := range rs {
		if r.Seen != 3 {
			t.Errorf("the result for call %d has Seen %d, want 3", r.ID, r.Seen)
		}
	}
}

// The page writes a result of its own when it times a call out. The engine's
// later reply must not take Seen below it, or a writer would reuse an ID that
// the page already watched.
func TestResultSeenDoesNotFallBelowAResultThePageWrote(t *testing.T) {
	f := &fakeCalls{}
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	_, _ = ServeCallsAsyncOn(f, "c", CallOptions{}, func(name string, a addArgs) (sum, error) {
		entered <- struct{}{}
		<-release
		return sum{Total: a.A + a.B}, nil
	})
	f.send(t, `{"ID":1,"Name":"add","Args":{"A":1,"B":1}}`)
	<-entered
	// The page watched calls 2 to 5 as well, which the engine has not received yet,
	// and timed them all out. Its result reaches the engine's subscription.
	f.results(json.RawMessage(`{"ID":5,"Name":"add","Error":"timeout","Value":{"Total":0},"Recent":[],"Seen":5}`))
	close(release)
	within(t, "the late reply", func() bool { return f.writeCount() == 1 })
	r := f.allResults(t)[0]
	if r.ID != 1 || r.Value.Total != 2 {
		t.Fatalf("late reply = %+v", r)
	}
	if r.Seen != 5 {
		t.Errorf("the late reply has Seen %d, want 5: it must not go below the page's result", r.Seen)
	}
}

// A page that watched calls without the engine hearing of them may write Seen
// without raising ID. The engine takes it either way.
func TestResultSeenFollowsTheSeenOfAResultThePageWrote(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	f.results(json.RawMessage(`{"ID":2,"Name":"add","Value":{"Total":0},"Seen":9}`))
	f.send(t, `{"ID":3,"Name":"add"}`)
	if r := f.allResults(t)[0]; r.ID != 3 || r.Seen != 9 {
		t.Fatalf("result = %+v, want ID 3 and Seen 9", r)
	}
}

// An engine that mounts again finds the last result in the store. Its first
// result must continue from the Seen of that one.
func TestResultSeenSurvivesARemount(t *testing.T) {
	f := &fakeCalls{
		resultInitial: json.RawMessage(`{"ID":3,"Name":"add","Value":{"Total":2},"Seen":7}`),
		initial:       json.RawMessage(`{"ID":4,"Name":"add","Args":{"A":1,"B":1}}`),
	}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	if r := f.allResults(t)[0]; r.ID != 4 || r.Seen != 7 {
		t.Fatalf("result = %+v, want ID 4 and Seen 7", r)
	}
}

// A result that replaces another because the page cannot take the first one
// keeps the same Seen.
func TestResultSeenIsKeptByTheErrorResultThatReplacesAFailedWrite(t *testing.T) {
	f := &failFirstWrite{fakeCalls: &fakeCalls{}}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	f.fakeCalls.send(t, `{"ID":4,"Name":"add"}`)
	rs := f.fakeCalls.allResults(t)
	if len(rs) != 1 || rs[0].Seen != 4 || rs[0].Error == "" {
		t.Fatalf("results = %+v", rs)
	}
}

// failFirstWrite refuses the first write, as a page that cannot take a result.
type failFirstWrite struct {
	*fakeCalls
	failed bool
}

func (f *failFirstWrite) Write(signal string, v any) error {
	if !f.failed {
		f.failed = true
		return errString("page refused")
	}
	return f.fakeCalls.Write(signal, v)
}

func (f *failFirstWrite) Subscribe(signal string, h func(json.RawMessage)) (HandleFunc, error) {
	return f.fakeCalls.Subscribe(signal, h)
}
