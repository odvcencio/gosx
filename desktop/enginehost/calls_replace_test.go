package enginehost

import (
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/desktop/bridge"
	"m31labs.dev/gosx/engine/host"
)

var asyncRequests atomic.Int64

// postAsync is post without the wait for the answer, for a goroutine other than
// the test's.
func (p *fakePage) postAsync(id string, msg map[string]any) {
	p.mu.Lock()
	session := p.sessions[id]
	p.mu.Unlock()
	raw, _ := json.Marshal(bridge.Envelope{
		Op: bridge.OpRequest, ID: "async-" + strconv.FormatInt(asyncRequests.Add(1), 10), Method: MethodPost,
		Payload: mustJSON(map[string]any{"id": id, "session": session, "msg": msg}),
	})
	_ = p.router.Dispatch(string(raw))
}

// reportMarks plays the page for the started signal of the channel "sim.cmd":
// when the host writes a mark, the page stores it and reports it to the engine's
// subscription sid, as a page does for every change of a signal that the engine
// subscribed to. ServeCallsAsync starts a handler only after this report. The
// reports stop when stop is called or the test ends.
func (p *fakePage) reportMarks(id string, sid uint64) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	stop = func() { once.Do(func() { close(done) }) }
	p.t.Cleanup(stop)
	go func() {
		reported := 0
		for {
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond):
			}
			marks := p.signalBatches(id, "$sim.cmd.started")
			for ; reported < len(marks); reported++ {
				p.postAsync(id, map[string]any{"t": "signal", "sid": sid, "name": "$sim.cmd.started", "json": marks[reported]})
			}
		}
	}()
	return stop
}

// The page attaches an engine again (a reload, or a second window) while a
// ServeCallsAsync handler runs. The host replaces the instance: the old one can
// write nothing more to the page, but it runs until its mailbox reaches the
// request to dispose it. The mailbox is busy then, so the old instance's server
// finishes its handler and goes on to the next call inside that gap.
//
// The next call must not start in the old instance. The page can take no mark
// from it, so the new instance would find the call with no result and no mark
// and run it, and the call would run twice.
func TestServeCallsAsyncReplacedWhileACallRunsNeverRunsTheNextCallTwice(t *testing.T) {
	p := newFakePage(t)
	h := newHost(t, p)
	var mu sync.Mutex
	runs := map[string]int{}
	entered := make(chan string, 8)
	release := make(chan struct{})
	gate := make(chan struct{})
	_ = h.Register("Sim", func(in *Instance) (host.Handle, error) {
		// A subscription that holds up the instance's mailbox when the page tells it
		// of a change, as any slow message handler does.
		block, err := in.CallTransport().Subscribe("$sim.block", func(json.RawMessage) { <-gate })
		if err != nil {
			return nil, err
		}
		calls, err := host.ServeCallsAsync(in, "sim.cmd", host.CallOptions{}, func(name string, a addArgs) (sum, error) {
			mu.Lock()
			runs[name]++
			mu.Unlock()
			entered <- name
			if name == "save" {
				<-release // a save that waits for storage
			}
			return sum{a.A + a.B}, nil
		})
		if err != nil {
			return nil, err
		}
		return host.HandleFunc(func() { calls.Dispose(); block.Dispose() }), nil
	})

	// Instance A.
	p.attach("e1", "Sim", false)
	mountEngine(t, p, "e1")
	subs := p.waitMsg("e1", "subscribe", 4)
	sids := map[string]uint64{}
	for _, m := range subs {
		sids[m.str("name")] = uint64(m.num("sid"))
	}
	stopReports := p.reportMarks("e1", sids["$sim.cmd.started"])
	mailbox := `[{"ID":1,"Name":"save","Args":{"A":1,"B":1}},{"ID":2,"Name":"order","Args":{"A":20,"B":22}}]`
	p.post("e1", map[string]any{"t": "signal", "sid": sids["$sim.cmd"], "name": "$sim.cmd", "json": mailbox})
	if got := <-entered; got != "save" {
		t.Fatalf("first handler = %q", got)
	}
	// Hold up A's mailbox, then attach again. The host swaps the instances at
	// once, and queues the request to dispose A behind the held message.
	p.post("e1", map[string]any{"t": "signal", "sid": sids["$sim.block"], "name": "$sim.block", "json": "1"})
	stopReports()
	p.attach("e1", "Sim", false)
	oldResults := p.countSignals("e1", "$sim.cmd.result")

	// A's handler returns inside the gap. A can write nothing now, and must not
	// start call 2.
	close(release)
	select {
	case name := <-entered:
		t.Fatalf("the replaced instance started %q; the page took no mark for it, so the next instance runs it too", name)
	case <-time.After(80 * time.Millisecond):
	}
	if got := p.countSignals("e1", "$sim.cmd.result"); got != oldResults {
		t.Fatalf("the replaced instance wrote %d results", got-oldResults)
	}
	close(gate) // A's mailbox goes on: it disposes the instance

	// Instance B. The page replays what its store holds: the mark of call 1 (A's
	// reply was lost) and the mailbox.
	p.post("e1", map[string]any{"t": "mount"})
	p.waitMsg("e1", "mounted", 2)
	subsB := p.waitMsg("e1", "subscribe", 8)[4:]
	sidsB := map[string]uint64{}
	for _, m := range subsB {
		sidsB[m.str("name")] = uint64(m.num("sid"))
	}
	p.reportMarks("e1", sidsB["$sim.cmd.started"])
	p.post("e1", map[string]any{"t": "signal", "sid": sidsB["$sim.cmd.started"], "name": "$sim.cmd.started", "json": "1", "initial": true})
	p.post("e1", map[string]any{"t": "signal", "sid": sidsB["$sim.cmd"], "name": "$sim.cmd", "json": mailbox, "initial": true})
	results := p.results(oldResults + 2)[oldResults:]
	if results[0].ID != 1 || results[0].Error != "interrupted" || results[1].ID != 2 || results[1].Error != "" || results[1].Value.Total != 42 {
		t.Fatalf("results of the new instance = %+v, want call 1 interrupted and call 2 answered", results)
	}
	time.Sleep(30 * time.Millisecond) // a second run of call 2, if any, would show by now
	mu.Lock()
	defer mu.Unlock()
	if runs["save"] != 1 || runs["order"] != 1 {
		t.Fatalf("handlers ran %v; each call must run exactly once across the two instances", runs)
	}
}
