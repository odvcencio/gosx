package host

import (
	"encoding/json"
	"reflect"
	"testing"
)

// A signal that holds one call keeps only its newest value. When a writer sets
// two calls in one batch, the engine receives the second and never hears of the
// first. The engine cannot run it, but it can say that it was lost, in Recent of
// the next result, so the writer does not wait for a reply that never comes.

type recentEntry struct {
	ID    uint64
	Error string
	Kept  bool
}

func recentOf(t *testing.T, raw json.RawMessage) []recentEntry {
	t.Helper()
	var r struct{ Recent []recentEntry }
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r.Recent
}

func TestServeCallsReportsASingleCallThatAWriteOverwrote(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{Recent: 8}, adder)
	f.send(t, `{"ID":1,"Name":"add"}`)
	// The writer set calls 2 and 3 in one batch: only 3 arrives.
	f.send(t, `{"ID":3,"Name":"add","Args":{"A":1,"B":2}}`)
	writes := f.writesCopy()
	got := recentOf(t, writes[len(writes)-1])
	want := []recentEntry{{ID: 1, Kept: true}, {ID: 2, Error: "overwritten"}, {ID: 3, Kept: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Recent = %+v, want %+v", got, want)
	}
	if r := f.allResults(t); r[len(r)-1].ID != 3 || r[len(r)-1].Value.Total != 3 {
		t.Fatalf("the call that arrived did not run: %+v", r)
	}
}

// The first call a writer sends can be the one that lost its predecessor.
func TestServeCallsReportsACallLostBeforeTheFirstOneThatArrived(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	f.send(t, `{"ID":2,"Name":"add"}`) // the writer set 1 and 2 in one batch
	got := recentOf(t, f.writesCopy()[0])
	want := []recentEntry{{ID: 1, Error: "overwritten"}, {ID: 2, Kept: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Recent = %+v, want %+v", got, want)
	}
}

// A mailbox lists every call, so a gap in it is not a loss.
func TestServeCallsDoesNotReportAGapInAMailbox(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	f.send(t, `[{"ID":1,"Name":"add"},{"ID":4,"Name":"add"}]`)
	for _, e := range recentOf(t, f.writesCopy()[1]) {
		if e.Error != "" {
			t.Fatalf("Recent has %+v; a mailbox lists every call it has", e)
		}
	}
}

// The page watched the calls that the engine has not received. They are not lost.
func TestServeCallsDoesNotReportACallThePageSaw(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{}, adder)
	f.send(t, `{"ID":1,"Name":"add"}`)
	f.results(json.RawMessage(`{"ID":3,"Name":"add","Error":"timeout","Value":{"Total":0},"Recent":[],"Seen":3}`))
	f.send(t, `{"ID":4,"Name":"add"}`)
	writes := f.writesCopy()
	for _, e := range recentOf(t, writes[len(writes)-1]) {
		if e.Error != "" {
			t.Fatalf("Recent has %+v; calls 2 and 3 were seen by the page", e)
		}
	}
}

// A jump of more than Recent places is a writer that numbers differently (a
// clock, a counter it kept), not a burst of overwritten calls.
func TestServeCallsDoesNotReportAJumpLongerThanRecent(t *testing.T) {
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{Recent: 3}, adder)
	f.send(t, `{"ID":1,"Name":"add"}`)
	f.send(t, `{"ID":5,"Name":"add"}`) // 2, 3 and 4 skipped: three places, so reported
	f.send(t, `{"ID":100,"Name":"add"}`)
	writes := f.writesCopy()
	if got := recentOf(t, writes[1]); len(got) != 3 || got[0].ID != 3 || got[0].Error != "overwritten" || got[2].ID != 5 {
		t.Fatalf("Recent after the burst = %+v", got)
	}
	// Recent still holds calls 4 and 5 from the burst; the jump adds only call 100.
	var after []uint64
	for _, e := range recentOf(t, writes[2]) {
		after = append(after, e.ID)
	}
	if !reflect.DeepEqual(after, []uint64{4, 5, 100}) {
		t.Fatalf("Recent after the jump holds calls %v, want [4 5 100]", after)
	}
}
