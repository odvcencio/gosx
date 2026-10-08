package telemetry

import (
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/internal/telemetryauthority"
	"m31labs.dev/gosx/telemetry/metric"
)

// HubOptions declares finite handler/reason labels. Session recording follows
// global consent policy when available; browser ack sampling is not yet supported.
type HubOptions struct {
	Events, DisconnectReasons []string
	DisableSessions           bool
	ClientAckSampleRate       float64
	QueueSampleEvery          uint32
}

// HubGroup registers one kind before App.Build. Attach accepts dynamic hub
// instances, before their first upgrade, and never owns their connections.
type HubGroup struct {
	owner              *Telemetry
	kind               string
	every              uint32
	metrics            hubInstruments
	instances, clients int64 // protected by hubState.mu
}

type hubHandler struct {
	duration *metric.Histogram
	panics   *metric.Counter
}
type hubInstruments struct {
	instances, clients                          *metric.Gauge
	connections, disconnects                    map[string]*metric.Counter
	messages, bytes                             [2][2]*metric.Counter
	drops                                       [2]*metric.Counter
	queue                                       [2]*metric.Histogram
	handlers                                    map[string]hubHandler
	broadcasts, slow, rate, malformed, timeouts *metric.Counter
	recipients, rtt                             *metric.Histogram
}
type hubVectors struct {
	instances, clients                                              *metric.GaugeVec
	connections, disconnects, messages, bytes, drops, handlerPanics *metric.CounterVec
	broadcasts, slow, rate, malformed, timeouts                     *metric.CounterVec
	recipients, queue, handlers, rtt                                *metric.HistogramVec
}
type hubState struct {
	mu       sync.Mutex
	groups   map[string]*HubGroup
	attached map[*hub.Hub]*hubAttachment
	vectors  hubVectors
	bytes    atomic.Int64
}
type hubAttachment struct {
	hub.NoopObserver
	group   *HubGroup
	h       *hub.Hub
	detach  func()
	alive   atomic.Bool
	clients int64 // protected by hubState.mu; no client/request pointers retained
}

const hubStateBytes = int64(128 << 10) // map capacities, vectors, and owner metadata
const hubAttachmentBytes = int64(512)  // subscriber/dispatch entries and removal closure
const hubMiscBytes = int64(2 << 20)

var hubResults = []string{"accepted", "rejected_origin", "rejected_rate", "rejected_capacity", "rejected_upgrade", "hub_closed", "other"}
var hubReasons = []string{"peer_closed", "read_error", "read_timeout", "write_error", "rate_limited", "slow_client", "server", "hub_closed", "panic", "other"}
var hubHandlerBounds = []float64{.0001, .00025, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, 1}
var hubRTTBounds = []float64{.005, .01, .025, .05, .1, .2, .35, .5, 1, 2, 5, 10}

func (t *Telemetry) initializeHubs() error {
	if !t.reserveMisc(hubStateBytes) {
		return ErrCapacity
	}
	s := &hubState{groups: make(map[string]*HubGroup, 32), attached: make(map[*hub.Hub]*hubAttachment, 1024)}
	v := &s.vectors
	label := []metric.Label{{Name: "hub", MaxValues: 33}}
	for _, d := range []struct {
		name, help string
		dst        **metric.GaugeVec
	}{
		{"gosx_hub_instances", "Attached hub instances.", &v.instances},
		{"gosx_hub_clients", "Connected clients on attached hubs.", &v.clients},
	} {
		x, err := t.authority.NewGauge(metric.GaugeOptions{Name: d.name, Help: d.help, Labels: label})
		if err != nil {
			return err
		}
		*d.dst = x
	}
	for _, d := range []struct {
		name, help string
		extra      []metric.Label
		dst        **metric.CounterVec
	}{
		{"gosx_hub_connections_total", "Accepted and rejected connection attempts.", []metric.Label{{Name: "result", Values: hubResults}}, &v.connections},
		{"gosx_hub_disconnects_total", "Connection cleanup by declared reason.", []metric.Label{{Name: "reason", MaxValues: 1024}}, &v.disconnects},
		{"gosx_hub_messages_total", "Successful logical reads and writes.", trafficLabels(), &v.messages},
		{"gosx_hub_message_bytes_total", "Logical payload bytes excluding framing and control traffic.", trafficLabels(), &v.bytes},
		{"gosx_hub_drops_total", "Full outbound queue losses.", []metric.Label{{Name: "kind", Values: []string{"text", "binary"}}}, &v.drops},
		{"gosx_hub_handler_panics_total", "Recovered application handler panics.", []metric.Label{{Name: "event", MaxValues: 1024}}, &v.handlerPanics},
		{"gosx_hub_broadcasts_total", "Broadcast calls.", nil, &v.broadcasts},
		{"gosx_hub_slow_client_evictions_total", "Connections evicted by interval drop policy.", nil, &v.slow},
		{"gosx_hub_inbound_rate_limited_total", "Payloads rejected by the inbound rate limit.", nil, &v.rate},
		{"gosx_hub_inbound_malformed_total", "Malformed logical payloads.", nil, &v.malformed},
		{"gosx_hub_rtt_timeouts_total", "Unanswered or expired measured control pings.", nil, &v.timeouts},
	} {
		x, err := t.authority.NewCounter(metric.CounterOptions{Name: d.name, Help: d.help, Labels: append(append([]metric.Label(nil), label...), d.extra...)})
		if err != nil {
			return err
		}
		*d.dst = x
	}
	for _, d := range []struct {
		name, help string
		extra      []metric.Label
		bounds     []float64
		dst        **metric.HistogramVec
	}{
		{"gosx_hub_broadcast_recipients", "Accepted enqueues per broadcast.", nil, []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024}, &v.recipients},
		{"gosx_hub_send_queue_depth", "Sampled outbound queue depth.", []metric.Label{{Name: "kind", Values: []string{"text", "binary"}}}, []float64{0, 1, 2, 4, 8, 16, 32, 64, 128, 256}, &v.queue},
		{"gosx_hub_handler_duration_seconds", "Whole application handler duration.", []metric.Label{{Name: "event", MaxValues: 1024}}, hubHandlerBounds, &v.handlers},
		{"gosx_hub_rtt_seconds", "Matched native control ping round-trip duration.", nil, hubRTTBounds, &v.rtt},
	} {
		x, err := t.authority.NewHistogram(metric.HistogramOptions{Name: d.name, Help: d.help, Labels: append(append([]metric.Label(nil), label...), d.extra...), Bounds: d.bounds})
		if err != nil {
			return err
		}
		*d.dst = x
	}
	t.hubs = s
	s.bytes.Store(hubStateBytes)
	_, err := t.bindHub("other", nil, nil)
	return err
}

func trafficLabels() []metric.Label {
	return []metric.Label{{Name: "direction", Values: []string{"in", "out"}}, {Name: "kind", Values: []string{"text", "binary"}}}
}

// bindHub reserves the entire kind atomically before any handle is published.
func (t *Telemetry) bindHub(kind string, events, reasons []string) (hubInstruments, error) {
	v := &t.hubs.vectors
	var batch []metric.TupleDeclaration
	add := func(instrument metric.InstrumentVec, values ...string) {
		batch = append(batch, metric.TupleDeclaration{Instrument: instrument, Values: values})
	}
	for _, x := range []metric.InstrumentVec{v.instances, v.clients, v.broadcasts, v.slow, v.rate, v.malformed, v.timeouts, v.recipients, v.rtt} {
		add(x, kind)
	}
	for _, r := range hubResults {
		add(v.connections, kind, r)
	}
	for _, r := range uniqueHubValues(append(append([]string(nil), hubReasons...), reasons...)) {
		add(v.disconnects, kind, r)
	}
	for _, e := range uniqueHubValues(append(append([]string(nil), events...), "other")) {
		add(v.handlers, kind, e)
		add(v.handlerPanics, kind, e)
	}
	for _, k := range []string{"text", "binary"} {
		add(v.drops, kind, k)
		add(v.queue, kind, k)
		for _, d := range []string{"in", "out"} {
			add(v.messages, kind, d, k)
			add(v.bytes, kind, d, k)
		}
	}
	if err := t.authority.DeclareBatch(batch); err != nil {
		return hubInstruments{}, err
	}
	m := hubInstruments{connections: make(map[string]*metric.Counter, 7), disconnects: make(map[string]*metric.Counter, len(reasons)+len(hubReasons)), handlers: make(map[string]hubHandler, len(events)+1)}
	m.instances, _ = v.instances.Bind(kind)
	m.clients, _ = v.clients.Bind(kind)
	m.broadcasts, _ = v.broadcasts.Bind(kind)
	m.slow, _ = v.slow.Bind(kind)
	m.rate, _ = v.rate.Bind(kind)
	m.malformed, _ = v.malformed.Bind(kind)
	m.timeouts, _ = v.timeouts.Bind(kind)
	m.recipients, _ = v.recipients.Bind(kind)
	m.rtt, _ = v.rtt.Bind(kind)
	for _, r := range hubResults {
		m.connections[r], _ = v.connections.Bind(kind, r)
	}
	for _, r := range uniqueHubValues(append(append([]string(nil), hubReasons...), reasons...)) {
		m.disconnects[strings.Clone(r)], _ = v.disconnects.Bind(kind, r)
	}
	for _, e := range uniqueHubValues(append(append([]string(nil), events...), "other")) {
		d, _ := v.handlers.Bind(kind, e)
		p, _ := v.handlerPanics.Bind(kind, e)
		m.handlers[strings.Clone(e)] = hubHandler{d, p}
	}
	for k, name := range []string{"text", "binary"} {
		m.drops[k], _ = v.drops.Bind(kind, name)
		m.queue[k], _ = v.queue.Bind(kind, name)
		for d, direction := range []string{"in", "out"} {
			m.messages[d][k], _ = v.messages.Bind(kind, direction, name)
			m.bytes[d][k], _ = v.bytes.Bind(kind, direction, name)
		}
	}
	return m, nil
}

func uniqueHubValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		found := false
		for _, old := range out {
			if v == old {
				found = true
				break
			}
		}
		if !found {
			out = append(out, v)
		}
	}
	return out
}

func validateHubValues(values []string, limit int) error {
	if len(values) > limit {
		return invalid("hub_labels", "capacity")
	}
	for i, v := range values {
		if v == "" || len(v) > 128 || !utf8.ValidString(v) || strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return invalid("hub_labels", "text")
		}
		for _, old := range values[:i] {
			if old == v {
				return invalid("hub_labels", "duplicate")
			}
		}
	}
	return nil
}

func (t *Telemetry) NewHubGroup(kind string, opts HubOptions) (*HubGroup, error) {
	if !kindName(kind) {
		return nil, invalid("hub_kind", "name")
	}
	if err := validateHubValues(opts.Events, 64); err != nil {
		return nil, err
	}
	if err := validateHubValues(opts.DisconnectReasons, 32); err != nil {
		return nil, err
	}
	if math.IsNaN(opts.ClientAckSampleRate) || math.IsInf(opts.ClientAckSampleRate, 0) || opts.ClientAckSampleRate < 0 || opts.ClientAckSampleRate > 1 {
		return nil, invalid("hub_ack", "range")
	}
	if opts.ClientAckSampleRate > 0 {
		return nil, invalid("hub_ack", "unsupported")
	}
	if t == nil || t.hubs == nil {
		return &HubGroup{}, nil
	}
	s := t.hubs
	s.mu.Lock()
	defer s.mu.Unlock()
	if !t.Enabled() {
		return nil, ErrClosed
	}
	if s.groups[kind] != nil {
		return nil, ErrConflict
	}
	if len(s.groups) == 32 {
		t.core.dropped["series"].Add(1)
		return nil, ErrCapacity
	}
	charge := int64(16<<10) + int64(len(kind))
	for _, list := range [][]string{opts.Events, opts.DisconnectReasons} {
		for _, value := range list {
			charge += int64(32 + len(value))
		}
	}
	if !t.reserveMisc(charge) {
		t.core.dropped["memory"].Add(1)
		return nil, ErrCapacity
	}
	m, err := t.bindHub(kind, opts.Events, opts.DisconnectReasons)
	if err != nil {
		t.releaseMisc(charge)
		if err == ErrCapacity {
			t.core.dropped["series"].Add(1)
		}
		return nil, err
	}
	every := opts.QueueSampleEvery
	if every == 0 {
		every = 64
	}
	g := &HubGroup{owner: t, kind: strings.Clone(kind), every: every, metrics: m}
	s.groups[g.kind] = g
	s.bytes.Add(charge)
	return g, nil
}

func (g *HubGroup) Attach(h *hub.Hub) (func(), error) {
	if h == nil {
		return nil, invalid("hub", "required")
	}
	if g == nil || g.owner == nil {
		return func() {}, nil
	}
	t := g.owner
	s := t.hubs
	s.mu.Lock()
	defer s.mu.Unlock()
	if !t.Enabled() {
		return nil, ErrClosed
	}
	if s.attached[h] != nil {
		return nil, ErrConflict
	}
	if len(s.attached) == 1024 {
		t.core.dropped["memory"].Add(1)
		return nil, ErrCapacity
	}
	if !t.reserveMisc(hubAttachmentBytes) {
		t.core.dropped["memory"].Add(1)
		return nil, ErrCapacity
	}
	a := &hubAttachment{group: g, h: h}
	a.alive.Store(true)
	detach, err := h.UseTelemetryObserver(a, g.every, telemetryauthority.New())
	if err != nil {
		t.releaseMisc(hubAttachmentBytes)
		if err == hub.ErrObserverConflict {
			return nil, ErrConflict
		}
		return nil, err
	}
	a.detach = detach
	s.attached[h] = a
	s.bytes.Add(hubAttachmentBytes)
	g.instances++
	g.metrics.instances.Set(float64(g.instances))
	return a.finish, nil
}

func (a *hubAttachment) finish() {
	s := a.group.owner.hubs
	s.mu.Lock()
	if !a.alive.Swap(false) {
		s.mu.Unlock()
		return
	}
	g := a.group
	g.instances--
	g.clients -= a.clients
	a.clients = 0
	g.metrics.instances.Set(float64(g.instances))
	g.metrics.clients.Set(float64(g.clients))
	delete(s.attached, a.h)
	s.bytes.Add(-hubAttachmentBytes)
	g.owner.releaseMisc(hubAttachmentBytes)
	detach := a.detach
	a.detach = nil
	a.h = nil
	s.mu.Unlock()
	if detach != nil {
		detach()
	}
}

func (t *Telemetry) releaseHubs() {
	if t.hubs == nil {
		return
	}
	for {
		s := t.hubs
		s.mu.Lock()
		var next *hubAttachment
		for _, a := range s.attached {
			next = a
			break
		}
		s.mu.Unlock()
		if next == nil {
			return
		}
		next.finish()
	}
}

func (a *hubAttachment) Closed(*hub.Hub) { a.finish() }
func (a *hubAttachment) ClientConnected(_ *hub.Hub, _ *hub.Client, _ *http.Request) {
	s := a.group.owner.hubs
	s.mu.Lock()
	defer s.mu.Unlock()
	if !a.alive.Load() {
		return
	}
	a.clients++
	a.group.clients++
	a.group.metrics.clients.Set(float64(a.group.clients))
	a.group.metrics.connections["accepted"].Add(1)
}
func (a *hubAttachment) ClientDisconnected(_ *hub.Hub, _ *hub.Client, e hub.DisconnectEvent) {
	s := a.group.owner.hubs
	s.mu.Lock()
	defer s.mu.Unlock()
	if !a.alive.Load() {
		return
	}
	if a.clients > 0 {
		a.clients--
		a.group.clients--
		a.group.metrics.clients.Set(float64(a.group.clients))
	}
	reason := e.Reason
	if e.AppReason != "" {
		reason = e.AppReason
	}
	c := a.group.metrics.disconnects[reason]
	if c == nil {
		c = a.group.metrics.disconnects["other"]
	}
	c.Add(1)
	if e.Reason == "slow_client" {
		a.group.metrics.slow.Add(1)
	}
}
func (a *hubAttachment) Rejected(_ *hub.Hub, r hub.RejectionReason) {
	if !a.alive.Load() {
		return
	}
	reason := "other"
	switch r {
	case hub.RejectedOrigin, hub.RejectedCapacity, hub.RejectedUpgrade, hub.RejectedClosed:
		reason = string(r)
	}
	c := a.group.metrics.connections[reason]
	if c == nil {
		c = a.group.metrics.connections["other"]
	}
	c.Add(1)
}
func (a *hubAttachment) Message(_ *hub.Hub, _ *hub.Client, e hub.TrafficEvent) {
	if !a.alive.Load() {
		return
	}
	m := &a.group.metrics
	k := 0
	if e.Binary {
		k = 1
	}
	n := e.Count
	if n == 0 {
		n = 1
	}
	if e.Direction == hub.Inbound || e.Direction == hub.Outbound && e.Written {
		if e.Bytes < 0 {
			return
		}
		d := 0
		if e.Direction == hub.Outbound {
			d = 1
		}
		m.messages[d][k].Add(n)
		size := uint64(e.Bytes)
		if size != 0 && n > ^uint64(0)/size {
			size = ^uint64(0)
		} else {
			size *= n
		}
		m.bytes[d][k].Add(size)
	}
	if e.Direction == hub.Outbound && !e.Written {
		if e.Dropped {
			m.drops[k].Add(n)
		}
		if e.QueueDepth >= 0 && e.QueueDepth <= 256 {
			a.observe(m.queue[k], float64(e.QueueDepth), n)
		}
	}
}
func (a *hubAttachment) MessageRejected(_ *hub.Hub, _ *hub.Client, r hub.MessageRejectionReason) {
	if !a.alive.Load() {
		return
	}
	if r == hub.MessageRateLimited {
		a.group.metrics.rate.Add(1)
	} else if r == hub.MessageMalformed {
		a.group.metrics.malformed.Add(1)
	}
}
func (a *hubAttachment) Broadcast(_ *hub.Hub, sent, _ int) {
	if !a.alive.Load() || sent < 0 {
		return
	}
	a.group.metrics.broadcasts.Add(1)
	a.observe(a.group.metrics.recipients, float64(sent), 1)
}
func (a *hubAttachment) Handler(_ *hub.Hub, e hub.HandlerEvent) {
	if !a.alive.Load() || e.Duration < 0 {
		return
	}
	m, ok := a.group.metrics.handlers[e.Event]
	if !ok {
		m = a.group.metrics.handlers["other"]
	}
	a.observe(m.duration, e.Duration.Seconds(), 1)
	if e.Panicked {
		m.panics.Add(1)
	}
}
func (a *hubAttachment) RoundTrip(_ *hub.Hub, _ *hub.Client, d time.Duration) {
	if a.alive.Load() && d >= 0 && d <= 60*time.Second {
		a.observe(a.group.metrics.rtt, d.Seconds(), 1)
	}
}
func (a *hubAttachment) RoundTripTimeout(*hub.Hub, *hub.Client) {
	if a.alive.Load() {
		a.group.metrics.timeouts.Add(1)
	}
}
func (a *hubAttachment) ObserverPanicked(*hub.Hub) {
	if a.alive.Load() {
		a.group.owner.core.dropped["observer_panic"].Add(1)
	}
}
func (a *hubAttachment) observe(h *metric.Histogram, value float64, n uint64) {
	if h.ObserveN(value, n) != nil {
		a.group.owner.core.dropped["series"].Add(1)
	}
}
