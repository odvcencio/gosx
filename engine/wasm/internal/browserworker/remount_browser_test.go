//go:build !js || !wasm

package browserworker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// effectServer counts what the fixture's handlers do. Each handler does its
// work against it with a request, so the count is the truth about how often a
// call ran, in whichever engine ran it, even after the engine is gone.
type effectServer struct {
	mu      sync.Mutex
	counts  map[string]int
	aborted int
	open    chan struct{}
	once    sync.Once
}

func newEffectServer() *effectServer {
	return &effectServer{counts: map[string]int{}, open: make(chan struct{})}
}

func (e *effectServer) routes(mux *http.ServeMux) {
	mux.HandleFunc("/effect", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.counts[r.URL.Query().Get("name")]++
		e.mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	})
	// The gate holds a worker until the test opens it. A request that is dropped
	// first, because the worker ended, is counted.
	mux.HandleFunc("/gate", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.counts["gate"]++
		e.mu.Unlock()
		select {
		case <-e.open:
			_, _ = w.Write([]byte("open"))
		case <-r.Context().Done():
			e.mu.Lock()
			e.aborted++
			e.mu.Unlock()
		}
	})
	mux.HandleFunc("/probe/open", func(w http.ResponseWriter, r *http.Request) {
		e.once.Do(func() { close(e.open) })
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/probe/effects", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		out := map[string]int{"aborted": e.aborted}
		for name, n := range e.counts {
			out[name] = n
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
}

func (e *effectServer) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.counts[name]
}

func (e *effectServer) abortedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.aborted
}

// A page replaces a worker engine while an async call runs. The call blocks the
// worker (a synchronous request), so the worker cannot handle the page's request
// to dispose until the call goes on. The call then returns, and the engine's
// server turns to the next call inside that gap, where the page drops every
// write the engine makes. If the server marked that call and ran it, the page
// would drop the mark, and the engine that mounts next would find a call with no
// result and no mark, and run it again: an order would happen twice.
//
// The server therefore starts a handler only after the page has reported the
// call's mark, and the page reports nothing for an engine it has begun to
// dispose. The counts come from the server the handlers call, so they hold in
// whichever engine ran them.
func TestRemountDuringAnInFlightAsyncCallRunsEachCallOnce(t *testing.T) {
	dir := assets(t)
	fx := newEffectServer()
	ts := serve(t, dir, func(*http.Request) string {
		return callsPageFor(t, dir, "worker", `{"scenario":"remount"}`)
	}, fx.routes)
	state := driveScript(t, ts.URL+"/", fmt.Sprintf(callsDone, "done"), 90*time.Second, "scripts/remount.mjs")
	o := state.observed(t)
	t.Logf("states: %v", o.States)
	t.Logf("steps: %s", state.Calls)

	if n := fx.abortedCount(); n != 0 {
		t.Fatalf("the worker ended while it waited at the gate (%d requests dropped); the page's grace period ran out before the test opened the gate, so the gap was never exercised", n)
	}
	if !containsInOrder(o.States, []string{"running/worker/", "disposed/worker/", "running/worker/"}) {
		t.Fatalf("status history %v does not show the engine disposed and mounted again", o.States)
	}
	if got := fx.count("save"); got != 1 {
		t.Errorf("call 1 (save) ran %d times, want exactly once", got)
	}
	if got := fx.count("order"); got != 1 {
		t.Errorf("call 2 (order) ran %d times, want exactly once across the two engines", got)
	}

	var result struct {
		ID     uint64
		Error  string
		Value  struct{ Total int }
		Recent []struct {
			ID    uint64
			Error string
		}
	}
	if err := json.Unmarshal(o.step("result"), &result); err != nil {
		t.Fatalf("step result: %v (%s)", err, o.step("result"))
	}
	if result.ID != 2 || result.Error != "" || result.Value.Total != 42 {
		t.Errorf("the last result is %+v, want call 2 answered with 42", result)
	}
	// The page hears of call 1 once, as interrupted: its handler ran in the first
	// engine and the page dropped the reply.
	var calls struct {
		Replies []struct {
			ID    uint64
			Error string
		}
	}
	data, _ := json.Marshal(state.Calls)
	if err := json.Unmarshal(data, &calls); err != nil {
		t.Fatal(err)
	}
	interrupted := 0
	for _, r := range calls.Replies {
		if r.ID == 1 && r.Error == "interrupted" {
			interrupted++
		}
		if r.ID == 1 && r.Error == "" {
			t.Errorf("a reply to call 1 reached the page after the page disposed the engine: %+v", calls.Replies)
		}
	}
	if interrupted == 0 {
		t.Errorf("the page never heard that call 1 was interrupted: %+v", calls.Replies)
	}
}
