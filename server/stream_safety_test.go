package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

func TestStreamSafetyDoesNotInventRepresentationValidator(t *testing.T) {
	for name, policy := range map[string]CachePolicy{
		"private": PrivateCache(time.Minute),
		"shared":  PublicCache(time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			render := func(value, conditional string) *httptest.ResponseRecorder {
				registry := NewDeferredRegistry()
				node := registry.Defer(gosx.Text("pending"), func() (gosx.Node, error) { return gosx.Text(value), nil })
				cache := NewCacheState()
				cache.SetPolicy(policy)
				req := httptest.NewRequest(http.MethodGet, "/stream", nil)
				req.Header.Set("If-None-Match", conditional)
				w := httptest.NewRecorder()
				WriteHTML(w, HTMLResponse{Node: node, Deferred: registry, Request: req, Headers: http.Header{}, Cache: cache})
				return w
			}
			first := render("first", "")
			if got := first.Header().Get("ETag"); got != "" {
				t.Errorf("stream advertised request-derived representation validator %q", got)
			}
			for _, value := range []string{"first", "changed"} {
				w := render(value, first.Header().Get("ETag"))
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), value) {
					t.Errorf("fragment %q: status=%d body=%q; want fresh streamed representation", value, w.Code, w.Body.String())
				}
				if got := w.Header().Get("Cache-Control"); got != policy.headerValue() {
					t.Errorf("cache policy=%q want %q", got, policy.headerValue())
				}
			}
		})
	}
}

type streamSafeTestError struct{}

func (streamSafeTestError) Error() string       { return "SYNTHETIC-INTERNAL-DETAIL" }
func (streamSafeTestError) SafeMessage() string { return "Please retry this section." }

func TestStreamSafetyRedactsErrorsAndPanics(t *testing.T) {
	const internal = "SYNTHETIC-INTERNAL-DETAIL"
	for _, tc := range []struct {
		name    string
		resolve DeferredResolver
		want    string
	}{
		{"error", func() (gosx.Node, error) { return gosx.Node{}, errors.New(internal) }, "The server could not finish this section."},
		{"wrapped", func() (gosx.Node, error) { return gosx.Node{}, fmt.Errorf("query: %w", errors.New(internal)) }, "The server could not finish this section."},
		{"panic-string", func() (gosx.Node, error) { panic(internal) }, "The server could not finish this section."},
		{"panic-error", func() (gosx.Node, error) { panic(errors.New(internal)) }, "The server could not finish this section."},
		{"safe", func() (gosx.Node, error) { return gosx.Node{}, streamSafeTestError{} }, "Please retry this section."},
		{"wrapped-safe", func() (gosx.Node, error) { return gosx.Node{}, fmt.Errorf("query: %w", streamSafeTestError{}) }, "Please retry this section."},
		{"panic-safe", func() (gosx.Node, error) { panic(streamSafeTestError{}) }, "The server could not finish this section."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewDeferredRegistry()
			node := registry.Defer(gosx.Text("pending"), tc.resolve)
			w := httptest.NewRecorder()
			WriteHTML(w, HTMLResponse{Node: node, Deferred: registry})
			body := w.Body.String()
			if strings.Contains(body, internal) || !strings.Contains(body, tc.want) {
				t.Errorf("public fragment=%q; want only safe text %q", body, tc.want)
			}
		})
	}
}

func TestStreamSafetyCanceledRequestReturnsBeforeLegacyResolver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release, finished, returned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	registry := NewDeferredRegistry()
	node := registry.Defer(gosx.Text("pending"), func() (gosx.Node, error) {
		close(started)
		<-release
		defer close(finished)
		return gosx.Text("late-result"), nil
	})
	w := httptest.NewRecorder()
	go func() {
		defer close(returned)
		WriteHTML(w, HTMLResponse{Node: node, Deferred: registry, Request: httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)})
	}()
	<-started
	cancel()
	var prompt bool
	select {
	case <-returned:
		prompt = true
	case <-time.After(time.Second):
		t.Error("canceled WriteHTML still waits for a noncooperating legacy resolver")
	}
	close(release)
	<-finished
	<-returned
	if prompt && strings.Contains(w.Body.String(), "late-result") {
		t.Fatal("a canceled response published a late resolver result")
	}
}

func TestStreamSafetyCanceledBeforeStartDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	registry := NewDeferredRegistry()
	node := registry.Defer(gosx.Text("pending"), func() (gosx.Node, error) {
		calls.Add(1)
		return gosx.Text("late"), nil
	})
	w := httptest.NewRecorder()
	WriteHTML(w, HTMLResponse{Node: node, Deferred: registry, Request: httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)})
	if calls.Load() != 0 || w.Body.Len() != 0 {
		t.Fatalf("already-canceled request launched=%d wrote=%d bytes", calls.Load(), w.Body.Len())
	}
}

type streamFailWriter struct {
	header http.Header
	writes int
}

func (w *streamFailWriter) Header() http.Header { return w.header }
func (w *streamFailWriter) WriteHeader(int)     {}
func (w *streamFailWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, io.ErrClosedPipe
}

func TestStreamSafetyFailedShellDoesNotLaunchOrWriteTail(t *testing.T) {
	var calls atomic.Int32
	registry := NewDeferredRegistry()
	node := gosx.Fragment(registry.Defer(gosx.Text("pending"), func() (gosx.Node, error) {
		calls.Add(1)
		return gosx.Text("late"), nil
	}), gosx.RawHTML(streamTailMarker+"tail"))
	w := &streamFailWriter{header: http.Header{}}
	WriteHTML(w, HTMLResponse{Node: node, Deferred: registry})
	if calls.Load() != 0 || w.writes != 1 {
		t.Fatalf("failed shell launched=%d, writes=%d; want no resolver launch or later writes", calls.Load(), w.writes)
	}
}

func TestStreamSafetyBoundsConcurrentResolvers(t *testing.T) {
	const count = 64
	registry := NewDeferredRegistry()
	var calls atomic.Int32
	started, release, returned := make(chan struct{}, count), make(chan struct{}), make(chan struct{})
	nodes := make([]gosx.Node, 0, count)
	for range count {
		nodes = append(nodes, registry.Defer(gosx.Text("pending"), func() (gosx.Node, error) {
			calls.Add(1)
			started <- struct{}{}
			<-release
			return gosx.Text("done"), nil
		}))
	}
	go func() {
		defer close(returned)
		WriteHTML(httptest.NewRecorder(), HTMLResponse{Node: gosx.Fragment(nodes...), Deferred: registry})
	}()
	<-started
	// All callbacks remain blocked; none can free a scheduling slot.
	time.Sleep(50 * time.Millisecond)
	launched := calls.Load()
	close(release)
	<-returned
	if launched > 8 {
		t.Fatalf("launched %d concurrent callbacks; want the bounded default (8)", launched)
	}
	if calls.Load() != count {
		t.Fatalf("completed callbacks=%d, want %d", calls.Load(), count)
	}
}
