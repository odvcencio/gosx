package hub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHubCloseWaitsForReservation(t *testing.T) {
	h := New("synthetic-room")
	o := newObservedHub()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	if !h.reserveClientSlot() {
		t.Fatal("reserve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := h.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close: %v", err)
	}
	if o.closed.Load() != 0 || h.observers.Load() == nil {
		t.Fatal("close completed with reservation pending")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/hub", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "hub closing") {
		t.Fatalf("new upgrade: %d %q", w.Code, w.Body.String())
	}
	if got := receiveHubEvent(t, o.rejected); got != "hub_closed" {
		t.Fatalf("rejected: %q", got)
	}
	h.releaseClientSlot()
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o.closed.Load() != 1 || h.observers.Load() != nil {
		t.Fatal("reservation cleanup did not finalize hub")
	}
}

func TestHubCloseDeadlineRetainsPumpOwner(t *testing.T) {
	h := New("synthetic-room")
	o := newObservedHub()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	h.On("turn", func(*Context) { close(entered); <-release })
	c := observedConnection(t, h)
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"event":"turn"}`)); err != nil {
		t.Fatal(err)
	}
	receiveHubEvent(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := h.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close: %v", err)
	}
	if o.closed.Load() != 0 || h.observers.Load() == nil {
		t.Fatal("close detached observers before pump exit")
	}
	if _, err := h.UseObserver(NoopObserver{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("install after close: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := h.Close(waitCtx); err != nil {
		t.Fatal(err)
	}
	if o.closed.Load() != 1 || o.disconnected.Load() != 1 || h.ClientCount() != 0 {
		t.Fatal("cleanup not exactly once")
	}
}

func TestHubCloseConcurrentAndDisconnect(t *testing.T) {
	h := New("synthetic-room")
	o := newObservedHub()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	c := observedConnection(t, h)
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := h.Close(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if o.closed.Load() != 1 || o.disconnected.Load() != 1 {
		t.Fatal("duplicate terminal events")
	}
	// Cleanup is idempotent even if another owner reaches the same client.
	select {
	case <-o.disconnects:
	default:
		t.Fatal("missing disconnect reason")
	}
	if h.reserveClientSlot() {
		t.Fatal("closed hub admitted upgrade")
	}
}

func TestHubDisconnectClassifiesServerReason(t *testing.T) {
	h := New("synthetic-room")
	o := newObservedHub()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	c := observedConnection(t, h)
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	h.mu.RLock()
	var client *Client
	for _, candidate := range h.clients {
		client = candidate
	}
	h.mu.RUnlock()
	if client == nil || !h.Disconnect(client.ID, "declared_leave") {
		t.Fatal("disconnect failed")
	}
	if e := receiveHubEvent(t, o.disconnects); e.Reason != "server" || e.AppReason != "declared_leave" {
		t.Fatalf("disconnect: %+v", e)
	}
	h.removeClient(client)
	if o.disconnected.Load() != 1 {
		t.Fatal("duplicate cleanup")
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHubCloseEmptyAndExpired(t *testing.T) {
	var nilHub *Hub
	if err := nilHub.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := New("synthetic-room")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expired close: %v", err)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if detach, err := h.UseObserver(nil); err != nil {
		t.Fatal(err)
	} else {
		detach()
		detach()
	}
}

func TestHubCloseRacesReservedUpgrade(t *testing.T) {
	previous := upgrader.CheckOrigin
	defer SetCheckOrigin(previous)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	SetCheckOrigin(func(*http.Request) bool { close(entered); <-release; return true })
	h := New("synthetic-room")
	o := newObservedHub()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer func() { releaseOnce.Do(func() { close(release) }); s.Close() }()
	dialDone := make(chan struct{})
	go func() {
		defer close(dialDone)
		conn, response, _ := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), nil)
		if conn != nil {
			_ = conn.Close()
		}
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
	}()
	receiveHubEvent(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := h.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	receiveHubEvent(t, dialDone)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := h.Close(waitCtx); err != nil {
		t.Fatal(err)
	}
	if o.connected.Load() != 0 || o.disconnected.Load() != 0 || o.closed.Load() != 1 {
		t.Fatal("reserved upgrade entered live connection lifecycle after close")
	}
	if got := receiveHubEvent(t, o.rejected); got != "hub_closed" {
		t.Fatalf("reason=%q", got)
	}
}

type blockedClosedObserver struct {
	NoopObserver
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (o *blockedClosedObserver) Closed(*Hub) { o.calls.Add(1); close(o.entered); <-o.release }

func TestHubCloseCallersBoundedDuringBlockedCompletion(t *testing.T) {
	h := New("synthetic-room")
	o := &blockedClosedObserver{entered: make(chan struct{}), release: make(chan struct{})}
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(o.release) })
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		err := h.Close(ctx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	receiveHubEvent(t, o.entered)
	if o.calls.Load() != 1 || h.observers.Load() != nil {
		t.Fatal("completion duplicated or dispatch still open")
	}
	select {
	case <-h.closeDone:
		t.Fatal("blocked completion falsely reported success")
	default:
	}
	releaseOnce.Do(func() { close(o.release) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if h.observers.Load() != nil {
		t.Fatal("completed owner retained subscriber")
	}
}

// A later Closed subscriber may call arbitrary hub APIs. It must not reopen
// dispatch to a subscriber whose Closed callback has already returned.
type reentrantCloseObserver struct {
	NoopObserver
	closed, broadcasts atomic.Int64
	reenter            bool
}

func (o *reentrantCloseObserver) Closed(h *Hub) {
	o.closed.Add(1)
	if o.reenter {
		h.Broadcast("state", 1)
	}
}
func (o *reentrantCloseObserver) Broadcast(*Hub, int, int) { o.broadcasts.Add(1) }

func TestHubClosedDetachesBeforeReentrantDispatch(t *testing.T) {
	h := New("synthetic-room")
	first := &reentrantCloseObserver{}
	second := &reentrantCloseObserver{reenter: true}
	for _, o := range []*reentrantCloseObserver{first, second} {
		if _, err := h.UseObserver(o); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.Broadcast("state", 2)
	for _, o := range []*reentrantCloseObserver{first, second} {
		if o.closed.Load() != 1 || o.broadcasts.Load() != 0 {
			t.Fatal("subscriber received a callback after Closed")
		}
	}
}
