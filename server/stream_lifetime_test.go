package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

func quietStreamError(StreamError) {}

func awaitStreamSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func TestStreamOptionsValidateWithoutChangingConfiguration(t *testing.T) {
	r := NewDeferredRegistry()
	if err := r.SetStreamOptions(StreamOptions{Concurrency: 2, Timeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []StreamOptions{{Concurrency: -1}, {Concurrency: 129}, {Timeout: -time.Second}} {
		if err := r.SetStreamOptions(invalid); err == nil {
			t.Fatalf("accepted invalid options: %+v", invalid)
		}
		if r.stream.Concurrency != 2 || r.stream.Timeout != time.Second {
			t.Fatal("invalid configuration mutated stream options")
		}
	}
	if err := r.SetStreamOptions(StreamOptions{}); err != nil || r.stream.Concurrency != 8 || r.stream.Timeout != 30*time.Second {
		t.Fatalf("defaults=%+v error=%v", r.stream, err)
	}
	var page *PageState
	if err := page.SetStreamOptions(StreamOptions{}); err == nil {
		t.Fatal("nil page accepted stream configuration")
	}
}

func TestStreamContextTimeoutCancelsCooperativeResolver(t *testing.T) {
	page := NewPageState()
	var observed []StreamError
	if err := page.SetStreamOptions(StreamOptions{Timeout: 40 * time.Millisecond, OnError: func(event StreamError) { observed = append(observed, event) }}); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	node := page.SuspenseContext(gosx.Text("pending"), func(ctx context.Context) (gosx.Node, error) {
		defer close(finished)
		if _, ok := ctx.Deadline(); !ok {
			return gosx.Text("missing deadline"), nil
		}
		<-ctx.Done()
		return gosx.Node{}, ctx.Err()
	})
	w := httptest.NewRecorder()
	WriteHTML(w, HTMLResponse{Node: node, Deferred: page.DeferredRegistry()})
	awaitStreamSignal(t, finished, "cooperative timeout")
	if len(observed) != 1 || !errors.Is(observed[0].Err, context.DeadlineExceeded) {
		t.Fatalf("timeout observation=%+v", observed)
	}
	if strings.Contains(w.Body.String(), "data-gosx-stream-template") {
		t.Fatal("timeout published a completion fragment")
	}
}

func TestStreamErrorObservationIsCorrelatedAndServerOnly(t *testing.T) {
	r := NewDeferredRegistry()
	var observed []StreamError
	if err := r.SetStreamOptions(StreamOptions{OnError: func(event StreamError) { observed = append(observed, event) }}); err != nil {
		t.Fatal(err)
	}
	node := r.DeferContextWithOptions(DeferredOptions{ID: "profile-section"}, gosx.Text("pending"), func(context.Context) (gosx.Node, error) {
		return gosx.Node{}, errors.New("SYNTHETIC-INTERNAL-SERVER-DETAIL")
	})
	req := httptest.NewRequest(http.MethodGet, "/stream?private=NOT-FOR-LOGS", nil)
	req.Header.Set("X-Request-ID", "synthetic-request")
	w := httptest.NewRecorder()
	WriteHTML(w, HTMLResponse{Node: node, Deferred: r, Request: req})
	if len(observed) != 1 || observed[0].RequestID != "synthetic-request" || observed[0].SlotID != "profile-section" || observed[0].Operation != "resolve" || observed[0].Err.Error() != "SYNTHETIC-INTERNAL-SERVER-DETAIL" {
		t.Fatalf("observation=%+v", observed)
	}
	if strings.Contains(w.Body.String(), "SYNTHETIC-INTERNAL") || strings.Contains(w.Body.String(), "NOT-FOR-LOGS") {
		t.Fatal("private server detail leaked into HTML")
	}
}

type streamPublicTextError string

func (e streamPublicTextError) Error() string       { return "SYNTHETIC-INTERNAL" }
func (e streamPublicTextError) SafeMessage() string { return string(e) }

func TestStreamSafeMessageEscapesMarkupAndObserverPanicIsContained(t *testing.T) {
	r := NewDeferredRegistry()
	_ = r.SetStreamOptions(StreamOptions{OnError: func(StreamError) { panic("synthetic observer failure") }})
	node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) { return gosx.Node{}, streamPublicTextError(`<img src=x onerror="unsafe">`) })
	w := httptest.NewRecorder()
	WriteHTML(w, HTMLResponse{Node: gosx.Fragment(node, gosx.RawHTML(streamTailMarker+"TAIL")), Deferred: r})
	body := w.Body.String()
	if strings.Contains(body, "<img") || !strings.Contains(body, "&lt;img") || strings.Contains(body, "synthetic observer") || !strings.HasSuffix(body, "TAIL") {
		t.Fatalf("safe text/observer boundary incorrect: %q", body)
	}
}

func TestStreamAdmissionAccountsForAbandonedCallbacksAcrossRequests(t *testing.T) {
	const each = 64
	started := make(chan struct{}, each*2)
	release := make(chan struct{})
	var callbacks sync.WaitGroup
	callbacks.Add(each * 2)
	var releaseOnce sync.Once
	defer func() {
		releaseOnce.Do(func() { close(release) })
		deadline := time.Now().Add(2 * time.Second)
		for len(deferredAdmission) != 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}()
	var requests []<-chan struct{}
	var cancels []context.CancelFunc
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		defer cancel()
		r := NewDeferredRegistry()
		if err := r.SetStreamOptions(StreamOptions{Concurrency: each, Timeout: 5 * time.Second, OnError: quietStreamError}); err != nil {
			t.Fatal(err)
		}
		for range each {
			r.Defer(gosx.Text("pending"), func() (gosx.Node, error) {
				defer callbacks.Done()
				started <- struct{}{}
				<-release
				return gosx.Text("late"), nil
			})
		}
		returned := make(chan struct{})
		requests = append(requests, returned)
		go func() {
			defer close(returned)
			WriteHTML(httptest.NewRecorder(), HTMLResponse{Node: gosx.Text("shell"), Deferred: r, Request: httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)})
		}()
	}
	for range each * 2 {
		awaitStreamSignal(t, started, "shared admission saturation")
	}
	for _, cancel := range cancels {
		cancel()
	}
	for _, returned := range requests {
		awaitStreamSignal(t, returned, "abandoned request return")
	}
	if got := len(deferredAdmission); got != cap(deferredAdmission) {
		t.Fatalf("abandoned callbacks hold %d permits, want %d", got, cap(deferredAdmission))
	}
	var extraCalls atomic.Int32
	extra := NewDeferredRegistry()
	if err := extra.SetStreamOptions(StreamOptions{Timeout: 30 * time.Millisecond, OnError: quietStreamError}); err != nil {
		t.Fatal(err)
	}
	node := extra.Defer(gosx.Text("pending"), func() (gosx.Node, error) {
		extraCalls.Add(1)
		return gosx.Text("admitted"), nil
	})
	WriteHTML(httptest.NewRecorder(), HTMLResponse{Node: node, Deferred: extra})
	if extraCalls.Load() != 0 {
		t.Fatal("saturated process launched another callback")
	}
	releaseOnce.Do(func() { close(release) })
	callbacks.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for len(deferredAdmission) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(deferredAdmission) != 0 {
		t.Fatal("returned callbacks did not release permits")
	}
	WriteHTML(httptest.NewRecorder(), HTMLResponse{Node: node, Deferred: extra})
	if extraCalls.Load() != 1 {
		t.Fatal("released capacity did not admit a subsequent request")
	}
}

type streamControlledWriter struct {
	*httptest.ResponseRecorder
	writes, flushes          int
	failWriteAt, failFlushAt int
	onWrite                  func(string)
}

func (w *streamControlledWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writes == w.failWriteAt {
		return 0, io.ErrClosedPipe
	}
	if w.onWrite != nil {
		w.onWrite(string(data))
	}
	return w.ResponseRecorder.Write(data)
}

// Disable the embedded recorder's StringWriter fast path so every write is
// observed through the test writer's controlled error/ordering contract.
func (w *streamControlledWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}
func (w *streamControlledWriter) FlushError() error {
	w.flushes++
	if w.flushes == w.failFlushAt {
		return io.ErrClosedPipe
	}
	w.ResponseRecorder.Flush()
	return nil
}

func TestStreamOutputFailureCancelsPeersWithoutTail(t *testing.T) {
	for _, mode := range []string{"write", "flush"} {
		t.Run(mode, func(t *testing.T) {
			r := NewDeferredRegistry()
			if err := r.SetStreamOptions(StreamOptions{Concurrency: 2, OnError: quietStreamError}); err != nil {
				t.Fatal(err)
			}
			peerStarted, peerFinished := make(chan struct{}), make(chan struct{})
			fast := r.Defer(gosx.Text("fast pending"), func() (gosx.Node, error) {
				<-peerStarted
				return gosx.Text("fast result"), nil
			})
			peer := r.DeferContext(gosx.Text("peer pending"), func(ctx context.Context) (gosx.Node, error) {
				close(peerStarted)
				defer close(peerFinished)
				<-ctx.Done()
				return gosx.Text("late result"), nil
			})
			w := &streamControlledWriter{ResponseRecorder: httptest.NewRecorder()}
			if mode == "write" {
				w.failWriteAt = 2
			} else {
				w.failFlushAt = 2
			}
			WriteHTML(w, HTMLResponse{Node: gosx.Fragment(fast, peer, gosx.RawHTML(streamTailMarker+"TAIL")), Deferred: r})
			awaitStreamSignal(t, peerFinished, "peer canceled on output failure")
			if w.writes != 2 || strings.Contains(w.Body.String(), "late result") || strings.Contains(w.Body.String(), "TAIL") {
				t.Fatalf("output failure kept writing: writes=%d body=%q", w.writes, w.Body.String())
			}
		})
	}
}

func TestStreamCompletionOrderNonceAndTail(t *testing.T) {
	r := NewDeferredRegistry()
	releaseSlow := make(chan struct{})
	slow := r.Defer(gosx.Text("pending slow"), func() (gosx.Node, error) { <-releaseSlow; return gosx.Text("SLOW-RESULT"), nil })
	fast := r.Defer(gosx.Text("pending fast"), func() (gosx.Node, error) { return gosx.Text("FAST-RESULT"), nil })
	w := &streamControlledWriter{ResponseRecorder: httptest.NewRecorder(), onWrite: func(value string) {
		if strings.Contains(value, "FAST-RESULT") {
			close(releaseSlow)
		}
	}}
	WriteHTML(w, HTMLResponse{Node: gosx.Fragment(slow, fast, gosx.RawHTML(streamTailMarker+"TAIL")), Deferred: r, Nonce: "test-nonce"})
	body := w.Body.String()
	if fastAt, slowAt, tailAt := strings.Index(body, "FAST-RESULT"), strings.Index(body, "SLOW-RESULT"), strings.Index(body, "TAIL"); fastAt < 0 || slowAt < fastAt || tailAt < slowAt {
		t.Fatalf("completion order/tail incorrect: %s", body)
	}
	if strings.Count(body, `<script nonce="test-nonce">`) != 2 || strings.Contains(body, streamTailMarker) {
		t.Fatalf("nonce/tail marker incorrect: %s", body)
	}
}

type streamDeadlineWriter struct {
	header  http.Header
	started chan struct{}
	stop    chan struct{}
	once    sync.Once
}

func (w *streamDeadlineWriter) Header() http.Header { return w.header }
func (w *streamDeadlineWriter) WriteHeader(int)     {}
func (w *streamDeadlineWriter) Write([]byte) (int, error) {
	close(w.started)
	<-w.stop
	return 0, io.ErrClosedPipe
}
func (w *streamDeadlineWriter) SetWriteDeadline(time.Time) error {
	w.once.Do(func() { close(w.stop) })
	return nil
}

func TestStreamCancellationInterruptsDeadlineCapableWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewDeferredRegistry()
	var calls atomic.Int32
	node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) { calls.Add(1); return gosx.Text("late"), nil })
	w := &streamDeadlineWriter{header: http.Header{}, started: make(chan struct{}), stop: make(chan struct{})}
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		WriteHTML(w, HTMLResponse{Node: node, Deferred: r, Request: httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)})
	}()
	awaitStreamSignal(t, w.started, "blocked shell write")
	cancel()
	awaitStreamSignal(t, returned, "write deadline cancellation")
	if calls.Load() != 0 {
		t.Fatal("failed blocked shell launched a resolver")
	}
}
