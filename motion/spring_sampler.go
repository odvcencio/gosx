package motion

import "math"

// A checkpoint retains the exact Euler state, including velocity. Replaying at
// most 31 steps preserves Spring.Value's bit identity without storing every
// 240 Hz sample (at most 1,216 checkpoint bytes for a ten-second spring).
const springCheckpointSteps = 32

type springCheckpoint struct {
	x, velocity float64
}

// PreparedSpring is an immutable sampler for one spring and its endpoints.
// Create it with Spring.Prepare. Sampling allocates nothing and is safe for
// concurrent readers, including out-of-order seeks.
type PreparedSpring struct {
	spring      Spring
	from, to    float64
	duration    float64
	checkpoints []springCheckpoint
}

// Prepare computes reusable Euler checkpoints for repeated Value calls. It
// preserves Spring.Value's exact fixed-step results and settle-time behavior.
// Preparation costs at most 2,400 integration steps and 1,216 checkpoint bytes;
// each sample replays at most 31 steps. Non-finite inputs retain the original
// uncached behavior. A one-off sample can continue to use Spring.Value.
func (s Spring) Prepare(from, to float64) *PreparedSpring {
	duration, count := springPreparation(s, from, to)
	return prepareSpring(s, from, to, duration, count)
}

func springPreparation(s Spring, from, to float64) (duration float64, count int) {
	duration = s.Duration(from, to)
	p := s.defaults()
	for _, value := range [...]float64{from, to, p.Mass, p.Stiffness, p.Damping, p.Velocity, duration} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return duration, 0
		}
	}
	if duration <= 0 || duration > springMaxTime {
		return duration, 0
	}
	return duration, int(math.Ceil(duration/SpringDT))/springCheckpointSteps + 1
}

func prepareSpring(s Spring, from, to, duration float64, count int) *PreparedSpring {
	sampler := &PreparedSpring{spring: s.defaults(), from: from, to: to, duration: duration}
	if count == 0 {
		return sampler
	}
	p := sampler.spring
	sampler.checkpoints = make([]springCheckpoint, count)
	x, velocity := from, p.Velocity
	sampler.checkpoints[0] = springCheckpoint{x, velocity}
	for checkpoint := 1; checkpoint < count; checkpoint++ {
		for step := 0; step < springCheckpointSteps; step++ {
			force := float64(-p.Stiffness*(x-to)) - float64(p.Damping*velocity)
			velocity += float64((force / p.Mass) * SpringDT)
			x += float64(velocity * SpringDT)
		}
		sampler.checkpoints[checkpoint] = springCheckpoint{x, velocity}
	}
	return sampler
}

// Duration returns the same settle-time bound as Spring.Duration for the
// prepared endpoints. The zero value represents a constant zero value.
func (p *PreparedSpring) Duration() float64 { return p.duration }

// Value samples the prepared spring at t seconds. The result is bit-identical
// to Spring.Value with the prepared endpoints; negative/zero times hold from,
// and times at or past Duration hold to. It owns no clock or mutable playhead.
func (p *PreparedSpring) Value(t float64) float64 {
	if len(p.checkpoints) == 0 {
		return p.spring.Value(p.from, p.to, t)
	}
	// NaN also holds from, as the original integrator performs no steps for it.
	if !(t > 0) {
		return p.from
	}
	if t >= p.duration {
		return p.to
	}
	steps := int(t / SpringDT)
	checkpoint := steps / springCheckpointSteps
	state := p.checkpoints[checkpoint]
	x, velocity := state.x, state.velocity
	physics := p.spring
	for step := checkpoint * springCheckpointSteps; step < steps; step++ {
		force := float64(-physics.Stiffness*(x-p.to)) - float64(physics.Damping*velocity)
		velocity += float64((force / physics.Mass) * SpringDT)
		x += float64(velocity * SpringDT)
	}
	return x
}
