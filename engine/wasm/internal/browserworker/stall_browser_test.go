//go:build !js || !wasm

package browserworker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// A page whose main thread is busy for longer than a channel's timeout is slow,
// not silent. The async server of a worker engine writes the mark of a call and
// starts the handler only after the page reports the mark. The page cannot take
// the mark, or report it, while its main thread is busy.
//
// The test writes a call and then keeps the page's main thread busy for 800 ms,
// well past the channel's 300 ms timeout. The page's watchdog fires when the page
// is free again, and the engine must still be waiting: it runs the call late,
// once, and its real reply replaces the page's "timeout". An engine that gave up
// the wait at the channel's timeout would answer "not-started" and never run the
// call, which a stall of this length would then cause.
//
// The counts come from the server the handlers call, so they hold however the
// engine and the page interleave.
func TestMainThreadStallLongerThanTheCallTimeoutStillRunsAnAsyncCall(t *testing.T) {
	dir := assets(t)
	fx := newEffectServer()
	ts := serve(t, dir, func(*http.Request) string {
		return callsPageFor(t, dir, "worker", `{"scenario":"stall"}`)
	}, fx.routes)
	state := driveScript(t, ts.URL+"/", fmt.Sprintf(callsDone, "done"), 60*time.Second, "scripts/stall.mjs")
	o := state.observed(t)
	t.Logf("states: %v", o.States)
	calls, _ := json.Marshal(state.Calls)
	t.Logf("calls: %s", calls)

	if !containsInOrder(o.States, []string{"running/worker/"}) {
		t.Fatalf("status history %v does not show the engine running in the worker", o.States)
	}
	var result struct {
		ID    uint64
		Name  string
		Error string
		Value struct {
			Total  int
			Thread string
		}
	}
	if err := json.Unmarshal(o.step("result"), &result); err != nil {
		t.Fatalf("step result: %v (%s)", err, o.step("result"))
	}
	if result.ID != 2 || result.Error != "" || result.Value.Total != 42 || result.Value.Thread != "worker" {
		t.Errorf("the last result is %+v, want the real reply of call 2 (42, from the worker); an error here means the engine gave up on the call while the page was only busy", result)
	}
	if got := fx.count("control"); got != 1 {
		t.Errorf("the control call ran %d times, want exactly once", got)
	}
	if got := fx.count("order"); got != 1 {
		t.Errorf("call 2 (order) ran %d times, want exactly once, late", got)
	}
}
