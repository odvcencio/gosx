package telemetry

import (
	"unicode/utf8"

	"m31labs.dev/gosx/telemetry/metric"
)

func (t *Telemetry) initializeDegraded() error {
	components := uniqueHubValues(append(append([]string(nil), frameworkDegraded...), t.opts.Metrics.DegradedComponents...))
	v, err := t.gauge("gosx_degraded", enumLabel("component", components...))
	if err != nil {
		return err
	}
	if err := t.declareProduct(v, components); err != nil {
		return err
	}
	t.degraded = make(map[string]*metric.Gauge, len(components))
	for _, component := range components {
		t.degraded[component], _ = v.Bind(component)
	}
	return nil
}

// SetDegraded accepts only startup-declared components and fixed framework
// components. Unknown values never create a metric label, including when off.
func (t *Telemetry) SetDegraded(component string, degraded bool) error {
	if len(component) == 0 || len(component) > 128 || !utf8.ValidString(component) {
		return invalid("degraded_component", "name")
	}
	if t == nil || t.registry == nil {
		return nil
	}
	g, ok := t.degraded[component]
	if !ok {
		return invalid("degraded_component", "undeclared")
	}
	if !t.Enabled() {
		return ErrClosed
	}
	value := 0.0
	if degraded {
		value = 1
	}
	return g.Set(value)
}
