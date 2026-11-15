//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestDocsRoomsDemoPlaysAFullRoundWithABot opens /demos/rooms on the docs app
// and watches the embedded Go/WASM client play one whole room lifecycle against
// the site's own room server: countdown, run (one person and one bot), and
// results. The round is 30 seconds, so the test takes about 45.
func TestDocsRoomsDemoPlaysAFullRoundWithABot(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t)))
	page := newBrowserPage(t, chrome, nil, 1440, 900, "", 5*time.Minute)
	if status := page.navigate(t, app.baseURL+"/demos/rooms"); status != 200 {
		t.Fatalf("/demos/rooms status = %d\n%s", status, app.logs.String())
	}

	type state struct {
		Status string   `json:"status"`
		Phase  string   `json:"phase"`
		Local  bool     `json:"local"`
		Seat   int      `json:"seat"`
		Code   string   `json:"code"`
		Seats  []string `json:"seats"`
	}
	read := func() (state, bool) {
		var s state
		err := page.tryEval(`(() => {
  const f = document.querySelector('iframe.rooms__client');
  const w = f && f.contentWindow;
  return w && typeof w.gosxRoomsState === 'function' ? w.gosxRoomsState() : null;
})()`, &s)
		return s, err == nil && s.Status != ""
	}

	seen := map[string]bool{}
	var order []string
	var running state
	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		s, ok := read()
		// Until the room's match starts the client plays a local match and only
		// reports the room (code, phase, seats); count the phase either way.
		if ok && s.Status == "online" && s.Code != "" {
			if !seen[s.Phase] {
				seen[s.Phase] = true
				order = append(order, s.Phase)
			}
			if s.Phase == "running" && !s.Local && running.Code == "" {
				running = s
			}
			if s.Phase == "results" {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !seen["results"] {
		t.Fatalf("round did not reach results; phases seen %v\nlogs:\n%s\nconsole:\n%s", order, app.logs.String(), page.Console())
	}
	if !seen["countdown"] || !seen["running"] {
		t.Fatalf("phases seen %v, want countdown, running, results in order", order)
	}
	for i, want := range []string{"countdown", "running", "results"} {
		if got := phaseAfter(order, i, want); !got {
			t.Fatalf("phase order %v, want countdown, running, results", order)
		}
	}
	humans, bots := 0, 0
	for _, kind := range running.Seats {
		switch kind {
		case "human":
			humans++
		case "bot":
			bots++
		}
	}
	if running.Status != "online" || len(running.Code) != 5 || humans != 1 || bots != 1 {
		t.Fatalf("running state %+v, want an online room with one person and one bot", running)
	}
	if errs := page.PageErrors(); len(errs) != 0 {
		t.Fatalf("page errors: %v", errs)
	}
	// The wasm loads only on this page: no other route may have asked for it.
	if !page.anyRequest(func(u string) bool { return strings.Contains(u, "/demos/rooms/client/main.wasm") }) {
		t.Fatalf("the rooms client wasm was never requested")
	}
}

// phaseAfter reports whether want is the i-th phase of order, ignoring a
// leading lobby phase.
func phaseAfter(order []string, i int, want string) bool {
	if len(order) > 0 && order[0] == "lobby" {
		order = order[1:]
	}
	return i < len(order) && order[i] == want
}
