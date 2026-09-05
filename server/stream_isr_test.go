package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

func TestStreamISRRetainsHealthyArtifactOnFailure(t *testing.T) {
	for _, failure := range []string{"timeout", "error", "safe-error", "panic", "write", "flush", "tail-write", "tail-flush"} {
		t.Run(failure, func(t *testing.T) {
			store := &fakeISRStore{}
			artifact := isrArtifact{page: isrRoute{Path: "/stream", File: "stream.html"}, bundleRoot: "bundle", staticDir: "static", store: store}
			_, _ = store.WriteArtifact("static", "/stream", "stream.html", []byte("HEALTHY-OLD-ARTIFACT"))
			state := ISRPageState{GeneratedAt: time.Unix(100, 0).UTC(), PathVersion: 3, TagVersions: map[string]uint64{"catalog": 2}}
			_ = store.SaveState("bundle", "/stream", state)
			cfg := &isrConfig{store: store}
			_, err := cfg.regenerate(artifact, nil, func(w http.ResponseWriter, req *http.Request, _ bool) {
				r := NewDeferredRegistry()
				_ = r.SetStreamOptions(StreamOptions{Timeout: 20 * time.Millisecond, OnError: quietStreamError})
				node := r.DeferContext(gosx.Text("pending"), func(ctx context.Context) (gosx.Node, error) {
					switch failure {
					case "timeout":
						<-ctx.Done()
						return gosx.Node{}, ctx.Err()
					case "error":
						return gosx.Node{}, errors.New("synthetic provider outage")
					case "safe-error":
						return gosx.Node{}, streamSafeTestError{}
					case "panic":
						panic("synthetic resolver panic")
					default:
						return gosx.Text("NEW-HEALTHY-ARTIFACT"), nil
					}
				})
				if failure == "write" || failure == "flush" {
					w = &isrFailingStreamWriter{ResponseWriter: w, flush: failure == "flush"}
				}
				if failure == "tail-write" || failure == "tail-flush" {
					controlled := &streamControlledWriter{ResponseRecorder: w.(*httptest.ResponseRecorder)}
					if failure == "tail-write" {
						controlled.failWriteAt = 3
					} else {
						controlled.failFlushAt = 3
					}
					w = controlled
				}
				WriteHTML(w, HTMLResponse{Node: gosx.Fragment(node, gosx.RawHTML(streamTailMarker+"TAIL")), Deferred: r, Request: req})
			})
			if err == nil {
				t.Error("failed streaming refresh reported success")
			}
			got, _ := store.ReadArtifact("static", "/stream", "stream.html")
			gotState, _ := store.LoadState("bundle", "/stream", time.Time{})
			if string(got.Body) != "HEALTHY-OLD-ARTIFACT" || store.writes != 1 || !reflect.DeepEqual(gotState, state) {
				t.Fatalf("failed refresh replaced healthy body/state: writes=%d body=%q state=%+v", store.writes, got.Body, gotState)
			}
		})
	}
}

type isrFailingStreamWriter struct {
	http.ResponseWriter
	flush bool
}

func (w *isrFailingStreamWriter) Write(data []byte) (int, error) {
	if !w.flush {
		return 0, errors.New("synthetic ISR output failure")
	}
	return w.ResponseWriter.Write(data)
}
func (w *isrFailingStreamWriter) FlushError() error {
	return errors.New("synthetic ISR flush failure")
}
func (w *isrFailingStreamWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestStreamISRPublishesCompletedBodyAndArtifactValidator(t *testing.T) {
	store := &fakeISRStore{}
	artifact := isrArtifact{page: isrRoute{Path: "/stream", File: "stream.html"}, bundleRoot: "bundle", staticDir: "static", store: store}
	cfg := &isrConfig{store: store}
	info, err := cfg.regenerate(artifact, nil, func(w http.ResponseWriter, req *http.Request, _ bool) {
		r := NewDeferredRegistry()
		node := r.Defer(gosx.Text("pending"), func() (gosx.Node, error) { return gosx.Text("HEALTHY-RESULT"), nil })
		WriteHTML(w, HTMLResponse{Node: gosx.Fragment(node, gosx.RawHTML(streamTailMarker+"TAIL")), Deferred: r, Request: req})
	})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	cfg.serve(w, req, artifact, "HIT")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "HEALTHY-RESULT") || !strings.HasSuffix(w.Body.String(), "TAIL") || w.Header().Get("Last-Modified") != info.ModTime.Format(http.TimeFormat) {
		t.Fatalf("completed artifact/validator incorrect: code=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	req.Header.Set("If-Modified-Since", w.Header().Get("Last-Modified"))
	w = httptest.NewRecorder()
	cfg.serve(w, req, artifact, "HIT")
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("artifact conditional response=%d %q", w.Code, w.Body.String())
	}
}
