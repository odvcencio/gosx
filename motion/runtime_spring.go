package motion

import "math"

// One WASM memory page of checkpoint tables per decoded program. Shared spring
// parameters/endpoints reuse their table; unique springs beyond this budget
// retain the original evaluator, so preparation cannot grow without bound.
const runtimeSpringCheckpointBytes = 64 << 10

type runtimeSpringPreparation struct {
	remaining int
	tables    map[[6]uint64]*PreparedSpring
	tracks    map[*Track]*PreparedSpring
}

func prepareRuntimeSprings(timeline *Timeline) map[*Track]*PreparedSpring {
	p := runtimeSpringPreparation{remaining: runtimeSpringCheckpointBytes}
	p.visit(timeline)
	return p.tracks
}

func (p *runtimeSpringPreparation) visit(timeline *Timeline) {
	for _, child := range timeline.Children {
		track := child.Track
		if track != nil && track.Gen != nil && track.Gen.Kind == GenSpring && track.Gen.Base.Arity.Width() >= 2 {
			gen := track.Gen
			physics := gen.Spring.defaults()
			from, to := gen.Base.F[0], gen.Base.F[1]
			// Bit keys preserve distinctions such as signed-zero endpoints.
			key := [6]uint64{math.Float64bits(physics.Mass), math.Float64bits(physics.Stiffness), math.Float64bits(physics.Damping), math.Float64bits(physics.Velocity), math.Float64bits(from), math.Float64bits(to)}
			sampler := p.tables[key]
			if sampler == nil {
				duration, count := springPreparation(gen.Spring, from, to)
				bytes := count * 16 // two float64 values per checkpoint
				if count > 1 && bytes <= p.remaining {
					sampler = prepareSpring(gen.Spring, from, to, duration, count)
					if p.tables == nil {
						p.tables = make(map[[6]uint64]*PreparedSpring)
					}
					p.tables[key] = sampler
					p.remaining -= bytes
				}
			}
			if sampler != nil {
				if p.tracks == nil {
					p.tracks = make(map[*Track]*PreparedSpring)
				}
				p.tracks[track] = sampler
			}
		}
		if child.Sub != nil {
			p.visit(child.Sub)
		}
	}
}
