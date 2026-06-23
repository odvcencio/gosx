package motion

import "math"

// Origin selects the reference point for stagger distance calculation.
type Origin uint8

const (
	FromFirst  Origin = iota // distance from index 0
	FromLast                 // distance from last index
	FromCenter               // distance from the float center
	FromIndex                // distance from a specific index
)

// StaggerSpec configures how delays are distributed across N targets.
type StaggerSpec struct {
	From    Origin
	FromIdx int    // used when From == FromIndex
	Delay   float64 // per-unit-distance delay (seconds)
	Grid    [2]int  // [cols, rows]; if cols>0 && rows>0 → 2D grid stagger, else linear
}

// ExpandStagger returns len(targetIDs) tracks. Each is a deep copy of template
// with TargetID = targetIDs[i] and every Key.T shifted by +delay(i).
// Pure & deterministic. Template is NOT mutated (deep-copy Keys).
func ExpandStagger(template Track, targetIDs []int, spec StaggerSpec) []Track {
	n := len(targetIDs)
	out := make([]Track, n)

	useGrid := spec.Grid[0] > 0 && spec.Grid[1] > 0

	for i := 0; i < n; i++ {
		var dist float64

		if useGrid {
			cols := spec.Grid[0]
			rows := spec.Grid[1]
			col := float64(i % cols)
			row := float64(i / cols)

			var ocol, orow float64
			switch spec.From {
			case FromFirst:
				ocol, orow = 0, 0
			case FromLast:
				ocol = float64(cols - 1)
				orow = float64(rows - 1)
			case FromCenter:
				ocol = float64(cols-1) / 2.0
				orow = float64(rows-1) / 2.0
			case FromIndex:
				ocol = float64(spec.FromIdx % cols)
				orow = float64(spec.FromIdx / cols)
			}

			dist = math.Hypot(col-ocol, row-orow)
		} else {
			switch spec.From {
			case FromFirst:
				dist = float64(i)
			case FromLast:
				dist = float64(n - 1 - i)
			case FromCenter:
				center := float64(n-1) / 2.0
				dist = math.Abs(float64(i) - center)
			case FromIndex:
				dist = math.Abs(float64(i) - float64(spec.FromIdx))
			}
		}

		delay := spec.Delay * dist

		// deep-copy template
		tr := template
		tr.TargetID = targetIDs[i]

		// deep-copy Keys slice and shift T by delay
		keys := make([]Key, len(template.Keys))
		for j, k := range template.Keys {
			keys[j] = k
			keys[j].T = k.T + delay
		}
		tr.Keys = keys

		out[i] = tr
	}

	return out
}
