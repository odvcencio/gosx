//go:build !js || !wasm

package browserworker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/bridge"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/engine/host"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
)

// These tests run an island in the real island runtime (client/bridge) and an
// engine behind host.ServeCalls, and carry each shared signal the island writes
// to the engine the way the page does: the page store keeps the newest value of
// a signal and tells its subscribers once when a handler ends. They need no
// browser, so they run on every `go test`. The tests in calls_browser_test.go
// run the same island in Chromium.

// compileIsland compiles one island from GSX source and returns its program.
func compileIsland(t *testing.T, source string) (*program.Program, []byte) {
	t.Helper()
	irProg, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile the island: %v", err)
	}
	irProg.Dir = t.TempDir()
	irProg.PackagePath = "example.com/fixture"
	for i, c := range irProg.Components {
		if !c.IsIsland {
			continue
		}
		prog, err := ir.LowerIsland(irProg, i)
		if err != nil {
			t.Fatalf("lower the island: %v", err)
		}
		data, err := program.EncodeJSON(prog)
		if err != nil {
			t.Fatal(err)
		}
		return prog, data
	}
	t.Fatal("no island in the source")
	return nil, nil
}

// pageIsland is an island hydrated in the island runtime, with a record of the
// shared-signal notifications the runtime sends to the page.
type pageIsland struct {
	t      *testing.T
	b      *bridge.Bridge
	prog   *program.Program
	notify []signalNotice
}

type signalNotice struct{ name, json string }

func hydrate(t *testing.T, source, component string) *pageIsland {
	t.Helper()
	prog, data := compileIsland(t, source)
	p := &pageIsland{t: t, b: bridge.New(), prog: prog}
	p.b.SetSharedSignalCallback(func(name, valueJSON string) {
		p.notify = append(p.notify, signalNotice{name, valueJSON})
	})
	if err := p.b.HydrateIsland("island", component, `{}`, data, "json"); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	p.notify = nil // the hydration baseline is not under test
	return p
}

// click runs a handler and returns the notifications the page received for
// signal while it ran. The island runtime sends the newest value of a signal
// when the handler ends, and may send it more than once, so the engine must
// ignore a value it has handled. The writes the handler made before its last
// one are never sent.
func (p *pageIsland) click(handler, signal string) []string {
	p.t.Helper()
	p.notify = nil
	if _, err := p.b.DispatchAction("island", handler, `{}`); err != nil {
		p.t.Fatalf("dispatch %s: %v", handler, err)
	}
	var values []string
	for _, n := range p.notify {
		if n.name == signal {
			values = append(values, n.json)
		}
	}
	return values
}

// text returns the text of the element with the given id, with tags removed.
func (p *pageIsland) text(id string) string {
	p.t.Helper()
	reconciler, ok := p.b.LookupReconciler("island")
	if !ok {
		p.t.Fatal("island not found")
	}
	html := island.RenderResolvedHTML(p.prog, reconciler.(*vm.Island).CurrentTree())
	m := regexp.MustCompile(`(?s)<p id="` + id + `"[^>]*>(.*?)</p>`).FindStringSubmatch(html)
	if m == nil {
		p.t.Fatalf("no #%s in %s", id, html)
	}
	return regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m[1], "")
}

// engineSide is the page end of a call channel as the engine sees it: a
// transport that hands the engine each value the page store notifies, and
// records the results the engine writes.
type engineSide struct {
	mu      sync.Mutex
	handler func(json.RawMessage)
	onReply func(json.RawMessage) // the engine's subscription to the result signal
	onMark  func(json.RawMessage) // the engine's subscription to the started signal
	results []json.RawMessage
}

// deliver hands the engine every notification, in order.
func (e *engineSide) deliver(values []string) {
	for _, v := range values {
		e.handler(json.RawMessage(v))
	}
}

// deliverResult hands the engine a value of the result signal, as the page does
// when the page host or the engine itself wrote it.
func (e *engineSide) deliverResult(value string) { e.onReply(json.RawMessage(value)) }

func (e *engineSide) Subscribe(signal string, h func(json.RawMessage)) (host.HandleFunc, error) {
	switch {
	case strings.HasSuffix(signal, ".result"):
		e.onReply = h
	case strings.HasSuffix(signal, ".started"):
		e.onMark = h
	default:
		e.handler = h
	}
	return func() {}, nil
}

func (e *engineSide) Write(signal string, v any) error {
	if strings.HasSuffix(signal, ".started") {
		// The mark of a call whose handler starts (ServeCallsAsync); not a result. The
		// page's store tells the engine's subscription about it, which is how the
		// engine learns that the page took it.
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if e.onMark != nil {
			e.onMark(data)
		}
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.results = append(e.results, data)
	e.mu.Unlock()
	return nil
}

func (e *engineSide) Declare(string, time.Duration, int, string) {}

func (e *engineSide) written() []json.RawMessage {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]json.RawMessage(nil), e.results...)
}

type callArgs struct{ A, B int }
type callSum struct {
	Total  int
	Thread string
}

func addCalls(name string, a callArgs) (callSum, error) {
	return callSum{Total: a.A + a.B, Thread: "test"}, nil
}

// An island handler that writes two calls into a mailbox sends the page a value
// that lists both. The engine runs both, in ID order, and runs neither again
// when the page repeats the value. The island shows both replies.
func TestIslandHandlerWritesTwoCallsAndTheEngineRunsBoth(t *testing.T) {
	p := hydrate(t, panelSource, "Panel")
	sent := p.click("pair", "$bv.box")
	if len(sent) == 0 {
		t.Fatal("the page was not told about $bv.box")
	}
	for _, value := range sent {
		var mailbox []struct {
			ID   uint64
			Name string
			Args callArgs
		}
		if err := json.Unmarshal([]byte(value), &mailbox); err != nil || len(mailbox) != 2 ||
			mailbox[0].ID != 1 || mailbox[0].Args.B != 2 || mailbox[1].ID != 2 || mailbox[1].Args.B != 20 {
			t.Fatalf("a notification does not list both calls: %s (%v)", value, err)
		}
	}
	if got := p.text("pending"); got != "2" {
		t.Errorf("the island lists %s pending calls, want 2", got)
	}

	e := &engineSide{}
	if _, err := host.ServeCallsOn(e, "bv.box", host.CallOptions{Recent: 4}, addCalls); err != nil {
		t.Fatal(err)
	}
	e.deliver(sent)
	results := e.written()
	if len(results) != 2 {
		t.Fatalf("the engine wrote %d results for 2 calls: %s", len(results), results)
	}
	// The page store keeps the newest value of the result signal and tells its
	// subscribers each time it changes; the island sees every write.
	for _, r := range results {
		if err := p.b.SetSharedSignalJSON("$bv.box.result", string(r)); err != nil {
			t.Fatal(err)
		}
	}
	if got := p.text("box"); got != "2:30::test" {
		t.Errorf("newest reply = %q, want 2:30::test", got)
	}
	if got := p.text("recent"); got != "1=3/;2=30/;" {
		t.Errorf("replies in Recent = %q, want 1=3/;2=30/;", got)
	}
}

// The page keeps only the newest value of the result signal. If both results
// reach the island as one write, Recent still carries the first reply with its
// value, which is why the engine puts values there.
func TestIslandReadsBothRepliesFromTheNewestResultAlone(t *testing.T) {
	p := hydrate(t, panelSource, "Panel")
	sent := p.click("pair", "$bv.box")
	e := &engineSide{}
	if _, err := host.ServeCallsOn(e, "bv.box", host.CallOptions{Recent: 4}, addCalls); err != nil {
		t.Fatal(err)
	}
	e.deliver(sent)
	results := e.written()
	if len(results) != 2 {
		t.Fatalf("the engine wrote %d results for 2 calls: %s", len(results), results)
	}
	if err := p.b.SetSharedSignalJSON("$bv.box.result", string(results[1])); err != nil {
		t.Fatal(err)
	}
	if got := p.text("recent"); got != "1=3/;2=30/;" {
		t.Errorf("replies in Recent = %q, want 1=3/;2=30/;", got)
	}
}

// Calls that the island lists while earlier ones are still unanswered keep
// their order and their numbers across handlers.
func TestIslandNumbersMailboxCallsAboveTheLastReply(t *testing.T) {
	p := hydrate(t, panelSource, "Panel")
	sent := p.click("pair", "$bv.box")
	e := &engineSide{}
	_, _ = host.ServeCallsOn(e, "bv.box", host.CallOptions{Recent: 4}, addCalls)
	e.deliver(sent)
	results := e.written()
	if len(results) != 2 {
		t.Fatalf("the engine wrote %d results for 2 calls: %s", len(results), results)
	}
	if err := p.b.SetSharedSignalJSON("$bv.box.result", string(results[1])); err != nil {
		t.Fatal(err)
	}
	// The page drops answered calls from the mailbox (the page host does it for
	// a worker engine). The next handler then numbers its calls after the reply.
	if err := p.b.SetSharedSignalJSON("$bv.box", `[]`); err != nil {
		t.Fatal(err)
	}
	next := p.click("ordered", "$bv.box")
	var calls []struct{ ID uint64 }
	if err := json.Unmarshal([]byte(next[len(next)-1]), &calls); err != nil || len(calls) != 2 || calls[0].ID != 3 || calls[1].ID != 4 {
		t.Fatalf("the next calls are numbered %v (%v), want 3 and 4: %s", calls, err, next)
	}
}

// twiceSource is an island that follows the older rule: one signal that holds
// one call. Its handler writes two calls.
const twiceSource = `package fixture

import "m31labs.dev/gosx/signal"

type Cmd struct {
	ID   uint64
	Name string
}
type Ref struct {
	ID    uint64
	Error string
}
type Reply struct {
	ID     uint64
	Recent []Ref
}

//gosx:island
func Twice(props struct{}) Node {
	cmd := signal.NewShared("bv.one", Cmd{})
	res := signal.NewShared("bv.one.result", Reply{})
	both := func() {
		cmd.Set(Cmd{ID: cmd.Get().ID + 1, Name: "first"})
		cmd.Set(Cmd{ID: cmd.Get().ID + 1, Name: "second"})
	}
	return <section>
		<button id="both" onClick={both}>both</button>
		<p id="recent"><Each of={res.Get().Recent} as="r"><span>{r.ID}={r.Error};</span></Each></p>
	</section>
}
`

// A signal keeps its newest value, and the page tells its subscribers once when
// a handler ends. Two single calls written by one handler therefore reach the
// engine as the second call alone. This is the reason a handler that sends
// several calls writes a mailbox (see the test above and docs/engine-worker.md).
// The engine cannot run the first call, but it says that it was lost, in the
// result of the second, so the island does not wait for a reply that never comes.
func TestIslandHandlerThatWritesTwoSingleCallsSendsOnlyTheSecond(t *testing.T) {
	p := hydrate(t, twiceSource, "Twice")
	sent := p.click("both", "$bv.one")
	if len(sent) == 0 {
		t.Fatal("the page was not told about $bv.one")
	}
	for _, value := range sent {
		if strings.Contains(value, `"first"`) {
			t.Fatalf("the first write reached the page: %s", value)
		}
	}
	e := &engineSide{}
	var ran []string
	if _, err := host.ServeCallsOn(e, "bv.one", host.CallOptions{}, func(name string, _ callArgs) (callSum, error) {
		ran = append(ran, name)
		return callSum{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	e.deliver(sent)
	if fmt.Sprint(ran) != "[second]" {
		t.Fatalf("the engine ran %v; a signal that holds one call delivers only the newest", ran)
	}
	results := e.written()
	if len(results) != 1 {
		t.Fatalf("the engine wrote %d results: %s", len(results), results)
	}
	if err := p.b.SetSharedSignalJSON("$bv.one.result", string(results[0])); err != nil {
		t.Fatal(err)
	}
	if got := p.text("recent"); got != "1=overwritten;2=;" {
		t.Errorf("the island reads %q from Recent, want 1=overwritten;2=;", got)
	}
}

// The page times out the calls of a slow handler and writes a result of its
// own. The handler's real reply for call 1 comes later and puts the result's ID
// back to 1. The island must not number its next call from that ID: the engine
// and the page both ignore an ID at or below the highest they have seen, so a
// call that reuses 2 would never run and never time out, and the next reply
// with ID 2 would look like its answer. The island counts from Seen.
func TestIslandDoesNotReuseAnIDAfterATimeoutAndALateReply(t *testing.T) {
	p := hydrate(t, panelSource, "Panel")
	e := &engineSide{}
	started := make(chan string, 8)
	release := make(chan struct{}, 8)
	var mu sync.Mutex
	var ran []string
	if _, err := host.ServeCallsAsyncOn(e, "bv.box", host.CallOptions{Recent: 4}, func(name string, a callArgs) (callSum, error) {
		mu.Lock()
		ran = append(ran, name)
		mu.Unlock()
		started <- name
		<-release
		return callSum{Total: a.A + a.B, Thread: "test"}, nil
	}); err != nil {
		t.Fatal(err)
	}

	// One handler writes calls 1 and 2. The engine accepts both and starts call 1.
	e.deliver(p.click("pair", "$bv.box"))
	<-started

	// The page's watchdog times out both calls and drops them from the mailbox.
	// It reports the highest ID it watched in Seen.
	timeout := `{"ID":2,"Name":"add","Error":"timeout","Value":{"Total":0,"Thread":""},` +
		`"Recent":[{"ID":1,"Error":"timeout"},{"ID":2,"Error":"timeout"}],"Seen":2}`
	if err := p.b.SetSharedSignalJSON("$bv.box.result", timeout); err != nil {
		t.Fatal(err)
	}
	e.deliverResult(timeout)
	if err := p.b.SetSharedSignalJSON("$bv.box", `[]`); err != nil {
		t.Fatal(err)
	}

	// Call 1 finishes late. Its real reply replaces the timeout, so the result's
	// ID goes back to 1; the engine starts call 2 at once.
	release <- struct{}{}
	<-started
	late := e.written()[0]
	if err := p.b.SetSharedSignalJSON("$bv.box.result", string(late)); err != nil {
		t.Fatal(err)
	}
	e.deliverResult(string(late))
	if got := p.text("box"); !strings.HasPrefix(got, "1:3::") {
		t.Fatalf("the island shows %q after the late reply, want the reply to call 1", got)
	}

	// The island writes a new call. It must be numbered above 2, which both the
	// engine and the page already saw.
	next := p.click("wait", "$bv.box")
	if len(next) == 0 {
		t.Fatal("the island wrote no call")
	}
	var calls []struct {
		ID   uint64
		Name string
	}
	if err := json.Unmarshal([]byte(next[len(next)-1]), &calls); err != nil || len(calls) != 1 {
		t.Fatalf("the island wrote %s (%v)", next, err)
	}
	if calls[0].Name != "wait" || calls[0].ID != 3 {
		t.Fatalf("the new call is %+v, want ID 3: ID 2 was already seen", calls[0])
	}

	// The engine runs it after call 2.
	e.deliver(next)
	release <- struct{}{} // call 2
	<-started             // call 3 starts
	release <- struct{}{}
	within(t, "three replies", func() bool { return len(e.written()) == 3 })
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(ran) != "[add add wait]" {
		t.Fatalf("handlers ran %v, want [add add wait]", ran)
	}
}

// within fails the test unless cond becomes true soon.
func within(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// An island decodes Recent into typed records, and a missing Value key decodes
// as the zero value. The entry of a reply whose value did not fit the engine's
// byte budget therefore renders exactly like a successful reply whose value is
// zero. Kept is the key that tells them apart.
func TestIslandTellsALostReplyFromAReplyWhoseValueIsZero(t *testing.T) {
	p := hydrate(t, panelSource, "Panel")
	e := &engineSide{}
	// Each value, {"Total":N,"Thread":"test"}, is 28 bytes, so the budget holds two.
	if _, err := host.ServeCallsOn(e, "bv.box", host.CallOptions{Recent: 5, RecentBytes: 60}, func(name string, a callArgs) (callSum, error) {
		if name == "zero" {
			return callSum{}, nil
		}
		return callSum{Total: a.A + a.B, Thread: "test"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	e.handler(json.RawMessage(`[{"ID":1,"Name":"add","Args":{"A":1,"B":1}},{"ID":2,"Name":"add","Args":{"A":2,"B":2}},{"ID":3,"Name":"add","Args":{"A":3,"B":3}},{"ID":4,"Name":"zero"}]`))
	results := e.written()
	if len(results) != 4 {
		t.Fatalf("the engine wrote %d results for 4 calls", len(results))
	}
	if err := p.b.SetSharedSignalJSON("$bv.box.result", string(results[3])); err != nil {
		t.Fatal(err)
	}
	// Calls 1 and 2 lost their values (rendered as zero, like call 4's reply)...
	if got := p.text("recent"); got != "1=0/;2=0/;3=6/;4=0/;" {
		t.Errorf("replies in Recent = %q, want 1=0/;2=0/;3=6/;4=0/;", got)
	}
	// ...and Kept says which of them arrived.
	if got := p.text("kept"); got != "1:false;2:false;3:true;4:true;" {
		t.Errorf("Kept in Recent = %q, want 1:false;2:false;3:true;4:true;", got)
	}
}
