package telemetry

import (
	"m31labs.dev/gosx/telemetry/metric"
	"m31labs.dev/gosx/telemetry/schema"
)

type activityComboMeters struct {
	tuple    [2]string
	started  *metric.Counter
	finished [18]*metric.Counter
}
type activityMeters struct {
	open         *metric.Gauge
	combinations []activityComboMeters
	outcomes     []string
	durations    [18]*metric.Histogram
	interrupted  *metric.Counter
	records      [2][4]*metric.Counter
	reconnects   *metric.Counter
	leaves       map[string]*metric.Counter
}

func (k *activityKindCore) bindActivityMeters() {
	t := k.owner
	m := &k.meters
	m.reconnects, _ = t.adapters.counters["gosx_activity_participant_reconnects_total"].Bind(k.name)
	m.leaves = make(map[string]*metric.Counter, len(k.reasons)+4)
	for _, reason := range uniqueHubValues(append(append([]string(nil), k.reasons...), "process_restart", "server_shutdown", "idle", "other")) {
		m.leaves[reason], _ = t.adapters.counters["gosx_activity_participant_leaves_total"].Bind(k.name, reason)
	}
	m.outcomes = uniqueHubValues(append(append([]string(nil), k.outcomes...), "interrupted", "other"))
	m.open, _ = t.adapters.gauges["gosx_activities_open"].Bind(k.name)
	for _, combo := range append(append([][2]string(nil), k.combinations...), [2]string{"other", "other"}) {
		row := activityComboMeters{tuple: combo}
		row.started, _ = t.adapters.counters["gosx_activities_started_total"].Bind(k.name, combo[0], combo[1])
		for i, outcome := range m.outcomes {
			row.finished[i], _ = t.adapters.counters["gosx_activities_finished_total"].Bind(k.name, combo[0], combo[1], outcome)
		}
		m.combinations = append(m.combinations, row)
	}
	for i, outcome := range m.outcomes {
		m.durations[i], _ = t.adapters.histograms["gosx_activity_duration_seconds"].Bind(k.name, outcome)
	}
	m.interrupted, _ = t.adapters.counters["gosx_activities_interrupted_total"].Bind(k.name)
	for i, state := range []string{"open", "final"} {
		for j, result := range []string{"queued", "committed", "failed", "disabled"} {
			m.records[i][j], _ = t.adapters.counters["gosx_activity_records_total"].Bind(k.name, state, result)
		}
	}
}

// Start is called under the activity owner lock after successful publication.
func (t *Telemetry) activityStartMetric(k *activityKindCore, tuple [2]string) {
	k.open++
	k.meters.open.Set(float64(k.open))
	for _, m := range k.meters.combinations {
		if m.tuple == tuple {
			m.started.Add(1)
			break
		}
	}
	k.meters.records[0][1].Add(1)
}
func (t *Telemetry) activityFinishMetric(e *activityEntity, v schema.Activity) {
	k := e.kind
	s := t.activities
	s.mu.Lock()
	k.open--
	k.meters.open.Set(float64(k.open))
	s.mu.Unlock()
	for i, outcome := range k.meters.outcomes {
		if outcome == v.Outcome {
			for _, m := range k.meters.combinations {
				if m.tuple == e.tuple {
					m.finished[i].Add(1)
					break
				}
			}
			k.meters.durations[i].Observe(v.ElapsedMS / 1000)
			break
		}
	}
	if v.Outcome == "interrupted" {
		k.meters.interrupted.Add(1)
	}
	k.meters.records[1][0].Add(1)
}
