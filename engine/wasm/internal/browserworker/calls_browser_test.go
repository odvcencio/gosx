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
	return <section>
		<p id="state">{status.Get().State}/{status.Get().Thread}/{status.Get().Reason}</p>
		<p id="tick">{view.Get().Tick}</p>
		<p id="result">{res.Get().ID}:{res.Get().Value.Total}:{res.Get().Error}:{res.Get().Value.Thread}</p>
		<button id="add" onClick={add} disabled={status.Get().State != "running"}>add</button>
		<button id="fail" onClick={fail} disabled={status.Get().State != "running"}>fail</button>
		<button id="slow" onClick={slow} disabled={status.Get().State != "running"}>slow</button>
		<button id="crash" onClick={crash} disabled={status.Get().State != "running"}>crash</button>
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

// ForceWorkerFallback runs the same Go code on the main thread: the same
// calls get the same answers, the status says so, and the view still paces.
func TestForcedFallbackRunsTheSameCallsOnTheMainThread(t *testing.T) {
	srv, _ := callsServer(t)
	state := driveScript(t, srv.url+"/?mode=fallback", fmt.Sprintf(callsDone, "fail"), 60*time.Second, "scripts/calls.mjs")
	o := state.observed(t)
	t.Logf("states: %v results: %v", o.States, o.Results)
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
