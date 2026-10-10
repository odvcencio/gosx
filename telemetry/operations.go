package telemetry

import (
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/telemetry/metric"
)

type operationMeters struct {
	total    [5]*metric.Counter
	duration *metric.Histogram
}

func operationPairs(values []Operation) []Operation {
	pairs := []Operation{{"isr", "regenerate"}, {"isr", "refresh"}}
	for _, value := range values {
		found := false
		for _, old := range pairs {
			if old == value {
				found = true
				break
			}
		}
		if !found {
			pairs = append(pairs, value)
		}
	}
	return pairs
}

func (t *Telemetry) initializeOperations() error {
	pairs := append(operationPairs(t.opts.Metrics.Operations), Operation{"other", "other"})
	labels := []metric.Label{{Name: "component", MaxValues: 66}, {Name: "operation", MaxValues: 66}}
	total, err := t.counter("gosx_operations_total", append(append([]metric.Label(nil), labels...), enumLabel("status", operationStatuses...))...)
	if err != nil {
		return err
	}
	duration, err := t.histogram("gosx_operation_duration_seconds", requestBounds, labels...)
	if err != nil {
		return err
	}
	var batch []metric.TupleDeclaration
	for _, op := range pairs {
		for _, status := range operationStatuses {
			batch = append(batch, metric.TupleDeclaration{Instrument: total, Values: []string{op.Component, op.Name, status}})
		}
		batch = append(batch, metric.TupleDeclaration{Instrument: duration, Values: []string{op.Component, op.Name}})
	}
	if err := t.authority.DeclareBatch(batch); err != nil {
		return err
	}
	t.operations = make(map[Operation]operationMeters, len(pairs))
	for _, op := range pairs {
		var m operationMeters
		for i, status := range operationStatuses {
			m.total[i], _ = total.Bind(op.Component, op.Name, status)
		}
		m.duration, _ = duration.Bind(op.Component, op.Name)
		t.operations[op] = m
	}
	return nil
}

// ObserveOperation records declared pairs and fixed status classes only. It
// ignores target paths and error text and retains no application event state.
func (t *Telemetry) ObserveOperation(event server.OperationEvent) {
	if !t.Enabled() || t.operations == nil {
		return
	}
	m, ok := t.operations[Operation{event.Component, event.Operation}]
	if !ok {
		m = t.operations[Operation{"other", "other"}]
		t.core.dropped["unknown_label"].Add(1)
	}
	index := 4
	for i, value := range operationStatuses {
		if event.Status == value {
			index = i
			break
		}
	}
	m.total[index].Add(1)
	elapsed := event.Duration.Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	_ = m.duration.Observe(elapsed)
}
