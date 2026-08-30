package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGoetropeWatchAppRegistersPageModules(t *testing.T) {
	var logs bytes.Buffer
	previousOutput, previousFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})

	_, testFile, _, _ := runtime.Caller(0)
	app, err := newGoetropeWatchApp(filepath.Dir(testFile))
	if err != nil {
		t.Fatalf("build Goetrope Watch app: %v", err)
	}
	handler := app.Build()

	tests := []struct {
		path  string
		wants []string
	}{
		{
			path: "/",
			wants: []string{
				`<section class="feature-card"> <span class="eyebrow">1</span> <h3>Canonical state stays on the server.</h3>`,
				`<article class="room-preview"> <div class="room-preview__top"> <span class="eyebrow">Playing</span> <span class="mono">Prewarmed</span> </div> <strong>Alien</strong> <p>Room Alpha</p> </article>`,
			},
		},
		{
			path: "/watch/alpha",
			wants: []string{
				`<div class="queue-row"> <span class="queue-row__index">01</span> <div class="queue-row__body"> <strong>Alien</strong> <p>1979</p> </div> <span class="queue-row__state">Ready</span> </div>`,
				`<section class="transport-shell" aria-label="Playback controls prototype">`,
				`<span class="chip chip--primary">Resume</span>`,
				`<div class="moment"> <span class="eyebrow">Canonical position</span>`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d\nbody: %s", tt.path, response.Code, http.StatusOK, response.Body.String())
			}
			body := response.Body.String()
			for _, want := range tt.wants {
				if !strings.Contains(body, want) {
					t.Fatalf("GET %s response is missing strict loader output %q\nbody: %s", tt.path, want, body)
				}
			}
			for _, forbidden := range []string{
				`data-gosx-component="TransportShell"`,
				"spread source is nil",
				"Internal Server Error",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("GET %s response contains %q\nbody: %s", tt.path, forbidden, body)
				}
			}
		})
	}

	for _, forbidden := range []string{"no registered module", "spread source is nil", "file page render error"} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("app emitted %q while building or rendering routes: %s", forbidden, logs.String())
		}
	}
}
