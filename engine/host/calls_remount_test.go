package host

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pageStore is the page's store of shared signals as an engine sees it on the
// main thread: it keeps the newest value of each signal, tells a new subscriber
// the current value at once (JSON null when there is none), and tells every
// subscriber of a signal about each write. It outlives the engine that uses it,
// so a second engine can mount on the same signals.
type pageStore struct {
	mu     sync.Mutex
	values map[string]json.RawMessage
	subs   map[string][]func(json.RawMessage)
	writes []string // signal names, in order
	// results are the values written to result signals, in order.
	results []json.RawMessage
}

func newPageStore() *pageStore {
	return &pageStore{values: map[string]json.RawMessage{}, subs: map[string][]func(json.RawMessage){}}
}

func (p *pageStore) Subscribe(signal string, h func(json.RawMessage)) (HandleFunc, error) {
	p.mu.Lock()
	idx := len(p.subs[signal])
	p.subs[signal] = append(p.subs[signal], h)
	current, ok := p.values[signal]
	p.mu.Unlock()
	if !ok {
		current = json.RawMessage("null")
	}
	h(current)
	return func() { p.mu.Lock(); p.subs[signal][idx] = nil; p.mu.Unlock() }, nil
}

func (p *pageStore) Write(signal string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.values[signal] = data
	p.writes = append(p.writes, signal)
	if strings.HasSuffix(signal, ".result") {
		p.results = append(p.results, data)
	}
	subs := append([]func(json.RawMessage){}, p.subs[signal]...)
	p.mu.Unlock()
	for _, h := range subs {
		if h != nil {
			h(data)
		}
	}
	return nil
}

func (p *pageStore) Declare(string, time.Duration, int, string) {}

func (p *pageStore) value(signal string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.values[signal])
}

func (p *pageStore) wrote(signal string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, w := range p.writes {
		if w == signal {
			n++
		}
	}
	return n
}

func (p *pageStore) result(t *testing.T) (r struct {
	ID    uint64
	Name  string
	Error string
	Value sum
}) {
	t.Helper()
	raw := p.value("$bv.save.result")
	if raw == "" {
		return r // nothing written yet
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("result signal %q: %v", raw, err)
	}
	return r
}

// An engine is disposed while its handler runs, for example because the page
// replaces it. The browser revokes the engine's context before it calls Dispose,
// so nothing can be written then, and the handler's reply is dropped. A second
// engine mounts on the same shared signals and finds the call with no result.
// Running it again would repeat a side effect that may be done: a non-idempotent
// call, such as a save or an order, would happen twice, and the first run may
// still be going. The engine marks each call as started before its handler runs,
// so the second engine can tell.
func TestServeCallsAsyncDisposedWhileRunningIsNotRunAgainByTheNextEngine(t *testing.T) {
	page := newPageStore()
	var runs atomic.Int32
	started := make(chan string, 4)
	release := make(chan struct{})
	handler := func(name string, a addArgs) (sum, error) {
		runs.Add(1)
		started <- name
		if name == "save" {
			<-release // for example, a save that waits for storage
		}
		return sum{Total: a.A + a.B}, nil
	}

	first, err := ServeCallsAsyncOn(page, "bv.save", CallOptions{}, handler)
	if err != nil {
		t.Fatal(err)
	}
	// Call 1 runs; call 2 waits behind it and never starts.
	_ = page.Write("$bv.save", []map[string]any{{"ID": 1, "Name": "save"}, {"ID": 2, "Name": "order", "Args": map[string]int{"A": 20, "B": 22}}})
	<-started
	first.Dispose()

	second, err := ServeCallsAsyncOn(page, "bv.save", CallOptions{}, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Dispose()
	// The second engine answers call 1 without running it, and runs call 2.
	within(t, "the answer to call 2", func() bool { return page.result(t).ID == 2 })
	if r := page.result(t); r.Error != "" || r.Value.Total != 42 {
		t.Fatalf("result of call 2 = %+v", r)
	}
	close(release) // the first run finishes after it was disposed
	time.Sleep(30 * time.Millisecond)
	if n := runs.Load(); n != 2 {
		t.Fatalf("handlers started %d times; want 2 (call 1 once, call 2 once)", n)
	}
	// The first run's reply was dropped; the result is still call 2's.
	if r := page.result(t); r.ID != 2 {
		t.Fatalf("the dropped reply of the first run replaced the result: %+v", r)
	}
	// The answer to call 1 names the call, as any result does.
	var answer struct {
		ID    uint64
		Name  string
		Error string
	}
	page.mu.Lock()
	firstResult := page.results[0]
	page.mu.Unlock()
	if err := json.Unmarshal(firstResult, &answer); err != nil || answer.ID != 1 || answer.Name != "save" || answer.Error != "interrupted" {
		t.Fatalf("first result = %s (%v), want call 1 \"save\" interrupted", firstResult, err)
	}
	var seen struct {
		Recent []struct {
			ID    uint64
			Error string
		}
	}
	if err := json.Unmarshal([]byte(page.value("$bv.save.result")), &seen); err != nil {
		t.Fatal(err)
	}
	if len(seen.Recent) != 2 || seen.Recent[0].ID != 1 || seen.Recent[0].Error != "interrupted" || seen.Recent[1].ID != 2 || seen.Recent[1].Error != "" {
		t.Fatalf("Recent = %+v, want call 1 interrupted and call 2 answered", seen.Recent)
	}
}

// The mark must exist before the handler can have any effect.
func TestServeCallsAsyncMarksACallStartedBeforeItsHandlerRuns(t *testing.T) {
	page := newPageStore()
	var atHandler string
	var marks int
	_, err := ServeCallsAsyncOn(page, "bv.save", CallOptions{}, func(string, addArgs) (sum, error) {
		atHandler = page.value("$bv.save.started")
		marks = page.wrote("$bv.save.started")
		return sum{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = page.Write("$bv.save", map[string]any{"ID": 7, "Name": "save"})
	within(t, "the result", func() bool { return page.result(t).ID == 7 })
	if atHandler != "7" || marks != 1 {
		t.Fatalf("when the handler ran the started signal was %q after %d writes; want 7 after 1", atHandler, marks)
	}
}

// A call that finished leaves its mark behind, and a new engine must not treat
// it as interrupted.
func TestServeCallsAsyncRemountAfterAnAnsweredCallIsQuiet(t *testing.T) {
	page := newPageStore()
	var runs atomic.Int32
	h := func(string, addArgs) (sum, error) { runs.Add(1); return sum{Total: 5}, nil }
	first, _ := ServeCallsAsyncOn(page, "bv.save", CallOptions{}, h)
	_ = page.Write("$bv.save", map[string]any{"ID": 1, "Name": "save"})
	within(t, "the first result", func() bool { return page.result(t).ID == 1 })
	first.Dispose()
	before := page.wrote("$bv.save.result")

	second, _ := ServeCallsAsyncOn(page, "bv.save", CallOptions{}, h)
	defer second.Dispose()
	if got := page.wrote("$bv.save.result"); got != before || runs.Load() != 1 {
		t.Fatalf("remount wrote %d more results and ran %d handlers; want none and 1 in all", got-before, runs.Load())
	}
	_ = page.Write("$bv.save", map[string]any{"ID": 2, "Name": "save"})
	within(t, "call 2", func() bool { return page.result(t).ID == 2 })
	if r := page.result(t); r.Error != "" || runs.Load() != 2 {
		t.Fatalf("result = %+v after %d runs", r, runs.Load())
	}
}

// ServeCalls runs its handler inside the delivery callback, where nothing can
// dispose the engine under it, so it writes no mark.
func TestServeCallsWritesNoStartedMark(t *testing.T) {
	page := newPageStore()
	_, _ = ServeCallsOn(page, "bv.save", CallOptions{}, adder)
	_ = page.Write("$bv.save", map[string]any{"ID": 1, "Name": "add"})
	if page.wrote("$bv.save.result") != 1 || page.wrote("$bv.save.started") != 0 {
		t.Fatalf("writes = %v", page.writes)
	}
	if strings.Contains(strings.Join(page.writes, ","), "started") {
		t.Fatalf("writes = %v", page.writes)
	}
}
