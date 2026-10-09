package route

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/server"
)

func TestFileActionObservationBeforeLookup(t *testing.T) {
	page := FilePage{Pattern: "/room/{id}", Source: "page.gsx"}
	called := 0
	leaf := buildFileActionHandler(page, FileActions{"save": func(*action.Context) error { called++; return nil }}, 0)
	router := NewRouter()
	router.handleKind("action", filePageActionPattern(page.Pattern), leaf)
	var inner, outer server.RequestEvent
	router.UseObserver(server.RequestObserverFunc(func(e server.RequestEvent) { inner = e }))
	app := server.New()
	app.UseObserver(server.RequestObserverFunc(func(e server.RequestEvent) { outer = e }))
	app.Mount("/game/", http.StripPrefix("/game", router.Build()))
	handler := app.Build()
	for _, name := range []string{"save", "missing"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/game/room/123/__actions/"+name, strings.NewReader(""))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handler.ServeHTTP(w, r)
		want := http.StatusSeeOther // Form actions preserve POST-redirect-GET.
		if name == "missing" {
			want = 404
		}
		if w.Code != want {
			t.Fatalf("%s: status=%d body=%s", name, w.Code, w.Body.String())
		}
		for _, event := range []server.RequestEvent{inner, outer} {
			if event.Kind != "action" || event.Pattern != "POST /room/{id}/__actions/{__gosx_action}" || event.Status != want {
				t.Fatalf("%s: event=%+v", name, event)
			}
		}
	}
	if called != 1 {
		t.Fatalf("action calls=%d", called)
	}
}

func TestFileActionObservationMissingName(t *testing.T) {
	var event server.RequestEvent
	h := server.ObserveHandler(buildFileActionHandler(FilePage{Pattern: "/"}, nil, 0), []server.RequestObserver{server.RequestObserverFunc(func(e server.RequestEvent) { event = e })})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/__actions/", nil))
	if event.Kind != "action" || event.Pattern != "POST /__actions/{__gosx_action}" || event.Status != 400 {
		t.Fatalf("event=%+v", event)
	}
}
