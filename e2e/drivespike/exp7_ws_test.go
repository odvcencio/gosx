//go:build e2e && drivespike

package drivespike

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const wsPage = `<!doctype html><html><head><meta charset="utf-8"><script>
window.got = []; window.listener = 0; window.opened = false; window.between = 0;
var ws = new WebSocket(location.origin.replace("http", "ws") + "/ws");
ws.onopen = function () { opened = true; };
ws.onmessage = function (e) { var m = JSON.parse(e.data); got.push({ seq: m.seq, v: __drive.now(), stepping: __drive.pending().stepping }); if (!__drive.pending().stepping) between++; };
ws.addEventListener("message", function () { listener++; });
</script></head><body>ws</body></html>`

// Experiment 7: inbound WebSocket messages queue in the probe and are delivered only on a step.
func TestExp7WebSocket(t *testing.T) {
	res := newResults(t, "7")
	defer res.summary()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for seq := 1; seq <= 400; seq++ {
			if err := c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"seq":%d}`, seq))); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(time.Minute)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(wsPage))
	})
	url := serve(t, mux)
	b := startBrowser(t)
	tb := b.newTab(t, time.Minute)
	tb.addProbe(map[string]any{"wall": 1.7e12})
	tb.navigate(url + "/")
	if !tb.waitFor(`opened`, 10*time.Second) {
		t.Fatalf("socket never opened\n%s", tb.Console())
	}
	time.Sleep(500 * time.Millisecond)
	type pageState struct {
		Got      []wsGot
		Listener int
		Between  int
		Inbox    int
	}
	read := func() pageState {
		var s pageState
		tb.eval(`({got, listener, between, inbox: __drive.pending().inbox})`, &s)
		return s
	}
	s := read()
	res.check("no message handler runs in 500 ms of real time without a step", len(s.Got) == 0 && s.Listener == 0 && s.Inbox > 0, "got=%d listener=%d queued=%d", len(s.Got), s.Listener, s.Inbox)
	contiguous := func(s pageState) bool {
		for i, g := range s.Got {
			if g.Seq != i+1 {
				return false
			}
		}
		return true
	}
	queued := s.Inbox
	r := tb.frames(1)
	s = read()
	res.check("one frame step delivers every queued message, in order", len(s.Got) >= queued && contiguous(s) && s.Listener == len(s.Got) && r.Delivered == len(s.Got),
		"queuedBefore=%d delivered=%d got=%d listener=%d contiguous=%v", queued, r.Delivered, len(s.Got), s.Listener, contiguous(s))
	allAt := true
	for _, g := range s.Got {
		if g.V != frameAt(1) {
			allAt = false
		}
	}
	res.check("delivered messages see the step's virtual time", allAt, "first=%+v", firstRec(s.Got))
	for round := 0; round < 5; round++ {
		n := len(s.Got)
		time.Sleep(150 * time.Millisecond)
		s2 := read()
		res.check(fmt.Sprintf("round %d: nothing delivered between steps", round), len(s2.Got) == n && s2.Between == 0, "before=%d after=%d between=%d queued=%d", n, len(s2.Got), s2.Between, s2.Inbox)
		r = tb.frames(3)
		s = read()
		res.check(fmt.Sprintf("round %d: step delivers the backlog in order", round), contiguous(s) && len(s.Got) == n+r.Delivered && s.Between == 0, "got=%d delivered=%d contiguous=%v", len(s.Got), r.Delivered, contiguous(s))
	}
	t.Logf("delivered %d messages; last seq %d", len(s.Got), lastSeq(s.Got))
}

type wsGot struct {
	Seq      int
	V        float64
	Stepping bool
}

func lastSeq(v []wsGot) int {
	if len(v) == 0 {
		return 0
	}
	return v[len(v)-1].Seq
}
