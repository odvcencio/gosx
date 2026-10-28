//go:build js && wasm

package jsreentry

import (
	"fmt"
	"strings"
	"sync"
	"syscall/js"
	"testing"
	"time"
)

// probeSource builds the JavaScript half. It wraps Go.prototype._resume, which
// wasm_exec.js calls for every entry into Go after the first run: timers, event
// listeners, microtask callbacks, and callbacks that JavaScript calls before a
// Go-to-JavaScript call returns. A _resume that starts while another _resume is
// still running is such a nested entry.
const probeSource = `
const Go = globalThis.Go;
if (typeof Go !== "function" || typeof Go.prototype._resume !== "function") {
	throw new Error("jsreentry: this host does not provide the Go class of wasm_exec.js");
}
const original = Go.prototype._resume;
const probe = { depth: 0, maxDepth: 0, entries: 0, reentries: [] };
Go.prototype._resume = function () {
	probe.depth++;
	probe.entries++;
	if (probe.depth > probe.maxDepth) probe.maxDepth = probe.depth;
	if (probe.depth > 1) {
		const limit = Error.stackTraceLimit;
		Error.stackTraceLimit = 80;
		const stack = new Error("nested entry at depth " + probe.depth).stack;
		Error.stackTraceLimit = limit;
		probe.reentries.push(String(stack));
	}
	try {
		return original.apply(this, arguments);
	} finally {
		probe.depth--;
	}
};
probe.uninstall = function () {
	if (Go.prototype._resume !== original) Go.prototype._resume = original;
};
return probe;
`

var (
	mu     sync.Mutex
	active *Probe
)

// Probe counts the calls from JavaScript into Go that started while another
// such call was in progress.
type Probe struct {
	state js.Value
}

// Install starts counting and stops when the test ends. Only one probe can be
// installed at a time.
//
// Install pauses the calling goroutine for a moment. The run that starts the
// program is not a call the probe can see, and code that is still inside it
// would hide every call nested in it. After the pause the goroutine runs inside
// a timer entry, which the probe counts as the outermost one.
func Install(t testing.TB) *Probe {
	t.Helper()
	mu.Lock()
	if active != nil {
		mu.Unlock()
		t.Fatal("jsreentry: a probe is already installed")
	}
	state, err := compile()
	if err != nil {
		mu.Unlock()
		t.Fatal(err)
	}
	p := &Probe{state: state}
	active = p
	mu.Unlock()
	t.Cleanup(p.uninstall)
	time.Sleep(time.Millisecond)
	return p
}

func compile() (state js.Value, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("jsreentry: install: %v", failure)
		}
	}()
	return js.Global().Get("Function").New(probeSource).Invoke(), nil
}

func (p *Probe) uninstall() {
	mu.Lock()
	defer mu.Unlock()
	if active == p {
		p.state.Call("uninstall")
		active = nil
	}
}

// Count returns how many calls from JavaScript into Go started while another
// was in progress since Install or the last Reset.
func (p *Probe) Count() int { return p.state.Get("reentries").Length() }

// Entries returns how many calls from JavaScript into Go the probe has seen,
// nested or not. A test can use it to confirm that the operation under test ran
// through JavaScript at all.
func (p *Probe) Entries() int { return p.state.Get("entries").Int() }

// MaxDepth returns the deepest nesting seen since Install. One means the calls
// never nested.
func (p *Probe) MaxDepth() int { return p.state.Get("maxDepth").Int() }

// Reset forgets the nested entries, the entry count and the deepest nesting seen
// so far. The depth of the entry that is running now stays counted.
func (p *Probe) Reset() {
	p.state.Set("reentries", js.Global().Get("Array").New())
	p.state.Set("maxDepth", p.state.Get("depth"))
	p.state.Set("entries", 0)
}

// Report describes each nested entry with the JavaScript and WebAssembly stack
// that led to it. When the test binary keeps the WebAssembly name section, the
// frames carry Go function names, and the first frames below the callback
// wrapper name the Go function that made the call into JavaScript.
func (p *Probe) Report() string {
	reentries := p.state.Get("reentries")
	var out strings.Builder
	for i := 0; i < reentries.Length(); i++ {
		fmt.Fprintf(&out, "nested entry %d of %d:\n", i+1, reentries.Length())
		for _, line := range strings.Split(reentries.Index(i).String(), "\n") {
			out.WriteString("    " + strings.TrimSpace(line) + "\n")
		}
	}
	return out.String()
}

// AssertNone fails the test if any nested entry was seen, and prints the stacks.
func (p *Probe) AssertNone(t testing.TB, format string, args ...any) {
	t.Helper()
	if n := p.Count(); n != 0 {
		t.Errorf("%s: %d call(s) from JavaScript into Go ran inside a call from Go into JavaScript\n%s",
			fmt.Sprintf(format, args...), n, p.Report())
	}
}
