package hosttest

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"m31labs.dev/gosx/game/host"
)

func startServer(t *testing.T) *Server {
	t.Helper()
	srv, err := Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}

// readyPage connects a page, steps one reading frame and waits for the server
// to report ready. The caller steps the frames after that.
func readyPage(t *testing.T, srv *Server) *fakePage {
	t.Helper()
	p := newFakePage(t, srv)
	p.frame(true)
	if err := srv.WaitReady(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	return p
}

func must(t *testing.T) func(Receipt, error) Receipt {
	return func(r Receipt, err error) Receipt {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
}

func eventsFor(p *fakePage, device string) []edge {
	var out []edge
	for _, e := range p.sink.snapshot() {
		if e.Device == device {
			out = append(out, e)
		}
	}
	return out
}

func TestPageURL(t *testing.T) {
	srv := startServer(t)
	in := url.URL{Scheme: "file", Path: "/game.html", RawQuery: "a=1&gosx-padtest=old&b=%20x", Fragment: "ref=title"}
	out := srv.PageURL(in)
	if out.Fragment != "ref=title" || out.Path != "/game.html" {
		t.Fatalf("fragment or path changed: %v", out.String())
	}
	if want := "a=1&b=%20x&gosx-padtest=" + host.FormatPadTestEndpoint(srv.port, srv.token); out.RawQuery != want {
		t.Fatalf("query %q, want %q", out.RawQuery, want)
	}
	if in.RawQuery != "a=1&gosx-padtest=old&b=%20x" {
		t.Fatal("PageURL changed its argument")
	}
	port, token, err := host.ParsePadTestEndpoint(out.Query().Get(QueryField))
	if err != nil || port != srv.port || token != srv.token || len(token) != 32 {
		t.Fatalf("endpoint %d %q %v", port, token, err)
	}
	if !strings.HasPrefix(srv.Addr(), "127.0.0.1:") {
		t.Fatal(srv.Addr())
	}
}

func TestTokensDifferPerRun(t *testing.T) {
	a, b := startServer(t), startServer(t)
	if a.token == b.token || a.port == b.port {
		t.Fatal("two servers shared a token or port")
	}
}

func TestWaitReadyNeedsAReadingFrame(t *testing.T) {
	srv := startServer(t)
	p := newFakePage(t, srv)
	p.frame(false)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := srv.WaitReady(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ready after a frame with no read: %v", err)
	}
	p.frame(true)
	if err := srv.WaitReady(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
}

func TestPressReleaseRoundTripThroughPadPoller(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	p.run()
	ctx := ctxFor(t)
	plug := must(t)(srv.Plug(ctx, 0, host.DefaultPadTestID, "standard"))
	press := must(t)(srv.Press(ctx, 0, 0))
	release := must(t)(srv.Release(ctx, 0, 0))
	if !(plug.Seq < press.Seq && press.Seq < release.Seq && press.Frame < release.Frame && press.Sampled < release.Sampled) {
		t.Fatalf("receipts not ordered: %+v %+v %+v", plug, press, release)
	}
	want := []edge{{press.Frame, "pad:0", "button0", 1}, {release.Frame, "pad:0", "button0", 0}}
	if got := eventsFor(p, "pad:0"); !reflect.DeepEqual(got, want) {
		t.Fatalf("poller edges %+v, want %+v", got, want)
	}
}

func TestStableSlotsAfterUnpluggingSlotZero(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	p.run()
	ctx := ctxFor(t)
	must(t)(srv.Plug(ctx, 0, "Pad A", "standard"))
	must(t)(srv.Plug(ctx, 1, "Pad B", "standard"))
	must(t)(srv.Unplug(ctx, 0))
	press := must(t)(srv.Press(ctx, 1, 4))
	if _, err := srv.Press(ctx, 0, 4); !errors.Is(err, ErrNotPlugged) {
		t.Fatalf("press on the empty slot: %v", err)
	}
	got := eventsFor(p, "pad:1")
	if len(got) != 1 || got[0].Frame != press.Frame || got[0].Code != "button4" {
		t.Fatalf("pad:1 edges %+v", got)
	}
	if len(eventsFor(p, "pad:0")) != 0 {
		t.Fatal("slot 0 reported edges")
	}
}

func TestReplugWithSameIDShowsAbsentPollFirst(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	p.run()
	ctx := ctxFor(t)
	must(t)(srv.Plug(ctx, 0, "SAME", "standard"))
	must(t)(srv.Press(ctx, 0, 2))
	if _, err := srv.Plug(ctx, 0, "SAME", "standard"); !errors.Is(err, ErrAlreadyPlugged) {
		t.Fatalf("plug over an occupied slot: %v", err)
	}
	unplug := must(t)(srv.Unplug(ctx, 0))
	replug := must(t)(srv.Plug(ctx, 0, "SAME", "standard"))
	if replug.Frame <= unplug.Frame {
		t.Fatalf("replug %+v did not follow the unplug %+v", replug, unplug)
	}
	got := eventsFor(p, "pad:0")
	want := []edge{{got[0].Frame, "pad:0", "button2", 1}, {unplug.Frame, "pad:0", "button2", 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edges %+v, want %+v (the unplug releases the held button)", got, want)
	}
}

func TestSetAtDoesTwoPadsAtomicallyOnItsFrame(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	p.run()
	ctx := ctxFor(t)
	must(t)(srv.Plug(ctx, 0, "A", "standard"))
	must(t)(srv.Plug(ctx, 1, "B", "standard"))
	a, b := host.NeutralStandardPad(0, "A", "standard"), host.NeutralStandardPad(1, "B", "standard")
	a.Buttons[7], b.Axes[2] = 1, -0.8
	target := p.src.Frame() + 40
	r := must(t)(srv.SetAt(ctx, target, []host.GamepadState{b, a}))
	if r.Frame != target {
		t.Fatalf("applied on frame %d, want %d", r.Frame, target)
	}
	var onTarget []string
	for _, e := range p.sink.snapshot() {
		if e.Frame == target {
			onTarget = append(onTarget, e.Device+"/"+e.Code)
		}
	}
	if !reflect.DeepEqual(onTarget, []string{"pad:0/button7", "pad:1/axis2"}) {
		t.Fatalf("edges on frame %d: %v", target, onTarget)
	}
}

func TestSetAtLateFrameIsRejectedWithCurrentFrame(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	for i := 0; i < 4; i++ {
		p.frame(true)
	}
	_, err := srv.SetAt(ctxFor(t), 3, []host.GamepadState{host.NeutralStandardPad(0, "A", "standard")})
	var late *LateFrameError
	if !errors.As(err, &late) || late.Requested != 3 || late.Current != 5 {
		t.Fatalf("err = %v, want LateFrameError for frame 3 at frame 5", err)
	}
	// The rejected change left no trace: the slot is still free.
	p.run()
	must(t)(srv.Plug(ctxFor(t), 0, "A", "standard"))
}

func TestSetAtWithNoReadOnItsFrame(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	target := p.src.Frame() + 3
	type result struct {
		r   Receipt
		err error
	}
	done := make(chan result, 1)
	go func() {
		r, err := srv.SetAt(ctxFor(t), target, []host.GamepadState{host.NeutralStandardPad(0, "A", "standard")})
		done <- result{r, err}
	}()
	p.waitSets(1)
	for p.src.Frame() < target {
		p.frame(false) // frames through the target draw but do not read
	}
	p.frame(true)
	res := <-done
	if !errors.Is(res.err, ErrFrameNotSampled) || res.r.Frame != target+1 {
		t.Fatalf("result %+v err %v, want ErrFrameNotSampled with receipt on frame %d", res.r, res.err, target+1)
	}
}

func TestWaitersOnAbsoluteThresholdsDoNotLoseWakeups(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	p.run()
	ctx := ctxFor(t)
	since := must(t)(srv.Plug(ctx, 0, "A", "standard"))
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for k := uint64(0); k < 30; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- srv.WaitSampled(ctx, since, k)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// A threshold that passed long ago returns at once.
	if err := srv.WaitSampled(ctx, Receipt{}, 1); err != nil {
		t.Fatal(err)
	}
	before := must(t)(srv.Progress(ctx))
	if err := srv.WaitFrames(ctx, 5); err != nil {
		t.Fatal(err)
	}
	if got := srv.Frames(); got < before.Drawn+5 {
		t.Fatalf("Frames() = %d after waiting 5 frames from %d", got, before.Drawn)
	}
}

func TestCanceledWaitsLeaveNoWaiters(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- srv.WaitFrames(ctx, 100) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait: %v", err)
	}
	srv.mu.Lock()
	n := len(srv.watches)
	srv.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d waiters left after cancel", n)
	}
	// The page's late progress for the dead watch changes nothing.
	for i := 0; i < 3; i++ {
		p.frame(true)
	}
	p.run()
	if err := srv.WaitFrames(ctxFor(t), 2); err != nil {
		t.Fatal(err)
	}
}

func TestStalledPageFailsWithDiagnosis(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err := srv.WaitFrames(ctx, 3); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "last drawn 1") {
		t.Fatalf("wait on a page that draws nothing: %v", err)
	}
	if _, err := srv.Plug(ctx, 0, "A", "standard"); !errors.Is(err, context.DeadlineExceeded) ||
		!strings.Contains(err.Error(), "sent seq 1") || !strings.Contains(err.Error(), "applied seq 0") {
		t.Fatalf("plug on a stalled page: %v", err)
	}
	// When the page wakes, the timed-out change lands and the next works.
	p.run()
	r := must(t)(srv.Press(ctxFor(t), 0, 0))
	if r.Seq != 2 {
		t.Fatalf("press receipt %+v, want seq 2", r)
	}
}

func TestClosedConnectionFailsPendingAndLaterOperations(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	errc := make(chan error, 1)
	go func() {
		_, err := srv.Plug(ctxFor(t), 0, "A", "standard")
		errc <- err
	}()
	p.waitSets(1)
	waitc := make(chan error, 1)
	go func() { waitc <- srv.WaitFrames(ctxFor(t), 50) }()
	time.Sleep(20 * time.Millisecond)
	p.conn.Close()
	for name, ch := range map[string]chan error{"plug": errc, "wait": waitc} {
		if err := <-ch; !errors.Is(err, ErrPageGone) {
			t.Errorf("pending %s: %v", name, err)
		}
	}
	ctx := ctxFor(t)
	if _, err := srv.Press(ctx, 0, 0); !errors.Is(err, ErrPageGone) {
		t.Errorf("press after loss: %v", err)
	}
	if _, err := srv.Progress(ctx); !errors.Is(err, ErrPageGone) {
		t.Errorf("progress after loss: %v", err)
	}
	if err := srv.WaitReady(ctx); err != nil {
		t.Errorf("WaitReady after a ready page was lost: %v", err)
	}
	// Single use: no new page connects.
	if c, _, err := websocket.DefaultDialer.Dial(host.PadTestWebSocketURL(srv.port), nil); err == nil {
		c.Close()
		t.Error("a second page connected after the first was lost")
	}
}

func TestCloseAndContextFailPendingOperations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv, err := Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := readyPage(t, srv)
	errc := make(chan error, 1)
	go func() {
		_, err := srv.Plug(context.Background(), 0, "A", "standard")
		errc <- err
	}()
	p.waitSets(1)
	cancel()
	if err := <-errc; !errors.Is(err, ErrClosed) {
		t.Fatalf("after Start ctx ended: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Progress(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
}

func TestOperationsBeforeAPageConnects(t *testing.T) {
	srv := startServer(t)
	if _, err := srv.Plug(ctxFor(t), 0, "A", "standard"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("plug with no page: %v", err)
	}
}

func TestServerValidatesBeforeSending(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	p.run()
	ctx := ctxFor(t)
	must(t)(srv.Plug(ctx, 0, "A", "standard"))
	if _, err := srv.SetPad(ctx, 1, host.NeutralStandardPad(0, "A", "standard")); err == nil {
		t.Error("SetPad accepted a state whose Index differs from the slot")
	}
	if _, err := srv.Press(ctx, 0, 99); err == nil {
		t.Error("Press accepted a button the pad does not have")
	}
	if _, err := srv.Axis(ctx, 0, 0, 1.5); err == nil {
		t.Error("Axis accepted 1.5")
	}
	if _, err := srv.Plug(ctx, 9, "A", "standard"); err == nil {
		t.Error("Plug accepted slot 9")
	}
	if _, err := srv.Plug(ctx, 1, "A", "weird"); err == nil {
		t.Error("Plug accepted mapping weird")
	}
	if _, err := srv.Plug(ctx, 1, "", "standard"); err == nil {
		t.Error("Plug accepted an empty ID")
	}
	srv.mu.Lock()
	sent := srv.sentSeq
	srv.mu.Unlock()
	if sent != 1 {
		t.Errorf("invalid requests used sequence numbers: sent %d", sent)
	}
	// A good axis change still works and keeps the deadzone path honest.
	r := must(t)(srv.Axis(ctx, 0, 0, 0.9))
	if got := eventsFor(p, "pad:0"); len(got) != 1 || got[0].Code != "axis0" || got[0].Frame != r.Frame {
		t.Fatalf("axis edges %+v", got)
	}
}

func TestNonStandardMappingIsIgnoredByPoller(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	p.run()
	ctx := ctxFor(t)
	must(t)(srv.Plug(ctx, 0, "Odd", ""))
	must(t)(srv.Press(ctx, 0, 0))
	if got := eventsFor(p, "pad:0"); len(got) != 0 {
		t.Fatalf("a pad with no standard mapping produced edges: %+v", got)
	}
}

func rawDial(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(host.PadTestWebSocketURL(srv.port), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	return c
}

func sendMsg(t *testing.T, c *websocket.Conn, m host.PadMessage) {
	t.Helper()
	data, err := host.EncodePadMessage(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatal(err)
	}
}

func expectClose(t *testing.T, c *websocket.Conn, code int) {
	t.Helper()
	_, _, err := c.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != code {
		t.Fatalf("read error %v, want close code %d", err, code)
	}
}

func TestWrongTokenIsRefusedAndRealPageStillConnects(t *testing.T) {
	srv := startServer(t)
	c := rawDial(t, srv)
	sendMsg(t, c, host.PadMessage{Type: host.PadMsgHello, Version: host.PadTestProtocolVersion, Token: strings.Repeat("0", 32)})
	expectClose(t, c, websocket.ClosePolicyViolation)
	c2 := rawDial(t, srv)
	sendMsg(t, c2, host.PadMessage{Type: host.PadMsgHello, Version: 99, Token: srv.token})
	expectClose(t, c2, websocket.ClosePolicyViolation)
	c3 := rawDial(t, srv)
	sendMsg(t, c3, host.PadMessage{Type: host.PadMsgProgress}) // not a hello
	expectClose(t, c3, websocket.ClosePolicyViolation)
	readyPage(t, srv)
}

func TestSecondPageIsRefused(t *testing.T) {
	srv := startServer(t)
	readyPage(t, srv)
	// A browser reports an HTTP status on a refused upgrade as an error and a
	// 1006 close, which a page treats as "retry". The refusal must be a
	// WebSocket close with a final code and a reason.
	c, _, err := websocket.DefaultDialer.Dial(host.PadTestWebSocketURL(srv.port), nil)
	if err != nil {
		t.Fatalf("second dial: %v, want an upgraded socket that is then closed", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = c.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != websocket.ClosePolicyViolation || ce.Text != "page already connected" {
		t.Fatalf("second dial read error %v, want close 1008 \"page already connected\"", err)
	}
}

func TestSecondPageWithValidTokenRacingTheFirstIsRefused(t *testing.T) {
	srv := startServer(t)
	a, b := rawDial(t, srv), rawDial(t, srv) // both upgraded before either says hello
	hello := host.PadMessage{Type: host.PadMsgHello, Version: host.PadTestProtocolVersion, Token: srv.token}
	sendMsg(t, a, hello)
	if _, _, err := a.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	sendMsg(t, b, hello)
	expectClose(t, b, websocket.ClosePolicyViolation)
}

func TestOversizeMessagesAreRefused(t *testing.T) {
	srv := startServer(t)
	c := rawDial(t, srv)
	c.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 200<<10)))
	expectClose(t, c, websocket.CloseMessageTooBig)

	// An authenticated page that sends too much loses the session.
	p := readyPage(t, srv)
	p.wmu.Lock()
	p.conn.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 200<<10)))
	p.wmu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := srv.Progress(ctxFor(t)); errors.Is(err, ErrPageGone) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session survived an oversize message")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHelloTimeout(t *testing.T) {
	srv, err := StartWith(context.Background(), Options{HelloTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	c := rawDial(t, srv)
	start := time.Now()
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("silent connection stayed open")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("hello timeout did not fire")
	}
	readyPage(t, srv)
}

func TestMalformedPageMessageEndsSession(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	p.wmu.Lock()
	p.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"set","seq":1}`)) // pages never send set
	p.wmu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := srv.Progress(ctxFor(t)); errors.Is(err, ErrPageGone) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("session survived a message a page may not send")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func Example() {
	ctx := context.Background()
	srv, _ := Start(ctx)
	defer srv.Close()
	page := srv.PageURL(url.URL{Scheme: "file", Path: "/game.html"})
	fmt.Println(strings.HasPrefix(page.RawQuery, "gosx-padtest="))
	// Output: true
}

func TestDefaultHelloTimeoutCoversABusyPage(t *testing.T) {
	srv := startServer(t)
	c := rawDial(t, srv)
	time.Sleep(6 * time.Second) // a busy page's main thread may hold its hello this long
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	sendMsg(t, c, host.PadMessage{Type: host.PadMsgHello, Version: host.PadTestProtocolVersion, Token: srv.token})
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("the server gave up on a hello that came after 6 s: %v", err)
	}
	m, err := host.DecodePadMessage(data)
	if err != nil || m.Type != host.PadMsgReady {
		t.Fatalf("answer %+v, %v; want ready", m, err)
	}
}

func TestASecondSocketJoinsAfterTheFirstTimesOut(t *testing.T) {
	srv, err := StartWith(context.Background(), Options{HelloTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	first := rawDial(t, srv)
	if _, _, err := first.ReadMessage(); err == nil {
		t.Fatal("the silent first socket stayed open")
	}
	second := rawDial(t, srv)
	sendMsg(t, second, host.PadMessage{Type: host.PadMsgHello, Version: host.PadTestProtocolVersion, Token: srv.token})
	_, data, err := second.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if m, err := host.DecodePadMessage(data); err != nil || m.Type != host.PadMsgReady {
		t.Fatalf("answer %+v, %v; want ready", m, err)
	}
}

func TestPadReturnsWhatWasLastSent(t *testing.T) {
	srv := startServer(t)
	p := readyPage(t, srv)
	p.run()
	ctx := ctxFor(t)
	if _, ok := srv.Pad(0); ok {
		t.Fatal("Pad(0) reported a pad before any was plugged")
	}
	must(t)(srv.Plug(ctx, 0, host.DefaultPadTestID, "standard"))
	must(t)(srv.Press(ctx, 0, 3))
	must(t)(srv.Axis(ctx, 0, 1, -0.5))
	got, ok := srv.Pad(0)
	if !ok || got.Index != 0 || got.Buttons[3] != 1 || got.Axes[1] != -0.5 {
		t.Fatalf("Pad(0) = %+v, %v", got, ok)
	}
	got.Buttons[3] = 0 // the copy is the caller's: the server's state must not change
	if again, _ := srv.Pad(0); again.Buttons[3] != 1 {
		t.Fatal("Pad returned a slice that aliases the server's state")
	}
	if _, ok := srv.Pad(1); ok {
		t.Fatal("Pad(1) reported a pad that was never plugged")
	}
	must(t)(srv.Unplug(ctx, 0))
	if _, ok := srv.Pad(0); ok {
		t.Fatal("Pad(0) still reports an unplugged pad")
	}
}
