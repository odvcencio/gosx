//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx/game/kitui"
)

// Side-select screen with the keyboard and two simulated gamepads
// (navigator.getGamepads is stubbed). Keys and the mouse are real CDP input.
// The page program is e2e/testdata/sidesbrowser.

type sidesState struct {
	Seats []struct {
		ID    string
		Slot  int
		Prev  int
		Ready bool
		Focus int
		Kind  int
	}
	Fields    map[string]string
	Starts    int
	Backs     int
	Startable bool
	Changes   []string
	Players   map[string][]int
}

func startSidesServer(t *testing.T) string {
	t.Helper()
	root := e2eRepoRoot(t)
	dir := t.TempDir()
	wasm := filepath.Join(dir, "main.wasm")
	cmd := exec.Command("go", "build", "-o", wasm, "./e2e/testdata/sidesbrowser")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build sidesbrowser: %v\n%s", err, out)
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	goroot := strings.TrimSpace(string(out))
	var wasmExec string
	for _, rel := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} {
		if _, err := os.Stat(filepath.Join(goroot, rel)); err == nil {
			wasmExec = filepath.Join(goroot, rel)
			break
		}
	}
	if wasmExec == "" {
		t.Fatal("wasm_exec.js not found")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/main.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		http.ServeFile(w, r, wasm)
	})
	mux.HandleFunc("/wasm_exec.js", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, wasmExec) })
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><style>%s</style><script>%s</script>
<script>window.pads=[null,null,null];window.mkpad=(i)=>({index:i,id:'e2e pad '+i,mapping:'standard',buttons:Array.from({length:17},()=>({value:0})),axes:[0,0,0,0]});
navigator.getGamepads=()=>window.pads;
window.press=(i,b)=>{window.pads[i].buttons[b]={value:1}};window.release=(i,b)=>{window.pads[i].buttons[b]={value:0}};</script></head>
<body style="margin:0;padding:16px;background:#111"><div id="root"></div>
<script src="/wasm_exec.js"></script><script>const go=new Go();WebAssembly.instantiateStreaming(fetch('/main.wasm'),go.importObject).then(r=>go.run(r.instance)).catch(e=>{window.wasmError=String(e)});</script></body></html>`,
			kitui.CSS(), kitui.ScriptSource())
	})
	app := httptest.NewServer(mux)
	t.Cleanup(app.Close)
	if e2eWindowsRelay() {
		return e2eExposeToWindows(t, app.URL)
	}
	return app.URL
}

func (p *browserPage) sides(t *testing.T) sidesState {
	t.Helper()
	var raw string
	p.eval(t, `kit.frame()`, &raw)
	var st sidesState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return st
}

// padTap presses and releases a pad button across two polled frames.
func (p *browserPage) padTap(t *testing.T, pad, button int) {
	t.Helper()
	p.eval(t, fmt.Sprintf(`press(%d,%d)`, pad, button), nil)
	p.sides(t)
	p.eval(t, fmt.Sprintf(`release(%d,%d)`, pad, button), nil)
	p.sides(t)
}

func (p *browserPage) iconIn(t *testing.T, device string, side int) bool {
	t.Helper()
	var ok bool
	p.eval(t, fmt.Sprintf(`!!document.querySelector('[data-side="%d"] [data-device="%s"]')`, side, device), &ok)
	return ok
}

func (s sidesState) seat(id string) (slot int, ready bool, ok bool) {
	for _, st := range s.Seats {
		if st.ID == id {
			return st.Slot, st.Ready, true
		}
	}
	return 0, false, false
}

func TestSideSelectBrowser(t *testing.T) {
	base := startSidesServer(t)

	load := func(t *testing.T, url string) *browserPage {
		p := kitPage(t)
		p.navigate(t, url)
		p.waitFor(t, `window.kit !== undefined || window.wasmError !== undefined`, 60*time.Second, "wasm start")
		var failed string
		p.eval(t, `window.wasmError||''`, &failed)
		if failed != "" {
			t.Fatalf("wasm: %s", failed)
		}
		return p
	}

	t.Run("keyboard and two gamepads slide, ready up and start", func(t *testing.T) {
		p := load(t, base+"/app?mode=local")
		p.eval(t, `window.pads[0]=mkpad(0);window.pads[1]=mkpad(1)`, nil)
		st := p.sides(t)
		if len(st.Seats) != 3 {
			t.Fatalf("each connected device shows as an icon: %+v", st.Seats)
		}
		for _, id := range []string{"keyboard", "pad:0", "pad:1"} {
			if !p.iconIn(t, id, 1) {
				t.Fatalf("%s icon not in the middle column", id)
			}
		}
		// Keyboard slides left with a real key event, and the icon animates.
		p.run(t, keyDown("ArrowLeft", "ArrowLeft", 37), keyUp("ArrowLeft", "ArrowLeft", 37))
		if !p.iconIn(t, "keyboard", 0) {
			t.Fatal("keyboard icon should be under Home")
		}
		var from string
		var animating bool
		p.eval(t, `document.querySelector('[data-device="keyboard"]').dataset.fromSlot`, &from)
		p.eval(t, `document.querySelector('[data-device="keyboard"]').getAnimations().some(a=>a.animationName==='kit-slide')`, &animating)
		if from != "1" || !animating {
			t.Fatalf("slide animation: from=%q running=%v", from, animating)
		}
		// Pad 0: d-pad left; pad 1: left stick right.
		p.padTap(t, 0, 14)
		p.eval(t, `window.pads[1].axes=[1,0,0,0]`, nil)
		p.sides(t)
		p.eval(t, `window.pads[1].axes=[0,0,0,0]`, nil)
		st = p.sides(t)
		if s, _, _ := st.seat("pad:0"); s != -1 {
			t.Fatalf("pad 0 slot %d", s)
		}
		if s, _, _ := st.seat("pad:1"); s != 1 {
			t.Fatalf("pad 1 slot %d", s)
		}
		if !p.iconIn(t, "pad:0", 0) || !p.iconIn(t, "pad:1", 2) {
			t.Fatal("icons did not follow the seats")
		}
		// Router: Home players 0 and 2 (keyboard, pad 0), Away player 1 (pad 1).
		if fmt.Sprint(st.Players) != "map[keyboard:[0] pad:0:[2] pad:1:[1]]" {
			t.Fatalf("router assignment %v", st.Players)
		}
		// Ready up one by one; the start waits for everyone on a side.
		p.run(t, keyDown("Enter", "Enter", 13), keyUp("Enter", "Enter", 13))
		p.padTap(t, 0, 0)
		if st = p.sides(t); st.Starts != 0 || st.Startable {
			t.Fatal("started before pad 1 was ready")
		}
		p.padTap(t, 1, 0)
		st = p.sides(t)
		var startable string
		p.eval(t, `document.querySelector('.kit-sides').dataset.startable`, &startable)
		if st.Starts != 1 || startable != "true" {
			t.Fatalf("starts=%d startable=%q", st.Starts, startable)
		}
		var readyBadges int
		p.eval(t, `document.querySelectorAll('.kit-device-ready').length`, &readyBadges)
		if readyBadges != 3 {
			t.Fatalf("ready badges %d", readyBadges)
		}
		// Cancel on a pad un-readies it and withdraws the start.
		p.padTap(t, 1, 1)
		if st = p.sides(t); st.Startable {
			t.Fatal("cancel should withdraw the start")
		}
		// Unplug pad 1: its icon goes away.
		p.eval(t, `window.pads[1]=null`, nil)
		st = p.sides(t)
		if _, _, ok := st.seat("pad:1"); ok || p.iconIn(t, "pad:1", 2) {
			t.Fatal("unplugged pad still shown")
		}
	})

	t.Run("settings row by keyboard, gamepad and mouse", func(t *testing.T) {
		p := load(t, base+"/app?mode=local")
		p.eval(t, `window.pads[0]=mkpad(0)`, nil)
		p.sides(t)
		p.run(t, keyDown("ArrowDown", "ArrowDown", 40), keyUp("ArrowDown", "ArrowDown", 40))
		var focus string
		p.eval(t, `document.querySelector('.kit-sides-field[data-focus="true"]')?.dataset.field||''`, &focus)
		if focus != "quarter" {
			t.Fatalf("keyboard focus on %q", focus)
		}
		p.run(t, keyDown("ArrowRight", "ArrowRight", 39), keyUp("ArrowRight", "ArrowRight", 39))
		st := p.sides(t)
		if st.Fields["quarter"] != "10" {
			t.Fatalf("keyboard changed the field: %v", st.Fields)
		}
		if s, _, _ := st.seat("keyboard"); s != 0 {
			t.Fatal("Right on a field must not move the icon")
		}
		// The gamepad uses the same row: down twice to Difficulty, then right.
		p.padTap(t, 0, 13)
		p.padTap(t, 0, 13)
		p.padTap(t, 0, 15)
		if st = p.sides(t); st.Fields["level"] != "hard" {
			t.Fatalf("pad changed the field: %v", st.Fields)
		}
		var value string
		p.eval(t, `document.querySelector('[data-field="level"] .kit-sides-field-value').textContent`, &value)
		if value != "Hard" {
			t.Fatalf("rendered value %q", value)
		}
		// The mouse buttons work too.
		p.run(t, chromedp.Click(`[data-field="quarter"] [data-kit-value="quarter:-1"]`, chromedp.ByQuery))
		if st = p.sides(t); st.Fields["quarter"] != "5" {
			t.Fatalf("mouse: %v", st.Fields)
		}
		// Leaving the row with Cancel (Escape), then Escape again in the middle goes back.
		p.run(t, keyDown("Escape", "Escape", 27), keyUp("Escape", "Escape", 27))
		p.run(t, keyDown("Escape", "Escape", 27), keyUp("Escape", "Escape", 27))
		if st = p.sides(t); st.Backs != 1 {
			t.Fatalf("backs=%d", st.Backs)
		}
	})

	t.Run("online: remote seats appear as icons, guests cannot change settings", func(t *testing.T) {
		p := load(t, base+"/app?mode=online")
		st := p.sides(t)
		if !p.iconIn(t, "net:1", 2) {
			t.Fatal("remote seat missing from Away")
		}
		var kind, tag string
		p.eval(t, `document.querySelector('[data-device="net:1"]').dataset.kind`, &kind)
		p.eval(t, `document.querySelector('[data-device="net:1"] .kit-device-tag')?.textContent||''`, &tag)
		if kind != "remote" || tag != "Host" {
			t.Fatalf("remote icon kind=%q tag=%q", kind, tag)
		}
		var disabled int
		p.eval(t, `document.querySelectorAll('.kit-sides-field button:disabled').length`, &disabled)
		if disabled != 4 {
			t.Fatalf("settings buttons disabled: %d", disabled)
		}
		p.run(t, keyDown("ArrowDown", "ArrowDown", 40), keyUp("ArrowDown", "ArrowDown", 40))
		p.run(t, keyDown("ArrowRight", "ArrowRight", 39), keyUp("ArrowRight", "ArrowRight", 39))
		if st = p.sides(t); st.Fields["quarter"] != "5" {
			t.Fatalf("guest changed a host setting: %v", st.Fields)
		}
		// The game moves the remote seat (a network update): it slides, and readiness gates the start.
		p.run(t, keyDown("Escape", "Escape", 27), keyUp("Escape", "Escape", 27)) // leave the settings row
		p.run(t, keyDown("ArrowLeft", "ArrowLeft", 37), keyUp("ArrowLeft", "ArrowLeft", 37))
		p.run(t, keyDown("Enter", "Enter", 13), keyUp("Enter", "Enter", 13))
		p.eval(t, `kit.remote(JSON.stringify([{ID:'net:1',Name:'Ana',Slot:1,Host:true,Ready:true}]))`, nil)
		if st = p.sides(t); st.Starts != 1 {
			t.Fatalf("local ready + remote ready should start: %+v", st)
		}
		p.eval(t, `kit.remote(JSON.stringify([{ID:'net:1',Name:'Ana',Slot:0,Host:true}]))`, nil)
		var from string
		p.eval(t, `document.querySelector('[data-device="net:1"]').dataset.fromSlot`, &from)
		if from != "2" || !p.iconIn(t, "net:1", 1) {
			t.Fatalf("remote slide: from=%q", from)
		}
		if st = p.sides(t); st.Startable {
			t.Fatal("remote left the side, local alone on Home still needs an opponent")
		}
	})
}
