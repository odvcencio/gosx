package room

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/wire"
	"m31labs.dev/gosx/sim"
)

// bombByte is the input byte that makes bombSim panic.
const bombByte = 0xEE

// bombSim is sumSim, except that Tick panics when any seat's input holds
// bombByte: player input bytes reaching a bug in game code.
type bombSim struct{ sumSim }

func (s *bombSim) Tick(inputs map[string]sim.Input) {
	for _, in := range inputs {
		if bytes.IndexByte(in.Data, bombByte) >= 0 {
			panic("bomb input")
		}
	}
	s.sumSim.Tick(inputs)
}

func bombMode(name string) mode.Mode {
	return mode.Mode{
		Name: name, Seats: 1, TickRate: 20, InputSize: 4, Countdown: -1,
		NewSim: func(uint64) sim.Simulation { return &bombSim{sumSim: *newSumSim(1)} },
	}
}

type panicLog struct {
	mu     sync.Mutex
	text   strings.Builder
	panics []Panic
	got    chan struct{}
}

func newPanicLog() *panicLog { return &panicLog{got: make(chan struct{}, 8)} }

func (l *panicLog) logf(format string, args ...any) {
	l.mu.Lock()
	fmt.Fprintf(&l.text, format+"\n", args...)
	l.mu.Unlock()
}

func (l *panicLog) onPanic(p Panic) {
	l.mu.Lock()
	l.panics = append(l.panics, p)
	l.mu.Unlock()
	l.got <- struct{}{}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func dialWelcome(t *testing.T, url string) (*websocket.Conn, wire.Welcome) {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { c.Close() })
	return c, readWelcome(t, c)
}

// A player's input makes one room's simulation panic. That room closes with
// 1011 and leaves the registry; another room keeps ticking; the server keeps
// accepting connections; the panic reaches Logf and OnPanic. Before the fix
// the panic ended the whole test process.
func TestRoomPanicClosesOnlyThatRoom(t *testing.T) {
	logs := newPanicLog()
	s, _, url := httpServer(t, Config{
		Modes: []mode.Mode{bombMode("bomb"), sumMode("sum", 2)},
		Logf:  logs.logf, OnPanic: logs.onPanic,
	})

	a, wa := dialWelcome(t, url+"?mode=bomb&private=1")
	_, wb := dialWelcome(t, url+"?mode=sum&private=1")
	ra, ok := s.Room(wa.Code)
	if !ok {
		t.Fatal("bomb room missing")
	}
	rb, _ := s.Room(wb.Code)
	waitUntil(t, "the bomb room to run", func() bool { return ra.Phase() == PhaseRunning })

	in := wire.Input{First: ra.Tick() + 1, Count: wire.MaxRedundancy}
	for i := range in.Count {
		in.Inputs[i] = []byte{bombByte}
	}
	if err := a.WriteMessage(websocket.BinaryMessage, in.Append(nil)); err != nil {
		t.Fatal(err)
	}

	a.SetReadDeadline(time.Now().Add(5 * time.Second))
	var closeErr *websocket.CloseError
	for {
		if _, _, err := a.ReadMessage(); err != nil {
			if !errors.As(err, &closeErr) {
				t.Fatalf("bomb room read error %v, want a close frame", err)
			}
			break
		}
	}
	if closeErr.Code != wire.CloseInternalError {
		t.Fatalf("bomb room closed with %d (%q), want %d", closeErr.Code, closeErr.Text, wire.CloseInternalError)
	}
	select {
	case <-logs.got:
	case <-time.After(5 * time.Second):
		t.Fatal("OnPanic was not called")
	}
	select {
	case <-ra.Done():
	default:
		t.Fatal("panicked room is not done")
	}
	if _, ok := s.Room(wa.Code); ok {
		t.Fatal("panicked room is still in the registry")
	}

	logs.mu.Lock()
	if len(logs.panics) != 1 || logs.panics[0].Room != wa.Code || logs.panics[0].Value != "bomb input" || len(logs.panics[0].Stack) == 0 {
		t.Fatalf("OnPanic got %+v", logs.panics)
	}
	text := logs.text.String()
	logs.mu.Unlock()
	if !strings.Contains(text, "bomb input") || !strings.Contains(text, "goroutine") {
		t.Fatalf("Logf did not get the panic and its stack:\n%s", text)
	}

	// The other room keeps ticking.
	waitUntil(t, "the sum room to run", func() bool { return rb.Phase() == PhaseRunning })
	start := rb.Tick()
	waitUntil(t, "the sum room to tick on", func() bool { return rb.Tick() >= start+5 })

	// The server still accepts connections, into a new room of the same mode.
	_, wc := dialWelcome(t, url+"?mode=bomb&private=1")
	if wc.Code == "" || wc.Code == wa.Code {
		t.Fatalf("new connection welcomed into %q", wc.Code)
	}
}

// A bot that panics closes only its own room when the caller drives time
// with Server.Advance; the other room in the same Advance call keeps running.
func TestBotPanicClosesOnlyItsRoom(t *testing.T) {
	logs := newPanicLog()
	bad := sumMode("badbot", 2)
	bad.NewBot = func(seat int, _ string) mode.Bot {
		return mode.BotFunc(func(ctx mode.BotContext, dst []byte) []byte {
			if ctx.Tick == 3 {
				panic(fmt.Sprintf("bot for seat %d", seat))
			}
			return dst
		})
	}
	s, clock := manualServer(t, Config{Modes: []mode.Mode{bad, sumMode("sum", 2)}, Logf: logs.logf, OnPanic: logs.onPanic})
	a := connect(t, s, JoinRequest{Mode: "badbot", Private: true})
	b := connect(t, s, JoinRequest{Mode: "sum", Private: true})
	rb, _ := s.Room(b.welcome.Code)

	advance(s, clock, 2*time.Second) // 1 s countdown, then ticks
	a.poll(t)
	b.poll(t)
	if code, _ := a.conn.CloseStatus(); !a.closed || code != wire.CloseInternalError {
		t.Fatalf("bad bot's room: closed %v code %d, want %d", a.closed, code, wire.CloseInternalError)
	}
	if b.closed || rb.Tick() < 10 {
		t.Fatalf("healthy room: closed %v tick %d", b.closed, rb.Tick())
	}
	if len(logs.panics) != 1 || logs.panics[0].Room != a.welcome.Code || logs.panics[0].Tick != 3 || logs.panics[0].Mode != "badbot" {
		t.Fatalf("OnPanic got %+v", logs.panics)
	}
	if _, ok := s.Room(a.welcome.Code); ok {
		t.Fatal("panicked room is still in the registry")
	}
	connect(t, s, JoinRequest{Mode: "badbot"}) // new joins still work
}

// panicLink panics when the room sends it a Pong.
type panicLink struct{ name string }

func (l panicLink) id() string { return l.name }
func (l panicLink) sendReliable(f []byte) {
	if k, _ := wire.KindOf(f); k == wire.KindPong {
		panic("send failed")
	}
}
func (l panicLink) local() *LocalConn { return nil }
func (l panicLink) close(int, string) {}

// A panic inside a frame handler (recovered by the hub's read pump) must not
// leave Room.mu held: a held lock would stall the room and, through
// quickMatchLocked, every join on the server.
func TestFramePanicReleasesRoomLock(t *testing.T) {
	s, _ := manualServer(t, Config{})
	r, err := s.Create("sum", false)
	if err != nil {
		t.Fatal(err)
	}
	r.attach(panicLink{"x"}, -1, "")
	ping := (&wire.Ping{Seq: 1}).Append(nil)
	func() {
		defer func() { _ = recover() }()
		r.onFrame("x", ping)
	}()
	if !r.mu.TryLock() {
		r.mu.Unlock() // release the leaked lock so cleanup can close the room
		t.Fatal("Room.mu is still held after a panic in a frame handler")
	}
	r.mu.Unlock()
}
