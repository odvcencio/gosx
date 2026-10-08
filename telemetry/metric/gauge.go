package metric

import "math"

type GaugeVec struct{ family *family }
type Gauge struct{ cell *cell }

func (v *GaugeVec) Bind(values ...string) (*Gauge, error) {
	var f *family
	if v != nil {
		f = v.family
	}
	c, err := bind(f, values)
	if c == nil || err != nil {
		return nil, err
	}
	return &c.gauge, nil
}

func (v *GaugeVec) Declare(values ...string) error {
	if v == nil || v.family == nil {
		return nil
	}
	return (&Registry{state: v.family.registry}).DeclareBatch([]TupleDeclaration{{v, values}})
}

func (g *Gauge) Set(n float64) error {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return invalid("metric.value", "not_finite")
	}
	if g != nil && g.cell != nil {
		g.cell.value.Store(math.Float64bits(n))
	}
	return nil
}
