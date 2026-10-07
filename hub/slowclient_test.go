package hub

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSlowClientPolicyValidation(t *testing.T) {
	for _, p := range []SlowClientPolicy{{}, {DropThreshold: 20}, {DropThreshold: 1, CheckInterval: 100 * time.Millisecond}, {DropThreshold: 1, CheckInterval: time.Minute}} {
		if err := p.Validate(); err != nil {
			t.Fatal(p, err)
		}
	}
	p, err := (SlowClientPolicy{DropThreshold: 20}).normalized()
	if err != nil || p.CheckInterval != time.Second || (&transportState{slow: p}).interval() != time.Second {
		t.Fatal("default policy interval", p, err)
	}
	p.CheckInterval = time.Minute
	if (&transportState{slow: p}).interval() != pingPeriod {
		t.Fatal("policy replaced the control-ping interval")
	}
	for _, p := range []SlowClientPolicy{{DropThreshold: -1}, {CheckInterval: -1}, {DropThreshold: 1, CheckInterval: 99 * time.Millisecond}, {DropThreshold: 1, CheckInterval: time.Minute + 1}} {
		if !errors.Is(p.Validate(), ErrInvalidSlowClient) {
			t.Fatal("invalid policy accepted", p)
		}
		h := New("transport-fixture")
		h.SlowClient = p
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != 503 || h.served || h.pendingClients != 0 {
			t.Fatal("invalid policy admitted an upgrade")
		}
	}
}

func TestSlowClientUsesIntervalDropDelta(t *testing.T) {
	c, _, _ := syntheticTransportClient(t)
	c.transport.slow = SlowClientPolicy{DropThreshold: 20, CheckInterval: time.Second}
	c.textDropped.Add(10)
	if c.slowClient(500 * time.Millisecond) {
		t.Fatal("early policy check")
	}
	c.binaryDropped.Add(9)
	if c.slowClient(time.Second) {
		t.Fatal("subthreshold policy evicted")
	}
	if c.slowClient(2 * time.Second) {
		t.Fatal("lifetime total was reused")
	}
	c.binaryDropped.Add(20)
	if !c.slowClient(3 * time.Second) {
		t.Fatal("threshold delta was not evicted")
	}
	if c.slowClient(4 * time.Second) {
		t.Fatal("old drops were counted again")
	}
	c.transport.lastDrops = DropStats{}
	c.textDropped.Store(^uint64(0))
	c.binaryDropped.Store(^uint64(0))
	if !c.slowClient(5 * time.Second) {
		t.Fatal("drop arithmetic overflow bypassed eviction")
	}
}
