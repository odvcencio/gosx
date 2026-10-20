package telemetry

import (
	"m31labs.dev/gosx/telemetry/metric"
)

// adapterVectors owns the finite default inventory. Framework owner callbacks
// attach at their delivery seams; selecting an unavailable callback still fails
// closed in Enable. Route tuples are admitted last, during the catalog callback.
type adapterVectors struct {
	counters   map[string]*metric.CounterVec
	gauges     map[string]*metric.GaugeVec
	histograms map[string]*metric.HistogramVec
}

var requestBounds = []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
var activityBounds = []float64{1, 5, 15, 30, 60, 120, 300, 600, 900, 1200, 1800, 3600, 7200}
var operationStatuses = []string{"ok", "error", "cancelled", "timeout", "other"}
var clientCategories = []string{"runtime", "navigation", "action", "region", "runtime-dom", "stream", "runtime-surface", "island", "compute-island", "engine", "video-sync", "scene3d", "scene3d-webgpu", "hub", "visit", "vitals", "client-health", "other"}
var batchResults = []string{"accepted", "rejected_body", "rejected_json", "rejected_rate", "rejected_origin", "rejected_method", "disabled", "consent_declined"}
var frameworkDegraded = []string{"telemetry", "persistence", "upload", "clock"}

func enumLabel(name string, values ...string) metric.Label {
	return metric.Label{Name: name, Values: values, MaxValues: 1024}
}

func (t *Telemetry) counter(name string, labels ...metric.Label) (*metric.CounterVec, error) {
	v, err := t.authority.NewCounter(metric.CounterOptions{Name: name, Labels: labels})
	if err == nil {
		t.adapters.counters[name] = v
	}
	return v, err
}
func (t *Telemetry) gauge(name string, labels ...metric.Label) (*metric.GaugeVec, error) {
	v, err := t.authority.NewGauge(metric.GaugeOptions{Name: name, Labels: labels})
	if err == nil {
		t.adapters.gauges[name] = v
	}
	return v, err
}
func (t *Telemetry) histogram(name string, bounds []float64, labels ...metric.Label) (*metric.HistogramVec, error) {
	v, err := t.authority.NewHistogram(metric.HistogramOptions{Name: name, Bounds: bounds, Labels: labels})
	if err == nil {
		t.adapters.histograms[name] = v
	}
	return v, err
}

// Only a fixed, bounded inventory uses Cartesian admission. Application kind
// combinations and route methods use explicit tuples instead.
func (t *Telemetry) declareProduct(v metric.InstrumentVec, domains ...[]string) error {
	var batch []metric.TupleDeclaration
	var walk func(int, []string)
	walk = func(i int, values []string) {
		if i == len(domains) {
			batch = append(batch, metric.TupleDeclaration{Instrument: v, Values: append([]string(nil), values...)})
			return
		}
		for _, value := range domains[i] {
			walk(i+1, append(values, value))
		}
	}
	walk(0, nil)
	return t.authority.DeclareBatch(batch)
}

func (t *Telemetry) initializeDefaultAdapters() error {
	if !t.reserveMisc(64 << 10) {
		return ErrCapacity
	}
	t.adapters = adapterVectors{make(map[string]*metric.CounterVec), make(map[string]*metric.GaugeVec), make(map[string]*metric.HistogramVec)}
	// Account the vector maps, pre-bound lookup tables and immutable route index
	// separately from the registry's descriptors/cells/snapshot arenas.
	t.adapterBytes.Store(64 << 10)
	if err := t.initializeAuth(); err != nil {
		return err
	}
	if err := t.initializeDegraded(); err != nil {
		return err
	}
	if !t.opts.Metrics.DisableOperations {
		if err := t.initializeOperations(); err != nil {
			return err
		}
	}
	if !t.opts.Metrics.DisableClientEvents {
		categories := uniqueHubValues(append(append([]string(nil), clientCategories...), t.opts.ClientEvents.Categories...))
		levels := []string{"debug", "info", "warn", "error"}
		v, err := t.counter("gosx_client_events_total", enumLabel("category", categories...), enumLabel("level", levels...))
		if err != nil {
			return err
		}
		if err := t.declareProduct(v, categories, levels); err != nil {
			return err
		}
		v, err = t.counter("gosx_client_event_batches_total", enumLabel("result", batchResults...))
		if err != nil {
			return err
		}
		if err := t.declareProduct(v, batchResults); err != nil {
			return err
		}
	}
	if !t.opts.Metrics.DisableRuntime {
		for _, name := range []string{"gosx_go_goroutines", "gosx_go_heap_alloc_bytes", "gosx_go_heap_objects"} {
			if _, err := t.gauge(name); err != nil {
				return err
			}
		}
		for _, name := range []string{"gosx_go_gc_cycles_total", "gosx_go_gc_pause_cpu_seconds_total", "gosx_go_cpu_user_seconds_total", "gosx_go_cpu_available_seconds_total"} {
			if _, err := t.counter(name); err != nil {
				return err
			}
		}
	}
	if !t.opts.Metrics.DisableReadiness {
		for _, name := range []string{"gosx_readiness_known", "gosx_readiness_last_ok", "gosx_readiness_last_evaluated_timestamp_seconds"} {
			if _, err := t.gauge(name); err != nil {
				return err
			}
		}
		checks := uniqueHubValues(append(append([]string(nil), t.opts.Metrics.ReadinessChecks...), "other"))
		v, err := t.gauge("gosx_readiness_check_ok", enumLabel("check", checks...))
		if err != nil {
			return err
		}
		if err := t.declareProduct(v, checks); err != nil {
			return err
		}
	}
	if !t.opts.Metrics.DisableScheduled {
		tasks := uniqueHubValues(append(append([]string(nil), t.opts.Metrics.ScheduledTasks...), "other"))
		for _, name := range []string{"gosx_scheduled_task_running", "gosx_scheduled_task_last_success_timestamp_seconds", "gosx_scheduled_task_next_due_timestamp_seconds"} {
			v, err := t.gauge(name, enumLabel("task", tasks...))
			if err != nil {
				return err
			}
			if err := t.declareProduct(v, tasks); err != nil {
				return err
			}
		}
	}
	if !t.opts.Activities.Disabled || t.opts.Persistence.Enabled || t.opts.Sessions.Enabled {
		for _, name := range []string{"gosx_telemetry_queue_bytes", "gosx_telemetry_queue_records", "gosx_telemetry_pending_finals"} {
			if _, err := t.gauge(name); err != nil {
				return err
			}
		}
	}
	if t.opts.Listen.Addr != "off" {
		if _, err := t.gauge("gosx_telemetry_insecure_metrics_listener"); err != nil {
			return err
		}
		if _, err := t.histogram("gosx_telemetry_scrape_duration_seconds", requestBounds); err != nil {
			return err
		}
	}
	v, err := t.counter("gosx_telemetry_clock_adjustments_total", enumLabel("source", "server", "client"))
	if err != nil {
		return err
	}
	if err := t.declareProduct(v, []string{"server", "client"}); err != nil {
		return err
	}
	if !t.opts.Activities.Disabled {
		if err := t.initializeActivityMetrics(); err != nil {
			return err
		}
	}
	if !t.opts.Metrics.DisableRequests {
		return t.initializeRequests()
	}
	return nil
}
