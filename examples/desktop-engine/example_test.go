package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/desktop/bridge"
	"m31labs.dev/gosx/desktop/enginehost"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/engine/host"
)

// A page played by the test: it records what the host emits and posts requests
// the way the page runtime does.
type pageEnd struct {
	t       *testing.T
	router  *bridge.Router
	mu      sync.Mutex
	msgs    []map[string]json.RawMessage
	seq     int
	session string
}

func newPageEnd(t *testing.T) *pageEnd {
	p := &pageEnd{t: t}
	p.router = bridge.NewRouter(func(raw string) error {
		env, err := bridge.ParseEnvelope(raw)
		if err != nil {
			return err
		}
		if env.Op == bridge.OpResponse {
			var answer struct{ Session string }
			if json.Unmarshal(env.Payload, &answer) == nil && answer.Session != "" {
				p.mu.Lock()
				p.session = answer.Session
				p.mu.Unlock()
			}
		}
		if env.Op != bridge.OpEvent || env.Method != enginehost.EventMessage {
			return nil
		}
		var ev struct {
			Msg map[string]json.RawMessage
		}
		if json.Unmarshal(env.Payload, &ev) == nil {
			p.mu.Lock()
			p.msgs = append(p.msgs, ev.Msg)
			p.mu.Unlock()
		}
		return nil
	}, bridge.Limit{})
	return p
}

func (p *pageEnd) request(method string, payload any) {
	p.t.Helper()
	p.seq++
	body, _ := json.Marshal(payload)
	raw, _ := json.Marshal(bridge.Envelope{Op: bridge.OpRequest, ID: "r" + time.Now().Format("05.000000000") + string(rune('a'+p.seq)), Method: method, Payload: body})
	if err := p.router.Dispatch(string(raw)); err != nil {
		p.t.Fatal(err)
	}
}

func (p *pageEnd) find(typ string, match func(map[string]json.RawMessage) bool) map[string]json.RawMessage {
	p.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		for _, m := range p.msgs {
			var got string
			_ = json.Unmarshal(m["t"], &got)
			if got == typ && (match == nil || match(m)) {
				p.mu.Unlock()
				return m
			}
		}
		p.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	p.t.Fatalf("no %q message", typ)
	return nil
}

func text(m map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(m[key], &s)
	return s
}

// The engine publishes its view and serves its calls through the desktop adapter,
// with the same signal names and result shape the island reads.
func TestCounterEngineRunsBehindDesktopIPC(t *testing.T) {
	p := newPageEnd(t)
	h := enginehost.New(p.router, enginehost.Options{})
	defer h.Close()
	if err := h.Register(component, func(in *enginehost.Instance) (host.Handle, error) { return Mount(in, 5*time.Millisecond) }); err != nil {
		t.Fatal(err)
	}
	id := "gosx-engine-" + mountID
	p.request(enginehost.MethodAttach, map[string]any{"id": id, "component": component})
	post := func(msg map[string]any) {
		p.mu.Lock()
		session := p.session
		p.mu.Unlock()
		p.request(enginehost.MethodPost, map[string]any{"id": id, "session": session, "msg": msg})
	}
	post(map[string]any{"t": "mount"})
	p.find("mounted", nil)

	// The clock advances and the view arrives paced, one batch at a time.
	batch := p.find("signals", func(m map[string]json.RawMessage) bool { return strings.Contains(text(m, "text"), `"Source":"native"`) })
	var first map[string]View
	if err := json.Unmarshal([]byte(text(batch, "text")), &first); err != nil || first[viewSignal].Source != "native" {
		t.Fatalf("first batch = %s (%v)", text(batch, "text"), err)
	}
	var seq float64
	_ = json.Unmarshal(batch["seq"], &seq)
	post(map[string]any{"t": "ack", "seq": seq, "ok": true})

	// Play the page: subscribe replies, then a call.
	call := p.find("subscribe", func(m map[string]json.RawMessage) bool { return text(m, "name") == "$"+callChannel })
	var sid float64
	_ = json.Unmarshal(call["sid"], &sid)
	post(map[string]any{"t": "signal", "sid": sid, "name": "$" + callChannel, "json": `{"ID":1,"Name":"increment","Args":{"By":3}}`})
	result := p.find("signals", func(m map[string]json.RawMessage) bool {
		return strings.Contains(text(m, "text"), `"$counter.cmd.result"`)
	})
	var reply map[string]engine.Result[Total]
	if err := json.Unmarshal([]byte(text(result, "text")), &reply); err != nil {
		t.Fatal(err)
	}
	if r := reply["$"+callChannel+".result"]; r.ID != 1 || r.Value.Total != 3 || r.Error != "" {
		t.Fatalf("reply = %+v", r)
	}
	post(map[string]any{"t": "signal", "sid": sid, "name": "$" + callChannel, "json": `{"ID":2,"Name":"refuse"}`})
	refused := p.find("signals", func(m map[string]json.RawMessage) bool { return strings.Contains(text(m, "text"), `"refused"`) })
	if !strings.Contains(text(refused, "text"), `"ID":2`) {
		t.Fatalf("refusal = %s", text(refused, "text"))
	}
}

func TestPageDeclaresTheWorkerEngineTheDesktopHostAnswers(t *testing.T) {
	handler, err := newHandler(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	html := sb.String()
	for _, want := range []string{`"thread":"worker"`, `"component":"` + component + `"`, "gosx-engine-counter", `"features":["engine-worker"]`} {
		if !strings.Contains(html, want) && !strings.Contains(html, strings.ReplaceAll(want, `"`, `&#34;`)) {
			t.Errorf("page lacks %s", want)
		}
	}
	prog, err := http.Get(srv.URL + "/app/Panel.json")
	if err != nil || prog.StatusCode != 200 {
		t.Fatalf("island program: %v %v", err, prog)
	}
	prog.Body.Close()
}
