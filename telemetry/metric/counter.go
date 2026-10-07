package metric

import "sync/atomic"

type cell struct {
	values    []string
	value     atomic.Uint64
	counter   Counter
	gauge     Gauge
	histogram Histogram
}

type CounterVec struct{ family *family }
type Counter struct{ cell *cell }

func (v *CounterVec) Bind(values ...string) (*Counter, error) {
	var f *family
	if v != nil {
		f = v.family
	}
	c, err := bind(f, values)
	if c == nil || err != nil {
		return nil, err
	}
	return &c.counter, nil
}

func (v *CounterVec) Declare(values ...string) error {
	if v == nil || v.family == nil {
		return nil
	}
	return (&Registry{state: v.family.registry}).DeclareBatch([]TupleDeclaration{{v, values}})
}

// Add saturates at uint64's maximum rather than making a counter decrease.
// A nil or zero Counter is inert.
func (c *Counter) Add(n uint64) {
	if c == nil || c.cell == nil {
		return
	}
	for {
		old := c.cell.value.Load()
		next := old + n
		if next < old {
			next = ^uint64(0)
		}
		if c.cell.value.CompareAndSwap(old, next) {
			return
		}
	}
}
