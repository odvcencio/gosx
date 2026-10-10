package telemetry

import "m31labs.dev/gosx/telemetry/metric"

// The activity lifecycle uses these shared, fixed-shape families. Reserve the
// complete kind before the route catalog can consume the remaining capacity.
func (t *Telemetry) initializeActivityMetrics() error {
	kind := metric.Label{Name: "kind", MaxValues: 33}
	dims := []metric.Label{kind, {Name: "dim0", MaxValues: 514}, {Name: "dim1", MaxValues: 514}}
	for _, d := range []struct {
		name   string
		labels []metric.Label
	}{
		{"gosx_activities_started_total", dims},
		{"gosx_activities_finished_total", append(append([]metric.Label(nil), dims...), metric.Label{Name: "outcome", MaxValues: 1024})},
		{"gosx_activity_participant_leaves_total", []metric.Label{kind, {Name: "reason", MaxValues: 1024}}},
		{"gosx_activity_participant_reconnects_total", []metric.Label{kind}},
		{"gosx_activities_interrupted_total", []metric.Label{kind}},
		{"gosx_activity_records_total", []metric.Label{kind, enumLabel("state", "open", "final"), enumLabel("result", "queued", "committed", "failed", "disabled")}},
	} {
		if _, err := t.counter(d.name, d.labels...); err != nil {
			return err
		}
	}
	if _, err := t.gauge("gosx_activities_open", kind); err != nil {
		return err
	}
	if _, err := t.gauge("gosx_activity_dimension_info", kind, enumLabel("slot", "dim0", "dim1"), metric.Label{Name: "name", MaxValues: 66}); err != nil {
		return err
	}
	if _, err := t.histogram("gosx_activity_duration_seconds", activityBounds, kind, metric.Label{Name: "outcome", MaxValues: 1024}); err != nil {
		return err
	}
	if err := t.bindActivityMetrics("other", [2]string{"none", "none"}, [][2]string{{"none", "none"}}, nil, nil); err != nil {
		return err
	}
	return t.initializeActivities()
}

func (t *Telemetry) bindActivityMetrics(kind string, names [2]string, combinations [][2]string, outcomes, reasons []string) error {
	outcomes = uniqueHubValues(append(append([]string(nil), outcomes...), "interrupted", "other"))
	reasons = uniqueHubValues(append(append([]string(nil), reasons...), "process_restart", "server_shutdown", "idle", "other"))
	var batch []metric.TupleDeclaration
	add := func(v metric.InstrumentVec, values ...string) {
		batch = append(batch, metric.TupleDeclaration{Instrument: v, Values: values})
	}
	for _, combo := range append(append([][2]string(nil), combinations...), [2]string{"other", "other"}) {
		add(t.adapters.counters["gosx_activities_started_total"], kind, combo[0], combo[1])
		for _, outcome := range outcomes {
			add(t.adapters.counters["gosx_activities_finished_total"], kind, combo[0], combo[1], outcome)
		}
	}
	for i, slot := range []string{"dim0", "dim1"} {
		add(t.adapters.gauges["gosx_activity_dimension_info"], kind, slot, names[i])
	}
	add(t.adapters.gauges["gosx_activities_open"], kind)
	add(t.adapters.counters["gosx_activity_participant_reconnects_total"], kind)
	add(t.adapters.counters["gosx_activities_interrupted_total"], kind)
	for _, reason := range reasons {
		add(t.adapters.counters["gosx_activity_participant_leaves_total"], kind, reason)
	}
	for _, outcome := range outcomes {
		add(t.adapters.histograms["gosx_activity_duration_seconds"], kind, outcome)
	}
	for _, state := range []string{"open", "final"} {
		for _, result := range []string{"queued", "committed", "failed", "disabled"} {
			add(t.adapters.counters["gosx_activity_records_total"], kind, state, result)
		}
	}
	if err := t.authority.DeclareBatch(batch); err != nil {
		return err
	}
	for i, slot := range []string{"dim0", "dim1"} {
		g, _ := t.adapters.gauges["gosx_activity_dimension_info"].Bind(kind, slot, names[i])
		_ = g.Set(1)
	}
	return nil
}
