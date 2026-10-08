package telemetry

import (
	"runtime"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/clock"
	"m31labs.dev/gosx/internal/telemetryauthority"
	"m31labs.dev/gosx/telemetry/metric"
)

// Capture process initialization once, independently of any telemetry owner or
// injected application clock. Static Go packages initialize before main runs.
var processStartedAt = clock.New().Now().Wall

type coreMetrics struct {
	uptime, series, memory *metric.Gauge
	dropped                map[string]*metric.Counter
}

func (t *Telemetry) initializeRegistry() (err error) {
	if !t.reserveMisc(t.ownerBytes) {
		return ErrCapacity
	}
	t.registry, err = metric.NewRegistry(metric.RegistryOptions{MaxSeries: t.opts.Metrics.MaxSeries, MaxBytes: 8 << 20})
	if err != nil {
		return err
	}
	t.authority, err = metric.WithAuthority(t.registry, telemetryauthority.New())
	if err != nil {
		return err
	}
	for _, item := range []struct {
		name, help string
		dst        **metric.Gauge
	}{
		{"gosx_process_uptime_seconds", "Elapsed process lifetime in seconds.", &t.core.uptime},
		{"gosx_telemetry_series", "Reserved scalar sample lines.", &t.core.series},
		{"gosx_telemetry_memory_bytes", "Accounted retained telemetry bytes, excluding application state.", &t.core.memory},
	} {
		v, err := t.authority.NewGauge(metric.GaugeOptions{Name: item.name, Help: item.help})
		if err != nil {
			return err
		}
		*item.dst, err = v.Bind()
		if err != nil {
			return err
		}
	}
	start, err := t.authority.NewGauge(metric.GaugeOptions{Name: "process_start_time_seconds", Help: "Process initialization timestamp in Unix seconds."})
	if err != nil {
		return err
	}
	g, err := start.Bind()
	if err != nil {
		return err
	}
	g.Set(processStartSeconds())
	values := []string{gosx.Version, runtime.Version(), t.opts.Identity.App, t.opts.Identity.Version, t.opts.Identity.Revision, "unknown"}
	names := []string{"gosx_version", "go_version", "app", "app_version", "revision", "modified"}
	labels := make([]metric.Label, len(names))
	for i, name := range names {
		labels[i] = metric.Label{Name: name, Values: []string{values[i]}}
	}
	build, err := t.authority.NewGauge(metric.GaugeOptions{Name: "gosx_build_info", Help: "Declared build identity.", Labels: labels})
	if err != nil {
		return err
	}
	g, err = build.Bind(values...)
	if err != nil {
		return err
	}
	g.Set(1)
	reasons := []string{"series", "memory", "queue", "unknown_route", "unknown_label", "visit_cap", "hub_session_cap", "loop_instances", "activity_cap", "event_cap", "field_budget", "observer_panic", "clock"}
	dropped, err := t.authority.NewCounter(metric.CounterOptions{Name: "gosx_telemetry_dropped_total", Help: "Rejected telemetry operations by fixed reason.", Labels: []metric.Label{{Name: "reason", Values: reasons}}})
	if err != nil {
		return err
	}
	t.core.dropped = make(map[string]*metric.Counter, len(reasons))
	for _, reason := range reasons {
		c, err := dropped.Bind(reason)
		if err != nil {
			return err
		}
		t.core.dropped[reason] = c
	}
	if err := t.initializeHubs(); err != nil {
		return err
	}
	if err := t.initializeLoops(); err != nil {
		return err
	}
	if err := t.initializeDefaultAdapters(); err != nil {
		return err
	}
	t.updateCore(t.start)
	return nil
}

func processStartSeconds() float64 {
	return float64(processStartedAt.Unix()) + float64(processStartedAt.Nanosecond())/float64(time.Second)
}

func (t *Telemetry) updateCore(now Instant) {
	elapsed := now.Monotonic - t.start.Monotonic
	if elapsed < 0 {
		elapsed = 0
	}
	usage := t.registry.Usage()
	t.core.series.Set(float64(usage.Samples))
	// Fixed owner/channel/clock state is reserved independently of the registry.
	bytes := t.ownerBytes
	bytes += t.adapterBytes.Load()
	if t.hubs != nil {
		bytes += t.hubs.bytes.Load()
	}
	if t.loops != nil {
		bytes += t.loops.bytes.Load()
	}
	if t.activities != nil {
		bytes += t.activities.bytes.Load()
	}
	t.core.memory.Set(float64(usage.Bytes + bytes))
	// Publish the collection time after refreshing its usage gauges.
	t.core.uptime.Set(elapsed.Seconds())
}
