package hosttest

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/host"
)

type edge struct {
	Frame  uint64
	Device string
	Code   string
	Value  float64
}

type edgeSink struct {
	mu     sync.Mutex
	frame  uint64
	events []edge
}

func (s *edgeSink) Feed(e actions.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, edge{s.frame, e.Device, e.Source.Code, e.Value})
}
func (s *edgeSink) ReleaseAll() {}

func (s *edgeSink) snapshot() []edge {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]edge(nil), s.events...)
}

// fakePage is the page side of the protocol in plain Go: a WebSocket, a
// PadTestSession over a PadTestSource, and a frame loop that tests step by
// hand (frame) or leave running (run). Frames poll through the real
// actions.PadPoller, like the game's input path.
type fakePage struct {
	t      *testing.T
	conn   *websocket.Conn
	src    *host.PadTestSource
	sess   *host.PadTestSession
	poller *actions.PadPoller
	sink   *edgeSink
	wmu    sync.Mutex
	fmu    sync.Mutex // one frame at a time
	stop   chan struct{}
	wg     sync.WaitGroup
	sets   atomic.Int64
}

// dialRaw opens the WebSocket named by a decorated page URL, parsing the one
// query field the way the browser adapter will.
func dialRaw(t *testing.T, page url.URL) (*websocket.Conn, *http.Response, string, error) {
	t.Helper()
	port, token, err := host.ParsePadTestEndpoint(page.Query().Get(QueryField))
	if err != nil {
		t.Fatalf("page URL %q: %v", page.String(), err)
	}
	conn, resp, err := websocket.DefaultDialer.Dial(host.PadTestWebSocketURL(port), nil)
	return conn, resp, token, err
}

func newFakePage(t *testing.T, srv *Server) *fakePage {
	t.Helper()
	page := srv.PageURL(url.URL{Scheme: "file", Path: "/game.html", Fragment: "ref=x"})
	conn, _, token, err := dialRaw(t, page)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	src := host.NewPadTestSource()
	p := &fakePage{t: t, conn: conn, src: src, sess: host.NewPadTestSession(src, token),
		poller: actions.NewPadPoller(), sink: &edgeSink{}, stop: make(chan struct{})}
	conn.SetReadLimit(host.PadTestMaxMessage)
	p.send(p.sess.Hello())
	p.wg.Add(1)
	go p.readLoop()
	t.Cleanup(func() { p.close() })
	return p
}

func (p *fakePage) send(msgs ...host.PadMessage) {
	for _, m := range msgs {
		data, err := host.EncodePadMessage(m)
		if err != nil {
			p.t.Errorf("encode: %v", err)
			return
		}
		p.wmu.Lock()
		p.conn.WriteMessage(websocket.TextMessage, data)
		p.wmu.Unlock()
	}
}

func (p *fakePage) readLoop() {
	defer p.wg.Done()
	for {
		_, data, err := p.conn.ReadMessage()
		if err != nil {
			return
		}
		m, err := host.DecodePadMessage(data)
		if err != nil {
			p.t.Errorf("page got bad message: %v", err)
			return
		}
		p.send(p.sess.Handle(m)...)
		if m.Type == host.PadMsgSet {
			p.sets.Add(1)
		}
	}
}

// frame runs one engine frame: begin, optionally read through the poller, end.
func (p *fakePage) frame(read bool) uint64 {
	p.fmu.Lock()
	defer p.fmu.Unlock()
	p.sink.mu.Lock()
	p.sink.frame = p.src.BeginFrame()
	f := p.sink.frame
	p.sink.mu.Unlock()
	if read {
		p.poller.Poll(p.src, p.sink)
	}
	p.send(p.sess.EndFrame()...)
	return f
}

// run steps a reading frame every millisecond until the test ends. It stands
// in for the browser's frame callback; the 1 ms step is a pacing choice, and
// no test asserts which frame a change lands on while it runs.
func (p *fakePage) run() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-tick.C:
				p.frame(true)
			}
		}
	}()
}

func (p *fakePage) close() {
	select {
	case <-p.stop:
	default:
		close(p.stop)
	}
	p.conn.Close()
	p.wg.Wait()
}

func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// waitSets blocks until the page has handled n set messages, so a test knows a
// change reached the source before it steps frames.
func (p *fakePage) waitSets(n int64) {
	p.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for p.sets.Load() < n {
		if time.Now().After(deadline) {
			p.t.Fatalf("page handled %d set messages, want %d", p.sets.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}
