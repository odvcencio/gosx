//gosx:deterministic

package parity

import "m31labs.dev/gosx/det"

// CorpusSteps is the number of steps the built-in corpus records.
const CorpusSteps = 240

// corpusBody is one particle of the built-in corpus.
type corpusBody struct {
	x, y, vx, vy, angle, spin float64
}

// RunCorpus runs the built-in corpus and returns its trace. The corpus is a
// particle system that calls every function in package det in each step,
// so a platform whose math differs in even one bit changes some hash. The
// scenario is fixed: it takes no input and uses no randomness.
func RunCorpus() Trace {
	const (
		n  = 24
		dt = 1.0 / 60.0
	)
	bodies := make([]corpusBody, n)
	// Fixed pseudo-random initial state from an integer generator.
	seed := uint64(0x9E3779B97F4A7C15)
	unit := func() float64 {
		seed ^= seed >> 12
		seed ^= seed << 25
		seed ^= seed >> 27
		return float64((seed*0x2545F4914F6CDD1D)>>11) / (1 << 53)
	}
	for i := range bodies {
		bodies[i] = corpusBody{
			x:     float64(unit()*40) - 20,
			y:     float64(unit()*40) - 20,
			vx:    float64(unit()*6) - 3,
			vy:    float64(unit()*6) - 3,
			angle: float64(unit()*6) - 3,
			spin:  float64(unit()*2) - 1,
		}
	}

	rec := NewRecorder("det-corpus", func(dst []byte) []byte {
		for i := range bodies {
			b := &bodies[i]
			dst = AppendFloat64(dst, b.x)
			dst = AppendFloat64(dst, b.y)
			dst = AppendFloat64(dst, b.vx)
			dst = AppendFloat64(dst, b.vy)
			dst = AppendFloat64(dst, b.angle)
			dst = AppendFloat64(dst, b.spin)
		}
		return dst
	})

	for step := 0; step < CorpusSteps; step++ {
		for i := range bodies {
			b := &bodies[i]
			// Pull toward the origin: force depends on distance and bearing.
			dist := det.Hypot(b.x, b.y)
			bearing := det.Atan2(b.y, b.x)
			pull := det.Tanh(float64(dist*0.05)) * 4
			fx := float64(-pull * det.Cos(bearing))
			fy := float64(-pull * det.Sin(bearing))
			// A swirl term from the body's own angle.
			fx = det.MulAdd(det.Sin(b.angle), 0.75, fx)
			fy = det.MulAdd(det.Cos(b.angle), 0.75, fy)
			// Drag with exponential decay; a power-law term for speed.
			decay := det.Exp(-0.35 * dt)
			speed := det.Hypot(b.vx, b.vy)
			gain := det.Pow(1+speed, -0.25)
			b.vx = float64(det.MulAdd(fx, dt, b.vx) * decay * gain)
			b.vy = float64(det.MulAdd(fy, dt, b.vy) * decay * gain)
			b.x = det.MulAdd(b.vx, dt, b.x)
			b.y = det.MulAdd(b.vy, dt, b.y)
			// Rotation uses logarithms and the inverse trig functions.
			b.spin = det.MulAdd(det.Atan(float64(det.Sinh(b.spin)*0.3)), 0.5, float64(det.Log(2+det.Cosh(float64(b.spin*0.1)))*0.1))
			b.angle = det.MulAdd(b.spin, dt, b.angle)
			if det.Abs(b.angle) > 3 {
				b.angle = det.Asin(det.Sin(b.angle)) +
					float64(det.Acos(det.Cos(b.angle))*0.01) +
					det.Tan(float64(det.Log2(det.Abs(b.x)+1)*0.01)) +
					float64(det.Log10(dist+1)*0.001) +
					float64(det.Exp2(-det.Abs(b.spin))*0.01)
			}
		}
		rec.Step()
	}
	return rec.Trace()
}
