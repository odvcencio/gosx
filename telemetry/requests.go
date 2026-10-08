package telemetry

import (
	"strings"
	"sync/atomic"
	"time"

	"m31labs.dev/gosx/internal/observationcatalog"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/telemetry/metric"
)

var requestMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "other"}
var requestStatuses = []string{"1xx", "2xx", "3xx", "4xx", "5xx", "hijacked", "other"}

type requestKey struct{ kind, route string }
type requestMeters struct {
	requests [8][7]*metric.Counter
	duration *metric.Histogram
	bytes    *metric.Counter
}
type requestTable struct{ rows map[requestKey]*requestMeters }
type requestState struct {
	counts    *metric.CounterVec
	durations *metric.HistogramVec
	bytes     *metric.CounterVec
	inFlight  *metric.Gauge
	table     atomic.Pointer[requestTable]
	building  map[requestKey]*requestMeters // startup owner only, before publication
}

func (t *Telemetry) initializeRequests() error {
	s := &requestState{building: make(map[requestKey]*requestMeters)}
	labels := []metric.Label{{Name: "kind", MaxValues: 16}, {Name: "route", MaxValues: 1024}}
	var err error
	s.counts, err = t.counter("gosx_http_requests_total", append(append([]metric.Label(nil), labels...), enumLabel("method", requestMethods...), enumLabel("status_class", requestStatuses...))...)
	if err != nil {
		return err
	}
	s.durations, err = t.histogram("gosx_http_request_duration_seconds", requestBounds, labels...)
	if err != nil {
		return err
	}
	s.bytes, err = t.counter("gosx_http_response_bytes_total", labels...)
	if err != nil {
		return err
	}
	v, err := t.gauge("gosx_http_requests_in_flight")
	if err != nil {
		return err
	}
	s.inFlight, _ = v.Bind()
	t.requests = s
	for _, key := range []requestKey{{"public", "(public)"}, {"runtime", "(runtime)"}, {"isr", "(isr)"}, {"not_found", "(unmatched)"}, {"other", "(other)"}, {"hub", "(hub)"}} {
		if err := t.reserveRequest(server.ObservationPattern{Kind: key.kind, Pattern: key.route, Methods: []string{"*"}}); err != nil {
			return err
		}
	}
	return nil
}

func methodIndex(method string) int {
	for i, value := range requestMethods {
		if method == value {
			return i
		}
	}
	return 7
}

func admittedMethods(values []string) [8]bool {
	var methods [8]bool
	for _, value := range values {
		if value == "*" {
			for i := range methods {
				methods[i] = true
			}
			return methods
		}
		methods[methodIndex(value)] = true
		if value == "GET" {
			methods[1] = true
		}
	}
	return methods
}

// A page's derived error row has response counters for the statuses an error
// renderer can produce (including custom status codes, excluding hijack).
// Its duration shares the page histogram instead of reserving another.
// The page and error rows are one atomic admission, or both collapse to other.
func (t *Telemetry) reserveRequest(row server.ObservationPattern) error {
	s := t.requests
	key := requestKey{row.Kind, row.Pattern}
	if s.building[key] != nil {
		return nil
	}
	methods := admittedMethods(row.Methods)
	keys := []requestKey{key}
	if row.Kind == "page" {
		keys = append(keys, requestKey{"error", row.Pattern})
	}
	var batch []metric.TupleDeclaration
	for _, key := range keys {
		for method, admitted := range methods {
			if !admitted {
				continue
			}
			for status, class := range requestStatuses {
				if key.kind == "error" && status == 5 {
					continue
				}
				batch = append(batch, metric.TupleDeclaration{Instrument: s.counts, Values: []string{key.kind, key.route, requestMethods[method], class}})
			}
		}
		batch = append(batch, metric.TupleDeclaration{Instrument: s.bytes, Values: []string{key.kind, key.route}})
		if key.kind != "error" {
			batch = append(batch, metric.TupleDeclaration{Instrument: s.durations, Values: []string{key.kind, key.route}})
		}
	}
	charge := int64(0)
	for _, k := range keys {
		charge += 768 + int64(len(k.kind)+len(k.route))*2
	}
	if !t.reserveMisc(charge) {
		return ErrCapacity
	}
	if err := t.authority.DeclareBatch(batch); err != nil {
		t.releaseMisc(charge)
		return err
	}
	for _, key := range keys {
		m := &requestMeters{}
		for method, admitted := range methods {
			if !admitted {
				continue
			}
			for status, class := range requestStatuses {
				if key.kind == "error" && status == 5 {
					continue
				}
				m.requests[method][status], _ = s.counts.Bind(key.kind, key.route, requestMethods[method], class)
			}
		}
		m.bytes, _ = s.bytes.Bind(key.kind, key.route)
		if key.kind == "error" {
			m.duration = s.building[requestKey{"page", key.route}].duration
		} else {
			m.duration, _ = s.durations.Bind(key.kind, key.route)
		}
		s.building[requestKey{strings.Clone(key.kind), strings.Clone(key.route)}] = m
		t.adapterBytes.Add(768 + int64(len(key.kind)+len(key.route))*2)
	}
	return nil
}

func (t *Telemetry) admitRequestCatalog(rows []server.ObservationPattern) {
	if t.requests == nil || t.requests.table.Load() != nil {
		return
	}
	c := observationcatalog.New(t.opts.Metrics.MaxRoutePatterns)
	for _, row := range rows {
		c.Add(row)
	}
	bounded, overflow := c.Result()
	if overflow {
		t.core.dropped["series"].Add(1)
	}
	for _, row := range bounded {
		if row.Kind == "error" {
			continue
		}
		if err := t.reserveRequest(row); err != nil {
			t.core.dropped["series"].Add(1)
		}
	}
	t.requests.table.Store(&requestTable{rows: t.requests.building})
	t.requests.building = nil
}

// Owner integration supplies measured bytes/hijack state after the response
// writer seam lands. This hot path never binds or learns a request label.
func (t *Telemetry) observeRequest(kind, pattern, method string, status int, bytes int64, elapsed time.Duration, hijacked bool) {
	if !t.Enabled() || t.requests == nil {
		return
	}
	table := t.requests.table.Load()
	if table == nil {
		return
	}
	key := requestKey{kind, pattern}
	switch kind {
	case "public":
		key.route = "(public)"
	case "runtime":
		key.route = "(runtime)"
	case "isr":
		key.route = "(isr)"
	case "not_found", "missing":
		key = requestKey{"not_found", "(unmatched)"}
	}
	if hijacked && status == 0 {
		key = requestKey{"hub", "(hub)"}
	}
	m := table.rows[key]
	methodID, statusID := methodIndex(method), 6
	if status >= 100 && status < 600 {
		statusID = status/100 - 1
	}
	if hijacked {
		statusID = 5
	}
	if m == nil || m.requests[methodID][statusID] == nil {
		m = table.rows[requestKey{"other", "(other)"}]
		t.core.dropped["unknown_route"].Add(1)
	}
	m.requests[methodID][statusID].Add(1)
	if bytes > 0 {
		m.bytes.Add(uint64(bytes))
	}
	if elapsed < 0 {
		elapsed = 0
	}
	_ = m.duration.Observe(elapsed.Seconds())
}
