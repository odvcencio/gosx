package motion

import (
	"encoding/json"
	"math"
	"testing"
)

func TestSignalWirePreservesAuthoredZeroMaximum(t *testing.T) {
	for _, max := range []float64{0, math.Copysign(0, -1), 2} {
		p := NewProgram("bounds")
		clock := p.Time("clock")
		p.Map("map", clock, 0, 1, -1, max)
		p.Clamp("clamp", clock, -1, max)
		data, err := p.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		var wire struct{ Signals []map[string]json.RawMessage }
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		if _, exists := wire.Signals[0]["max"]; exists {
			t.Fatal("unrelated source acquired an unused maximum")
		}
		for _, signal := range wire.Signals[1:] {
			var got float64
			if err := json.Unmarshal(signal["max"], &got); err != nil || math.Float64bits(got) != math.Float64bits(max) {
				t.Fatalf("authored maximum %g lost: %s (%v)", max, data, err)
			}
		}
	}
}
