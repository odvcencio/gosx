package perf

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// RunREPL starts an interactive GoSX browser console.
// It reads commands from stdin and dispatches them against the Driver.
func RunREPL(d *Driver, currentURL string) error {
	scanner := bufio.NewScanner(os.Stdin)
	var recorder *Recorder

	fmt.Println("gosx repl — connected to " + currentURL)
	fmt.Println("type 'help' for commands, 'exit' to quit")
	fmt.Println()

	for {
		fmt.Print("gosx> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := parts[0]
		args := parts[1:]

		switch cmd {
		case "exit", "quit":
			if recorder != nil {
				fmt.Println("stopping active recording...")
				recorder.Stop(d, "repl-recording.gif")
			}
			return nil

		case "help":
			printREPLHelp(os.Stdout)

		case "islands":
			replIslands(d)

		case "engines":
			replEngines(d)

		case "signals":
			replSignals(d)

		case "dispatch":
			replDispatch(d, args)

		case "scene":
			replScene(d)

		case "profile":
			replProfile(d, args)

		case "scroll":
			replScroll(d, args)

		case "click":
			replClick(d, args)

		case "type":
			replType(d, args)

		case "navigate":
			if len(args) == 0 {
				fmt.Println("usage: navigate <url>")
				continue
			}
			currentURL = args[0]
			replNavigate(d, currentURL)

		case "record":
			recorder = replRecord(d, recorder, args)

		case "perf":
			replPerf(d, currentURL)

		case "eval":
			replEval(d, strings.TrimPrefix(line, "eval "))

		case "heap":
			replHeap(d)

		default:
			fmt.Printf("unknown command: %s (type 'help')\n", cmd)
		}
	}
	return scanner.Err()
}

func printREPLHelp(w io.Writer) {
	fmt.Fprintln(w, `Commands:
  islands              List active islands
  engines              List active engines
  signals              Show shared signal values
  dispatch <id> <h> [json]  Dispatch action to island
  scene                Scene3D object summary
  profile [duration]   Profile frame timing (default 3s)
  scroll <px>          Scroll page by N pixels
  click <selector>     Click element
  type <sel> <text>    Type into element
  navigate <url>       Navigate to URL
  record start|stop [path]  Screen recording
  perf                 Full page performance report
  eval <js>            Evaluate JavaScript expression
  heap                 Show JS heap size
  help                 Show this help
  exit                 Quit REPL`)
}

func replIslands(d *Driver) {
	st, err := QueryRuntimeState(d)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("islands: %d\n", st.IslandCount)
	var raw string
	err = d.Evaluate(`JSON.stringify(
		Array.from((window.__gosx && window.__gosx.islands) ? window.__gosx.islands.entries() : [])
			.map(function(e){ return {id: e[0]} })
	)`, &raw)
	if err != nil || raw == "" {
		return
	}
	var islands []struct{ ID string `json:"id"` }
	if json.Unmarshal([]byte(raw), &islands) == nil {
		for _, isl := range islands {
			fmt.Printf("  %s\n", isl.ID)
		}
	}
}

func replEngines(d *Driver) {
	st, err := QueryRuntimeState(d)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Printf("engines: %d\n", st.EngineCount)
	var raw string
	err = d.Evaluate(`JSON.stringify(
		Array.from((window.__gosx && window.__gosx.engines) ? window.__gosx.engines.entries() : [])
			.map(function(e){ return {id: e[0]} })
	)`, &raw)
	if err != nil || raw == "" {
		return
	}
	var engines []struct{ ID string `json:"id"` }
	if json.Unmarshal([]byte(raw), &engines) == nil {
		for _, eng := range engines {
			fmt.Printf("  %s\n", eng.ID)
		}
	}
}

func replSignals(d *Driver) {
	var raw string
	err := d.Evaluate(`JSON.stringify(
		Object.fromEntries(
			Object.entries(window.__gosx && window.__gosx.sharedSignals || {})
				.map(function(e){ return [e[0], e[1].value] })
		)
	)`, &raw)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if raw == "" || raw == "{}" {
		fmt.Println("no shared signals")
		return
	}
	var signals map[string]interface{}
	if json.Unmarshal([]byte(raw), &signals) == nil {
		for k, v := range signals {
			fmt.Printf("  %s = %v\n", k, v)
		}
	}
}

func replDispatch(d *Driver, args []string) {
	if len(args) < 2 {
		fmt.Println("usage: dispatch <islandID> <handler> [json-data]")
		return
	}
	islandID := args[0]
	handler := args[1]
	data := "null"
	if len(args) >= 3 {
		data = strings.Join(args[2:], " ")
	}
	_ = d.Evaluate(`window.__gosx_perf && (window.__gosx_perf.dispatchLog = [])`, nil)
	js := fmt.Sprintf(`window.__gosx_action(%s, %s, %s)`,
		jsString(islandID), jsString(handler), data)
	if err := d.Evaluate(js, nil); err != nil {
		fmt.Printf("dispatch error: %v\n", err)
		return
	}
	time.Sleep(200 * time.Millisecond)
	dispatches, err := QueryDispatchLog(d)
	if err != nil {
		fmt.Printf("error reading dispatch log: %v\n", err)
		return
	}
	if len(dispatches) == 0 {
		fmt.Println("dispatched (no log entries)")
		return
	}
	for _, dl := range dispatches {
		fmt.Printf("  %s:%s  %.1fms  %d patches\n", dl.Island, dl.Handler, dl.Ms, dl.Patches)
	}
}

func replScene(d *Driver) {
	var raw string
	err := d.Evaluate(`(function(){
		if (!window.__gosx || !window.__gosx.engines) return "";
		var result = [];
		window.__gosx.engines.forEach(function(v, k){
			if (v && v.scene) {
				var s = v.scene;
				result.push({
					id: k,
					objects: s.children ? s.children.length : 0,
					type: v.type || "unknown"
				});
			}
		});
		return JSON.stringify(result);
	})()`, &raw)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if raw == "" || raw == "[]" {
		fmt.Println("no Scene3D engines detected")
		return
	}
	var scenes []struct {
		ID      string `json:"id"`
		Objects int    `json:"objects"`
		Type    string `json:"type"`
	}
	if json.Unmarshal([]byte(raw), &scenes) == nil {
		for _, s := range scenes {
			fmt.Printf("  engine %s (%s): %d scene objects\n", s.ID, s.Type, s.Objects)
		}
	}
}

func replProfile(d *Driver, args []string) {
	dur := 3 * time.Second
	if len(args) > 0 {
		if parsed, err := time.ParseDuration(args[0]); err == nil {
			dur = parsed
		}
	}
	fmt.Printf("profiling for %s...\n", dur)
	time.Sleep(dur)
	entries, err := QuerySceneFrames(d)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if len(entries) == 0 {
		fmt.Println("no scene3d frames recorded")
		return
	}
	durations := make([]float64, len(entries))
	for i, e := range entries {
		durations[i] = e.Duration
	}
	fs := ComputeFrameStats(durations)
	fmt.Printf("  frames: %d\n", fs.Count)
	fmt.Printf("  p50    %.1fms\n", fs.P50)
	fmt.Printf("  p95    %.1fms\n", fs.P95)
	fmt.Printf("  p99    %.1fms\n", fs.P99)
	fmt.Printf("  max    %.1fms\n", fs.Max)
	fmt.Printf("  mean   %.1fms\n", fs.Mean)
}

func replScroll(d *Driver, args []string) {
	if len(args) == 0 {
		fmt.Println("usage: scroll <pixels>")
		return
	}
	px, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Printf("invalid pixel value: %s\n", args[0])
		return
	}
	if err := Scroll(d, px); err != nil {
		fmt.Printf("scroll error: %v\n", err)
		return
	}
	fmt.Printf("scrolled %dpx\n", px)
}

func replClick(d *Driver, args []string) {
	if len(args) == 0 {
		fmt.Println("usage: click <selector>")
		return
	}
	sel := strings.Join(args, " ")
	if err := Click(d, sel); err != nil {
		fmt.Printf("click error: %v\n", err)
		return
	}
	time.Sleep(200 * time.Millisecond)
	dispatches, _ := QueryDispatchLog(d)
	if len(dispatches) > 0 {
		for _, dl := range dispatches {
			fmt.Printf("  dispatch: %s:%s  %.1fms  %d patches\n", dl.Island, dl.Handler, dl.Ms, dl.Patches)
		}
	} else {
		fmt.Println("clicked (no dispatches)")
	}
}

func replType(d *Driver, args []string) {
	if len(args) < 2 {
		fmt.Println("usage: type <selector> <text>")
		return
	}
	sel := args[0]
	text := strings.Join(args[1:], " ")
	if err := Type(d, sel, text); err != nil {
		fmt.Printf("type error: %v\n", err)
		return
	}
	time.Sleep(200 * time.Millisecond)
	dispatches, _ := QueryDispatchLog(d)
	if len(dispatches) > 0 {
		for _, dl := range dispatches {
			fmt.Printf("  dispatch: %s:%s  %.1fms  %d patches\n", dl.Island, dl.Handler, dl.Ms, dl.Patches)
		}
	} else {
		fmt.Printf("typed %q into %s\n", text, sel)
	}
}

func replNavigate(d *Driver, url string) {
	if err := d.Navigate(url); err != nil {
		fmt.Printf("navigate error: %v\n", err)
		return
	}
	if err := d.WaitReady(); err != nil {
		fmt.Printf("warning: wait ready: %v\n", err)
	}
	time.Sleep(300 * time.Millisecond)
	nav, err := QueryNavigationTiming(d)
	if err != nil {
		fmt.Printf("navigated (timing unavailable: %v)\n", err)
		return
	}
	fmt.Printf("navigated to %s\n", url)
	fmt.Printf("  TTFB             %.1fms\n", nav.TTFB)
	fmt.Printf("  DOMContentLoaded %.1fms\n", nav.DOMContentLoaded)
	fmt.Printf("  FullyLoaded      %.1fms\n", nav.FullyLoaded)
}

func replRecord(d *Driver, recorder *Recorder, args []string) *Recorder {
	if len(args) == 0 {
		fmt.Println("usage: record start|stop [path]")
		return recorder
	}
	switch args[0] {
	case "start":
		if recorder != nil {
			fmt.Println("recording already active")
			return recorder
		}
		rec, err := StartRecording(d)
		if err != nil {
			fmt.Printf("record start error: %v\n", err)
			return nil
		}
		fmt.Println("recording started")
		return rec
	case "stop":
		if recorder == nil {
			fmt.Println("no active recording")
			return nil
		}
		path := "repl-recording.gif"
		if len(args) > 1 {
			path = args[1]
		}
		if err := recorder.Stop(d, path); err != nil {
			fmt.Printf("record stop error: %v\n", err)
			return nil
		}
		fmt.Printf("recording saved to %s\n", path)
		return nil
	default:
		fmt.Println("usage: record start|stop [path]")
		return recorder
	}
}

func replPerf(d *Driver, url string) {
	fmt.Println("collecting page report...")
	page, err := CollectPageReport(d, url)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	report := &Report{
		URL:        url,
		Timestamp:  time.Now(),
		PageReport: *page,
	}
	fmt.Print(FormatTable(report))
}

func replEval(d *Driver, expr string) {
	var result interface{}
	if err := d.Evaluate(expr, &result); err != nil {
		fmt.Printf("eval error: %v\n", err)
		return
	}
	if result == nil {
		fmt.Println("undefined")
		return
	}
	fmt.Printf("%v\n", result)
}

func replHeap(d *Driver) {
	mb, err := QueryHeapSize(d)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	if mb == 0 {
		fmt.Println("heap size unavailable (performance.memory not supported)")
		return
	}
	fmt.Printf("JS heap: %.1f MB\n", mb)
}
