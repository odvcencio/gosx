package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type requestStartProbe struct {
	order  []string
	events int
}

func (o *requestStartProbe) ObserveRequestStart() { o.order = append(o.order, "start") }
func (o *requestStartProbe) Observe(RequestEvent) { o.order = append(o.order, "complete"); o.events++ }

func TestObserveHandlerOptionalStart(t *testing.T) {
	o := &requestStartProbe{}
	ordinary := 0
	h := ObserveHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		o.order = append(o.order, "handler")
		w.WriteHeader(204)
	}), []RequestObserver{nil, o, RequestObserverFunc(func(RequestEvent) { ordinary++ })})
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !reflect.DeepEqual(o.order, []string{"start", "handler", "complete"}) || o.events != 1 || ordinary != 1 {
		t.Fatal(o.order, o.events, ordinary)
	}
}
