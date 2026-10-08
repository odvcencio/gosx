//go:build !js || !wasm

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/internal/telemetryauthority"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/telemetry/metric"
)

func hubCoreOptions() Options {
	o := Defaults()
	o.Listen.Addr = "off"
	o.Activities.Disabled = true
	o.Metrics.DisableRequests = true
	o.Metrics.DisableOperations = true
	o.Metrics.DisableClientEvents = true
	o.Metrics.DisableRuntime = true
	o.Metrics.DisableReadiness = true
	o.Metrics.DisableScheduled = true
	return o
}
func hubTelemetry(tb testing.TB) (*Telemetry, *server.App) {
	tb.Helper()
	tb.Setenv("GOSX_TELEMETRY", "on")
	a := server.New()
	tel, err := Enable(a, hubCoreOptions())
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := tel.Close(context.Background()); err != nil {
			tb.Error(err)
		}
	})
	return tel, a
}
func hubSample(tb testing.TB, tel *Telemetry, name string, labels ...string) metric.SeriesSnapshot {
	tb.Helper()
	var result metric.SeriesSnapshot
	found := false
	err := tel.registry.WithSnapshot(context.Background(), func(s metric.Snapshot) error {
		for _, f := range s.Families {
			if f.Name != name {
				continue
			}
			for _, row := range f.Series {
				match := len(row.Labels) == len(labels)/2
				for i := 0; i < len(labels); i += 2 {
					ok := false
					for _, l := range row.Labels {
						if l.Name == labels[i] && l.Value == labels[i+1] {
							ok = true
						}
					}
					match = match && ok
				}
				if !match {
					continue
				}
				found = true
				result = row
				result.Labels = slices.Clone(row.Labels)
				if h := row.Histogram; h != nil {
					result.Histogram = &metric.HistogramSnapshot{Bounds: slices.Clone(h.Bounds), Counts: slices.Clone(h.Counts), Count: h.Count, Sum: h.Sum}
				}
			}
		}
		return nil
	})
	if err != nil || !found {
		tb.Fatalf("sample %s %v: found=%v err=%v", name, labels, found, err)
	}
	return result
}
func hubWait(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("hub callback did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestHubGroupValidationAdmissionAndDisabled(t *testing.T) {
	for _, tel := range []*Telemetry{nil, {}} {
		if _, err := tel.NewHubGroup("Bad", HubOptions{}); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal(err)
		}
		if _, err := tel.NewHubGroup("room", HubOptions{ClientAckSampleRate: .01}); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal(err)
		} else {
			var e *ConfigError
			if !errors.As(err, &e) || e.Code != "unsupported" {
				t.Fatal(err)
			}
		}
		g, err := tel.NewHubGroup("room", HubOptions{})
		if err != nil {
			t.Fatal(err)
		}
		h := hub.New("fixture")
		detach, err := g.Attach(h)
		if err != nil {
			t.Fatal(err)
		}
		detach()
		if _, err := h.UseTelemetryObserver(hub.NoopObserver{}, 0, telemetryauthority.New()); err != nil {
			t.Fatal("disabled group reserved a slot", err)
		}
		if _, err := g.Attach(nil); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal(err)
		}
	}
	tel, app := hubTelemetry(t)
	for _, opts := range []HubOptions{{Events: []string{"x", "x"}}, {DisconnectReasons: []string{""}}, {Events: make([]string, 65)}} {
		if _, err := tel.NewHubGroup("room", opts); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal(err)
		}
	}
	g, err := tel.NewHubGroup("room", HubOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tel.NewHubGroup("room", HubOptions{}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	app.Build()
	if _, err := tel.NewHubGroup("late", HubOptions{}); !errors.Is(err, ErrAfterBuild) {
		t.Fatal(err)
	}
	h := hub.New("dynamic")
	detach, err := g.Attach(h)
	if err != nil {
		t.Fatal("dynamic attachment after Build", err)
	}
	other, _ := hubTelemetry(t)
	g2, _ := other.NewHubGroup("room", HubOptions{})
	if _, err := g2.Attach(h); !errors.Is(err, ErrConflict) {
		t.Fatal("cross-owner duplicate", err)
	}
	detach()
	detach()
	if _, err := g2.Attach(h); err != nil {
		t.Fatal("unused hub slot was not released", err)
	}
	if hubSample(t, tel, "gosx_hub_instances", "hub", "room").Gauge != 0 {
		t.Fatal("detach counted twice")
	}

	o := aggregateCoreOptions(t)
	o.Metrics.MaxSeries = 200
	small, err := Enable(server.New(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close(context.Background())
	before := small.registry.Usage()
	if _, err := small.NewHubGroup("room", HubOptions{Events: []string{"input"}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if after := small.registry.Usage(); before != after || len(small.hubs.groups) != 0 {
		t.Fatal("failed group partially reserved", before, after)
	}
}

func TestHubGroupExactCountersAndPrivacy(t *testing.T) {
	tel, _ := hubTelemetry(t)
	opts := HubOptions{Events: []string{"input"}, DisconnectReasons: []string{"idle"}, QueueSampleEvery: 1}
	g, err := tel.NewHubGroup("room", opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Events[0] = "private-event"
	opts.DisconnectReasons[0] = "private-reason"
	h := hub.New("private-room-code")
	detach, err := g.Attach(h)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	a := tel.hubs.attached[h]
	a.ClientConnected(h, &hub.Client{ID: "private-client-id"}, httptest.NewRequest("GET", "/private-url?secret=value", nil))
	a.Message(h, nil, hub.TrafficEvent{Direction: hub.Inbound, Bytes: 17, QueueDepth: -1})
	a.Message(h, nil, hub.TrafficEvent{Direction: hub.Outbound, Bytes: 19, QueueDepth: 3, Count: 4})
	a.Message(h, nil, hub.TrafficEvent{Direction: hub.Outbound, Bytes: 19, QueueDepth: -1, Written: true, Count: 2})
	a.Message(h, nil, hub.TrafficEvent{Direction: hub.Outbound, Binary: true, Dropped: true, QueueDepth: 256, Count: 3})
	a.Broadcast(h, 7, 3) // its drop value must not count the already observed losses again
	a.MessageRejected(h, nil, hub.MessageRateLimited)
	a.MessageRejected(h, nil, hub.MessageMalformed)
	a.Rejected(h, hub.RejectedCapacity)
	a.Rejected(h, hub.RejectionReason("accepted"))
	a.Handler(h, hub.HandlerEvent{Event: "input", Duration: time.Millisecond, Panicked: true})
	a.Handler(h, hub.HandlerEvent{Event: "private-event", Duration: time.Millisecond})
	a.RoundTrip(h, nil, 25*time.Millisecond)
	a.RoundTripTimeout(h, nil)
	a.ObserverPanicked(h)
	a.ClientDisconnected(h, nil, hub.DisconnectEvent{Reason: "server", AppReason: "idle"})
	a.ClientConnected(h, nil, nil)
	a.ClientDisconnected(h, nil, hub.DisconnectEvent{Reason: "server", AppReason: "private-reason"})
	checks := []struct {
		name   string
		labels []string
		value  uint64
	}{
		{"gosx_hub_messages_total", []string{"hub", "room", "direction", "in", "kind", "text"}, 1},
		{"gosx_hub_messages_total", []string{"hub", "room", "direction", "out", "kind", "text"}, 2},
		{"gosx_hub_message_bytes_total", []string{"hub", "room", "direction", "out", "kind", "text"}, 38},
		{"gosx_hub_drops_total", []string{"hub", "room", "kind", "binary"}, 3},
		{"gosx_hub_connections_total", []string{"hub", "room", "result", "accepted"}, 2},
		{"gosx_hub_connections_total", []string{"hub", "room", "result", "rejected_capacity"}, 1},
		{"gosx_hub_connections_total", []string{"hub", "room", "result", "other"}, 1},
		{"gosx_hub_disconnects_total", []string{"hub", "room", "reason", "idle"}, 1},
		{"gosx_hub_disconnects_total", []string{"hub", "room", "reason", "other"}, 1},
		{"gosx_hub_inbound_rate_limited_total", []string{"hub", "room"}, 1},
		{"gosx_hub_inbound_malformed_total", []string{"hub", "room"}, 1},
		{"gosx_hub_rtt_timeouts_total", []string{"hub", "room"}, 1},
		{"gosx_hub_handler_panics_total", []string{"hub", "room", "event", "input"}, 1},
		{"gosx_telemetry_dropped_total", []string{"reason", "observer_panic"}, 1},
	}
	for _, c := range checks {
		if got := hubSample(t, tel, c.name, c.labels...).Counter; got != c.value {
			t.Fatalf("%s %v: %d != %d", c.name, c.labels, got, c.value)
		}
	}
	if s := hubSample(t, tel, "gosx_hub_send_queue_depth", "hub", "room", "kind", "text").Histogram; s.Count != 4 || s.Sum != 12 {
		t.Fatal(s)
	}
	if s := hubSample(t, tel, "gosx_hub_send_queue_depth", "hub", "room", "kind", "binary").Histogram; s.Count != 3 || s.Sum != 768 {
		t.Fatal(s)
	}
	if s := hubSample(t, tel, "gosx_hub_rtt_seconds", "hub", "room").Histogram; s.Count != 1 || s.Sum != .025 {
		t.Fatal(s)
	}
	if s := hubSample(t, tel, "gosx_hub_handler_duration_seconds", "hub", "room", "event", "other").Histogram; s.Count != 1 {
		t.Fatal(s)
	}
	var text strings.Builder
	if err := tel.registry.WritePrometheus(&text); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text.String(), "private-") || strings.Contains(text.String(), "secret=value") {
		t.Fatal("unregistered connection data entered exposition")
	}
}

type hubProbe struct {
	hub.NoopObserver
	connected chan *hub.Client
}

func (p *hubProbe) ClientConnected(_ *hub.Hub, c *hub.Client, _ *http.Request) { p.connected <- c }

func TestHubGroupActualSuccessfulPayloadBytesAndShutdown(t *testing.T) {
	tel, app := hubTelemetry(t)
	g, _ := tel.NewHubGroup("room", HubOptions{Events: []string{"input"}, DisconnectReasons: []string{"idle"}, QueueSampleEvery: 1})
	h := hub.New("private-room-code")
	probe := &hubProbe{connected: make(chan *hub.Client, 1)}
	h.UseObserver(probe)
	h.On("input", func(c *hub.Context) { c.Hub.Send(c.Client.ID, "reply", 42) })
	if _, err := g.Attach(h); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UseShutdownSource("rooms", h); err != nil {
		t.Fatal(err)
	}
	app.Build()
	srv := httptest.NewServer(h)
	defer srv.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, welcome, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	client := <-probe.connected
	_ = client
	bad, good := []byte("invalid-json"), []byte(`{"event":"input","data":{}}`)
	if err := ws.WriteMessage(websocket.TextMessage, bad); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.TextMessage, good); err != nil {
		t.Fatal(err)
	}
	_, reply, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	hubWait(t, func() bool {
		return hubSample(t, tel, "gosx_hub_messages_total", "hub", "room", "direction", "out", "kind", "text").Counter == 2
	})
	if got := hubSample(t, tel, "gosx_hub_message_bytes_total", "hub", "room", "direction", "out", "kind", "text").Counter; got != uint64(len(welcome)+len(reply)) {
		t.Fatal("outbound bytes", got)
	}
	if got := hubSample(t, tel, "gosx_hub_message_bytes_total", "hub", "room", "direction", "in", "kind", "text").Counter; got != uint64(len(bad)+len(good)) {
		t.Fatal("inbound bytes", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if hubSample(t, tel, "gosx_hub_clients", "hub", "room").Gauge != 0 || hubSample(t, tel, "gosx_hub_instances", "hub", "room").Gauge != 0 {
		t.Fatal("close gauges remained live")
	}
	if hubSample(t, tel, "gosx_hub_disconnects_total", "hub", "room", "reason", "hub_closed").Counter != 1 {
		t.Fatal("hub drain was not observed before flush")
	}
}

func TestHubGroupSignalKeepsDrainCallbacksAndCloseOwnsDetach(t *testing.T) {
	t.Setenv("GOSX_TELEMETRY", "on")
	tel, err := Enable(server.New(), hubCoreOptions())
	if err != nil {
		t.Fatal(err)
	}
	wantDeadline := false
	t.Cleanup(func() {
		err := tel.Close(context.Background())
		if wantDeadline {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Error("lost shared Flush deadline", err)
			}
		} else if err != nil {
			t.Error(err)
		}
	})
	g, _ := tel.NewHubGroup("room", HubOptions{})
	h := hub.New("fixture")
	g.Attach(h)
	a := tel.hubs.attached[h]
	a.ClientConnected(h, nil, nil)
	tel.prepareShutdown(context.Background())
	if _, err := g.Attach(hub.New("later")); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	a.ClientDisconnected(h, nil, hub.DisconnectEvent{Reason: "hub_closed"})
	if hubSample(t, tel, "gosx_hub_disconnects_total", "hub", "room", "reason", "hub_closed").Counter != 1 {
		t.Fatal("signal dropped drain callbacks")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	original := a.detach
	a.detach = func() { close(entered); <-release; original() }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	wantDeadline = true
	if err := tel.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-entered
	select {
	case <-tel.done:
		t.Fatal("unfinished detach owner released")
	default:
	}
	close(release)
	if err := tel.Close(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if g.instances != 0 || g.clients != 0 {
		t.Fatal("detach lost gauge cleanup")
	}
}

func TestHubGroupGlobalCapsAndAccountedHeap(t *testing.T) {
	app := server.New()
	hubs := make([]*hub.Hub, 1025)
	for i := range hubs {
		hubs[i] = hub.New("fixture")
	}
	t.Setenv("GOSX_TELEMETRY", "on")
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	tel, err := Enable(app, hubCoreOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer tel.Close(context.Background())
	groups := make([]*HubGroup, 32)
	for i := range groups {
		groups[i], err = tel.NewHubGroup(fmt.Sprintf("kind_%d", i), HubOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tel.NewHubGroup("overflow", HubOptions{}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	for i := range 1024 {
		if _, err := groups[i%32].Attach(hubs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := groups[0].Attach(hubs[1024]); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if hubSample(t, tel, "gosx_telemetry_dropped_total", "reason", "series").Counter != 1 || hubSample(t, tel, "gosx_telemetry_dropped_total", "reason", "memory").Counter != 1 {
		t.Fatal("capacity failures were not counted")
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	charged := tel.registry.Usage().Bytes + tel.hubs.bytes.Load() + tel.ownerBytes
	if retained > charged {
		t.Fatalf("retained %d exceeds charged %d", retained, charged)
	}
	if tel.hubs.bytes.Load()+tel.ownerBytes > hubMiscBytes {
		t.Fatal("miscellaneous reservation exceeded")
	}
	t.Logf("retained=%d charged=%d", retained, charged)
	runtime.KeepAlive(hubs)
	runtime.KeepAlive(app)
}

func TestHubGroupConcurrentCallbacksDetachAndClose(t *testing.T) {
	tel, _ := hubTelemetry(t)
	g, _ := tel.NewHubGroup("room", HubOptions{})
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			h := hub.New("fixture")
			detach, err := g.Attach(h)
			if err != nil {
				t.Error(err)
				return
			}
			tel.hubs.mu.Lock()
			a := tel.hubs.attached[h]
			tel.hubs.mu.Unlock()
			var callbacks sync.WaitGroup
			callbacks.Go(func() {
				for range 500 {
					a.ClientConnected(h, nil, nil)
					a.Message(h, nil, hub.TrafficEvent{Direction: hub.Inbound, Bytes: 1, QueueDepth: -1})
					a.ClientDisconnected(h, nil, hub.DisconnectEvent{Reason: "peer_closed"})
				}
			})
			detach()
			h.Close(context.Background())
			callbacks.Wait()
		})
	}
	wg.Wait()
	if g.clients != 0 || g.instances != 0 || len(tel.hubs.attached) != 0 {
		t.Fatal("concurrent lifecycle retained gauges or attachments")
	}
}

func TestHubAccountingWarmAllocations(t *testing.T) {
	tel, _ := hubTelemetry(t)
	g, _ := tel.NewHubGroup("room", HubOptions{})
	h := hub.New("fixture")
	g.Attach(h)
	a := tel.hubs.attached[h]
	e := hub.TrafficEvent{Direction: hub.Outbound, Written: true, Bytes: 32, QueueDepth: -1}
	if n := testing.AllocsPerRun(1000, func() { a.Message(h, nil, e) }); n != 0 {
		t.Fatal("message allocations", n)
	}
	e = hub.TrafficEvent{Direction: hub.Outbound, Count: 256, QueueDepth: 128, Dropped: true}
	if n := testing.AllocsPerRun(1000, func() { a.Message(h, nil, e); a.Broadcast(h, 256, 256) }); n != 0 {
		t.Fatal("counted queue/broadcast allocations", n)
	}
}
