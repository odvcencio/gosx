//go:build !js || !wasm

package browserworker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
)

// panelSource is the island under test. It uses no JavaScript: it reads the
// worker's status, view and result signals and writes one call signal per
// click. Cmd, Reply and Ref repeat the field names of engine.Call, engine.Result
// and engine.ResultRef, because those names are the wire keys.
const panelSource = `package fixture

import (
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/signal"
)

type Args struct{ A, B int }
type Sum struct {
	Total  int
	Thread string
}
type Cmd struct {
	ID   uint64
	Name string
	Args Args
}
type Ref struct {
	ID    uint64
	Error string
}
type Reply struct {
	ID     uint64
	Name   string
	Error  string
	Value  Sum
	Recent []Ref
}
type BoxRef struct {
	ID    uint64
	Error string
	Kept  bool
	Value Sum
}
type BoxReply struct {
	ID     uint64
	Name   string
	Error  string
	Value  Sum
	Recent []BoxRef
	Seen   uint64
}
type View struct {
	Tick   int
	Thread string
}

//gosx:island
func Panel(props struct{}) Node {
	status := signal.NewShared("gosx.engine.gosx-engine-bv", engine.WorkerStatus{})
	view := signal.NewShared("bv.view", View{})
	cmd := signal.NewShared("bv.cmd", Cmd{})
	res := signal.NewShared("bv.cmd.result", Reply{})
	add := func() { cmd.Set(Cmd{ID: cmd.Get().ID + 1, Name: "add", Args: Args{A: 2, B: 3}}) }
	fail := func() { cmd.Set(Cmd{ID: cmd.Get().ID + 1, Name: "fail"}) }
	slow := func() { cmd.Set(Cmd{ID: cmd.Get().ID + 1, Name: "slow"}) }
	crash := func() { cmd.Set(Cmd{ID: cmd.Get().ID + 1, Name: "crash"}) }
	box := signal.NewShared("bv.box", []Cmd{})
	boxres := signal.NewShared("bv.box.result", BoxReply{})
	// bv.box is a mailbox: each call is appended, so one handler can write
	// several. A call is numbered above every call in the mailbox, or, when the
	// mailbox is empty, above Seen: the highest ID the engine or the page has
	// seen. Not above the result's ID, which goes back after a late reply. An
	// island handler cannot call a function that returns a value, so the number
	// is written out where a call is appended.
	pair := func() {
		if len(box.Get()) == 0 {
			box.Set(append(box.Get(), Cmd{ID: boxres.Get().Seen + 1, Name: "add", Args: Args{A: 1, B: 2}}))
		} else {
			box.Set(append(box.Get(), Cmd{ID: box.Get()[len(box.Get())-1].ID + 1, Name: "add", Args: Args{A: 1, B: 2}}))
		}
		box.Set(append(box.Get(), Cmd{ID: box.Get()[len(box.Get())-1].ID + 1, Name: "add", Args: Args{A: 10, B: 20}}))
	}
	wait := func() {
		if len(box.Get()) == 0 {
			box.Set(append(box.Get(), Cmd{ID: boxres.Get().Seen + 1, Name: "wait", Args: Args{A: 120, B: 1}}))
		} else {
			box.Set(append(box.Get(), Cmd{ID: box.Get()[len(box.Get())-1].ID + 1, Name: "wait", Args: Args{A: 120, B: 1}}))
		}
	}
	ordered := func() {
		if len(box.Get()) == 0 {
			box.Set(append(box.Get(), Cmd{ID: boxres.Get().Seen + 1, Name: "wait", Args: Args{A: 150, B: 100}}))
		} else {
			box.Set(append(box.Get(), Cmd{ID: box.Get()[len(box.Get())-1].ID + 1, Name: "wait", Args: Args{A: 150, B: 100}}))
		}
		box.Set(append(box.Get(), Cmd{ID: box.Get()[len(box.Get())-1].ID + 1, Name: "add", Args: Args{A: 5, B: 5}}))
	}
	// bv.late is a mailbox whose calls reply late: the page times out the first two
	// after 300 ms, and the engine answers them after 900 ms and 1300 ms. When the
	// first reply comes the result's ID is 1 again, and call 2 is still running.
	// The island numbers the call it writes then from Seen, not from that ID.
	late := signal.NewShared("bv.late", []Cmd{})
	lateres := signal.NewShared("bv.late.result", BoxReply{})
	latepair := func() {
		if len(late.Get()) == 0 {
			late.Set(append(late.Get(), Cmd{ID: lateres.Get().Seen + 1, Name: "wait", Args: Args{A: 900, B: 1}}))
		} else {
			late.Set(append(late.Get(), Cmd{ID: late.Get()[len(late.Get())-1].ID + 1, Name: "wait", Args: Args{A: 900, B: 1}}))
		}
		late.Set(append(late.Get(), Cmd{ID: late.Get()[len(late.Get())-1].ID + 1, Name: "wait", Args: Args{A: 400, B: 2}}))
	}
	latenext := func() {
		if len(late.Get()) == 0 {
			late.Set(append(late.Get(), Cmd{ID: lateres.Get().Seen + 1, Name: "add", Args: Args{A: 7, B: 8}}))
		} else {
			late.Set(append(late.Get(), Cmd{ID: late.Get()[len(late.Get())-1].ID + 1, Name: "add", Args: Args{A: 7, B: 8}}))
		}
	}
	return <section>
		<p id="state">{status.Get().State}/{status.Get().Thread}/{status.Get().Reason}</p>
		<p id="tick">{view.Get().Tick}</p>
		<p id="result">{res.Get().ID}:{res.Get().Value.Total}:{res.Get().Error}:{res.Get().Value.Thread}</p>
		<button id="add" onClick={add} disabled={status.Get().State != "running"}>add</button>
		<button id="fail" onClick={fail} disabled={status.Get().State != "running"}>fail</button>
		<button id="slow" onClick={slow} disabled={status.Get().State != "running"}>slow</button>
		<button id="crash" onClick={crash} disabled={status.Get().State != "running"}>crash</button>
		<p id="box">{boxres.Get().ID}:{boxres.Get().Value.Total}:{boxres.Get().Error}:{boxres.Get().Value.Thread}</p>
		<p id="recent"><Each of={boxres.Get().Recent} as="r"><span>{r.ID}={r.Value.Total}/{r.Error};</span></Each></p>
		<p id="kept"><Each of={boxres.Get().Recent} as="r"><span>{r.ID}:{r.Kept};</span></Each></p>
		<p id="pending">{len(box.Get())}</p>
		<p id="seen">{boxres.Get().Seen}</p>
		<button id="pair" onClick={pair} disabled={status.Get().State != "running"}>pair</button>
		<button id="wait" onClick={wait} disabled={status.Get().State != "running"}>wait</button>
		<button id="ordered" onClick={ordered} disabled={status.Get().State != "running"}>ordered</button>
		<p id="late">{lateres.Get().ID}:{lateres.Get().Value.Total}:{lateres.Get().Error}:{lateres.Get().Seen}</p>
		<button id="latepair" onClick={latepair} disabled={status.Get().State != "running"}>latepair</button>
		<button id="latenext" onClick={latenext} disabled={status.Get().State != "running"}>latenext</button>
	</section>
}
`

var (
	islandOnce sync.Once
	islandJSON []byte
	islandSize int64
	islandHash string
	islandErr  error
)

// islandRuntime builds the shared island runtime (client/wasm) and the Panel
// island program into dir, once, and returns the runtime's size and hash.
func islandRuntime(t *testing.T, dir string) (int64, string) {
	t.Helper()
	islandOnce.Do(func() {
		irProg, err := gosx.Compile([]byte(panelSource))
		if err != nil {
			islandErr = fmt.Errorf("compile the Panel island: %w", err)
			return
		}
		irProg.Dir = dir
		irProg.PackagePath = "example.com/fixture"
		var prog *program.Program
		for i, c := range irProg.Components {
			if c.IsIsland {
				if prog, err = ir.LowerIsland(irProg, i); err != nil {
					islandErr = fmt.Errorf("lower the Panel island: %w", err)
					return
				}
			}
		}
		if prog == nil {
			islandErr = fmt.Errorf("Panel island not found")
			return
		}
		if islandJSON, err = program.EncodeJSON(prog); err != nil {
			islandErr = err
			return
		}
		if err = os.WriteFile(filepath.Join(dir, "Panel.json"), islandJSON, 0o644); err != nil {
			islandErr = err
			return
		}
		cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "runtime.wasm"), "./client/wasm")
		cmd.Dir = repoRoot(t)
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off", "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			islandErr = fmt.Errorf("building the island runtime: %v\n%s", err, out)
			return
		}
		goroot, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			islandErr = err
			return
		}
		exec, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js"))
		if err != nil {
			islandErr = err
			return
		}
		if err = os.WriteFile(filepath.Join(dir, "wasm_exec.js"), exec, 0o644); err != nil {
			islandErr = err
			return
		}
		wasm, err := os.ReadFile(filepath.Join(dir, "runtime.wasm"))
		if err != nil {
			islandErr = err
			return
		}
		islandSize, islandHash = int64(len(wasm)), buildmanifest.ContentHash(wasm)
	})
	if islandErr != nil {
		t.Fatal(islandErr)
	}
	return islandSize, islandHash
}

// callsPage renders the Panel island next to the calls engine. mode is
// "worker" (default), "fallback" (ForceWorkerFallback) or "required"
// (WorkerRequired with ForceWorkerFallback).
func callsPage(t *testing.T, dir, mode string) string {
	t.Helper()
	cfg := engine.Config{
		Name: "WorkerFixture", Kind: engine.KindWorker, Runtime: engine.RuntimeGoWASM, Thread: engine.ThreadWorker,
		MountID: "bv", WASMPath: "/engines/fixture.wasm", Props: json.RawMessage(`{"scenario":"calls"}`),
	}
	switch mode {
	case "fallback":
		cfg.ForceWorkerFallback = true
	case "required":
		cfg.ForceWorkerFallback = true
		cfg.Worker = engine.WorkerRequired
	}
	size, hash := islandRuntime(t, dir)
	return page(t, pageOptions{
		engines:     []engine.Config{cfg},
		runtimeSize: size,
		runtimeHash: hash,
		islands: func(r *island.Renderer) gosx.Node {
			r.SetProgramAsset("Panel", "/gosx/islands/Panel.json", "json", "")
			prog, err := program.DecodeJSON(islandJSON)
			if err != nil {
				t.Fatal(err)
			}
			return r.RenderIslandFromProgram(prog, struct{}{})
		},
	})
}

type step struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type callsObservation struct {
	States  []string `json:"states"`
	Results []string `json:"results"`
	Renders int      `json:"renders"`
	Steps   []step   `json:"steps"`
}

func (s browserState) observed(t *testing.T) callsObservation {
	t.Helper()
	data, err := json.Marshal(s.Calls)
	if err != nil {
		t.Fatal(err)
	}
	var o callsObservation
	if err := json.Unmarshal(data, &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func (o callsObservation) step(name string) json.RawMessage {
	for _, s := range o.Steps {
		if s.Name == name {
			return s.Value
		}
	}
	return nil
}

func callsServer(t *testing.T) (*serverHandle, string) {
	t.Helper()
	dir := assets(t)
	ts := serve(t, dir, func(r *http.Request) string { return callsPage(t, dir, r.URL.Query().Get("mode")) })
	return &serverHandle{url: ts.URL}, dir
}

type serverHandle struct{ url string }

const callsDone = `(window.__calls && window.__calls.steps || []).some(s => s.name === "%s")`

// An island clicks a button and the worker's reply renders; the view stream
// updates the island at a paced rate; a call the worker answers too late fails
// with "timeout" in the island; a crashed worker fails the next call with
// "worker-crashed" and shows in the status.
func TestIslandCallsAWorkerAndSeesItsView(t *testing.T) {
	srv, _ := callsServer(t)
	state := driveScript(t, srv.url+"/?mode=worker", fmt.Sprintf(callsDone, "crash"), 90*time.Second, "scripts/calls.mjs")
	o := state.observed(t)
	t.Logf("states: %v", o.States)
	t.Logf("results: %v", o.Results)
	t.Logf("steps: %s", state.Calls)

	wantStates := []string{"running/worker/", "crashed/worker/worker-module-exited"}
	if !containsInOrder(o.States, wantStates) {
		t.Fatalf("status history %v does not contain %v in order", o.States, wantStates)
	}
	for _, want := range []string{"1:5::worker", "2:0:refused:", "3:0:timeout:", "3:-1::worker", "4:5::worker", "5:0:worker-crashed:"} {
		if !containsInOrder(o.Results, []string{want}) {
			t.Errorf("island never showed result %q; saw %v", want, o.Results)
		}
	}
	var pace struct {
		Renders  int `json:"renders"`
		LastTick int `json:"lastTick"`
	}
	if err := json.Unmarshal(o.step("pace"), &pace); err != nil {
		t.Fatal(err)
	}
	// The engine ticks every 10 ms (about 100 times in the one-second window);
	// MinInterval is 100 ms, so the island should render roughly ten times.
	if pace.Renders < 4 || pace.Renders > 25 {
		t.Errorf("island rendered the view %d times in 1 s, want about 10 (engine ticks every 10 ms)", pace.Renders)
	}
	checkMailbox(t, o, "worker")
	checkLateReplies(t, o)
	var crash struct {
		State       string `json:"state"`
		Result      string `json:"result"`
		AddDisabled bool   `json:"addDisabled"`
	}
	if err := json.Unmarshal(o.step("crash"), &crash); err != nil {
		t.Fatal(err)
	}
	if !crash.AddDisabled || !strings.HasPrefix(crash.State, "crashed/worker/") {
		t.Errorf("after a crash the island should disable its controls: %+v", crash)
	}
}

// checkMailbox checks the bv.box steps of scripts/calls.mjs. An island handler
// wrote two calls into a mailbox; the engine, which serves the channel on
// goroutines, ran both and the page got both replies, in call order, with the
// mailbox emptied again. thread is where the engine runs.
func checkMailbox(t *testing.T, o callsObservation, thread string) {
	t.Helper()
	type seen = [3]any // ID, Value.Total, Error
	var pair, promise, ordered struct {
		Box     string `json:"box"`
		Recent  string `json:"recent"`
		Pending string `json:"pending"`
		Seen    []seen `json:"seen"`
	}
	for name, into := range map[string]any{"pair": &pair, "promise": &promise, "ordered": &ordered} {
		if err := json.Unmarshal(o.step(name), into); err != nil {
			t.Fatalf("step %s: %v (%s)", name, err, o.step(name))
		}
	}
	ids := func(list []seen) []float64 {
		var out []float64
		for _, s := range list {
			out = append(out, s[0].(float64))
		}
		return out
	}
	// Two calls written by one handler: both replies reach the page, and the
	// island, which renders only the newest result, still shows both through Recent.
	if pair.Recent != "1=3/;2=30/;" || pair.Box != "2:30::"+thread || pair.Pending != "0" {
		t.Errorf("pair: %+v", pair)
	}
	if len(pair.Seen) != 2 || pair.Seen[0][0] != 1.0 || pair.Seen[0][1] != 3.0 || pair.Seen[1][0] != 2.0 || pair.Seen[1][1] != 30.0 {
		t.Errorf("the page store did not hold both replies: %v", pair.Seen)
	}
	// A handler that waits for a browser promise completes instead of hanging the engine.
	if promise.Box != "3:121::"+thread {
		t.Errorf("promise: %+v", promise)
	}
	// Call 4 waits 150 ms and call 5 is quick. The engine runs them in call
	// order, so the page gets the reply to 4 before the reply to 5.
	want := []float64{1, 2, 3, 4, 5}
	got := ids(ordered.Seen)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("replies reached the page in order %v, want %v", got, want)
	}
	if last := ordered.Seen[len(ordered.Seen)-1]; last[1] != 10.0 || ordered.Seen[3][1] != 250.0 {
		t.Errorf("ordered replies: %v", ordered.Seen)
	}
	if ordered.Recent != "2=30/;3=121/;4=250/;5=10/;" || ordered.Pending != "0" {
		t.Errorf("ordered: %+v", ordered)
	}
}

// checkLateReplies checks the bv.late steps of scripts/calls.mjs. The page timed
// out calls 1 and 2, which wait 900 ms and 400 ms. Call 1's late reply put the
// result's ID back to 1 while call 2 ran. The island then wrote a call, and
// counted its number from Seen: it got 3. Counted from the result's ID it would
// have been 2, which the engine and the page had already seen, and it would
// never have run. The engine ran call 3, and Seen never went down on the way.
func checkLateReplies(t *testing.T, o callsObservation) {
	t.Helper()
	var late struct {
		Final string `json:"final"`
		Seen  [][4]any
	}
	if err := json.Unmarshal(o.step("late"), &late); err != nil {
		t.Fatalf("step late: %v (%s)", err, o.step("late"))
	}
	if late.Final != "3:15::3" {
		t.Errorf("the island's last reply on bv.late is %q, want 3:15::3 (call 3 ran)", late.Final)
	}
	var ids []float64
	highest := 0.0
	for _, r := range late.Seen { // ID, Value.Total, Error, Seen
		id, _ := r[0].(float64)
		seen, _ := r[3].(float64)
		ids = append(ids, id)
		if seen < highest {
			t.Errorf("Seen went down from %v to %v in %v", highest, seen, late.Seen)
		}
		highest = seen
	}
	// The page times out calls 1 and 2 and reports the highest ID it watched, 2,
	// in both. The engine, busy in call 1, then answers all three calls late.
	if len(late.Seen) < 5 || late.Seen[0][0] != 1.0 || late.Seen[0][2] != "timeout" || late.Seen[0][3] != 2.0 ||
		late.Seen[1][0] != 2.0 || late.Seen[1][2] != "timeout" || late.Seen[1][3] != 2.0 {
		t.Errorf("results on bv.late = %v, want the timeouts of calls 1 and 2 first, both with Seen 2", late.Seen)
	}
	var replied []float64
	for _, r := range late.Seen {
		if r[2] == "" {
			replied = append(replied, r[0].(float64))
		}
	}
	if fmt.Sprint(replied) != "[1 2 3]" {
		t.Errorf("replies on bv.late came for calls %v (all results %v), want [1 2 3] in order", replied, ids)
	}
}

// ForceWorkerFallback runs the same Go code on the main thread: the same
// calls get the same answers, the status says so, and the view still paces.
func TestForcedFallbackRunsTheSameCallsOnTheMainThread(t *testing.T) {
	srv, _ := callsServer(t)
	state := driveScript(t, srv.url+"/?mode=fallback", fmt.Sprintf(callsDone, "ordered"), 60*time.Second, "scripts/calls.mjs")
	o := state.observed(t)
	t.Logf("states: %v results: %v", o.States, o.Results)
	checkMailbox(t, o, "main")
	if !containsInOrder(o.States, []string{"running/main/worker-forced-fallback"}) {
		t.Fatalf("status history %v", o.States)
	}
	for _, want := range []string{"1:5::main", "2:0:refused:"} {
		if !containsInOrder(o.Results, []string{want}) {
			t.Errorf("island never showed result %q; saw %v", want, o.Results)
		}
	}
	mounted := state.find("mounted")
	if mounted["thread"] != "" || mounted["hasDocument"] != true || mounted["fallback"] != "worker-forced-fallback" {
		t.Fatalf("engine did not fall back: %v", mounted)
	}
	var pace struct {
		Renders int `json:"renders"`
	}
	if err := json.Unmarshal(o.step("pace"), &pace); err != nil {
		t.Fatal(err)
	}
	if pace.Renders < 4 || pace.Renders > 25 {
		t.Errorf("main-thread view rendered %d times in 1 s, want about 10", pace.Renders)
	}
}

// WorkerRequired never runs the engine on the main thread.
func TestWorkerRequiredNeverRunsOnTheMainThread(t *testing.T) {
	srv, _ := callsServer(t)
	state := driveScript(t, srv.url+"/?mode=required", fmt.Sprintf(callsDone, "required-disabled"), 60*time.Second, "scripts/calls.mjs")
	o := state.observed(t)
	if !containsInOrder(o.States, []string{"unsupported//worker-forced-fallback"}) {
		t.Fatalf("status history %v", o.States)
	}
	if string(o.step("required-disabled")) != "true" {
		t.Fatalf("island controls must be disabled, got %s", o.step("required-disabled"))
	}
	if state.find("mounted") != nil {
		t.Fatalf("the engine ran although the worker was required: %v", state.find("mounted"))
	}
	if len(state.Events) != 1 || state.Events[0]["name"] != "worker-unsupported" {
		t.Fatalf("want one worker-unsupported event, got %v", state.Events)
	}
}

func containsInOrder(have, want []string) bool {
	i := 0
	for _, h := range have {
		if i < len(want) && h == want[i] {
			i++
		}
	}
	return i == len(want)
}
