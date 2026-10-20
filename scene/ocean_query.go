package scene

import (
	_ "embed"
	"encoding/json"
	"math"
)

//go:embed ocean_waves.json
var oceanWaveJSON []byte
var oceanWaveTable = func() (t struct {
	Ratio, Angle, Weight []float64
	Gravity, PhaseStep   float64
}) {
	if err := json.Unmarshal(oceanWaveJSON, &t); err != nil {
		panic(err)
	}
	return
}()

// OceanQuery mirrors the GPU ocean vertex pass. Quality is "low" (four waves)
// or "high" (six). Floor must sample the ocean bathymetry texture in world
// coordinates, including its encoding; nil means deep water. Zero-valued Ocean
// properties use the public defaults, as they do after scene normalization.
type OceanQuery struct {
	data  [84]float64
	floor func(x, z float64) float64
}

// OceanSample is a displaced surface point, its GPU shading normal, and water
// particle velocity (metres/second). Parameter is the undisplaced XZ coordinate.
// Tangents match the shader: shoaling and run-up gradients are not differentiated.
type OceanSample struct {
	Position, Normal, Velocity Vector3
	TangentX, TangentZ         Vector3
	Parameter                  Vector3
}

func NewOceanQuery(o Ocean, quality string, floor func(x, z float64) float64) *OceanQuery {
	q := &OceanQuery{floor: floor}
	if q.floor == nil {
		q.floor = func(x, z float64) float64 { return -1e4 }
	}
	f32 := func(v float64) float64 { return float64(float32(v)) }
	defaulted := func(v, d float64) float64 {
		if v == 0 {
			return d
		}
		return v
	}
	count := 6
	if quality == "low" {
		count = 4
	}
	q.data[0], q.data[3], q.data[19], q.data[27] = f32(o.Level), f32(defaulted(o.Speed, 1)), f32(defaulted(o.Surf, .5)), float64(count)
	h, l, c := defaulted(o.WaveHeight, .8), defaulted(o.WaveLength, 18), defaulted(o.Choppiness, .6)
	var weights float64
	for i := 0; i < count; i++ {
		weights += oceanWaveTable.Weight[i] * oceanWaveTable.Weight[i]
	}
	for i := 0; i < count; i++ {
		b := 36 + i*8
		angle := o.WindDirection*math.Pi/180 + oceanWaveTable.Angle[i]
		k := 2 * math.Pi / (l * oceanWaveTable.Ratio[i])
		values := []float64{math.Sin(angle), math.Cos(angle), k, math.Sqrt(oceanWaveTable.Gravity*k) * q.data[3], oceanWaveTable.Weight[i] * h / math.Sqrt(8*weights), c / (k * float64(count)), math.Mod(float64(i)*oceanWaveTable.PhaseStep, 1) * 2 * math.Pi}
		for j, v := range values {
			q.data[b+j] = f32(v)
		}
	}
	return q
}

func oceanSmooth(a, b, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-a)/(b-a)))
	return t * t * (3 - 2*t)
}
func oceanSlope(a, b, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-a)/(b-a)))
	return 6 * t * (1 - t) / (b - a)
}

// Evaluate samples a material point in the undeformed ocean grid.
func (q *OceanQuery) Evaluate(x, z, seconds float64) OceanSample {
	u := q.data
	t := float64(float32(seconds))
	depth := u[0] - q.floor(x, z)
	p, dx, dz, v := Vec3(x, u[0], z), Vec3(1, 0, 0), Vec3(0, 0, 1), Vector3{}
	for i := 0; i < int(u[27]); i++ {
		b := 36 + i*8
		wx, wz, k, omega := u[b], u[b+1], u[b+2], u[b+3]
		shoal := oceanSmooth(0, 1.2, depth*k)
		a, qa := u[b+4]*shoal, u[b+5]*shoal
		th := k*(wx*x+wz*z) - omega*t + u[b+6]
		s, c := math.Sin(th), math.Cos(th)
		p.X += qa * wx * c
		p.Y += a * s
		p.Z += qa * wz * c
		dx.X -= k * qa * wx * wx * s
		dx.Y += k * a * wx * c
		dx.Z -= k * qa * wx * wz * s
		dz.X -= k * qa * wx * wz * s
		dz.Y += k * a * wz * c
		dz.Z -= k * qa * wz * wz * s
		v.X += qa * wx * omega * s
		v.Y -= a * omega * c
		v.Z += qa * wz * omega * s
	}
	ph := t*u[3]/9 + .15*math.Sin(x*.07+1.3) + .08*math.Sin(x*.19)
	ph -= math.Floor(ph)
	up, down := oceanSmooth(0, .28, ph), oceanSmooth(.28, 1, ph)
	runup := u[19] * .45 * (1 - oceanSmooth(.3, 4, depth))
	p.Y += runup * up * (1 - down)
	v.Y += runup * (oceanSlope(0, .28, ph)*(1-down) - up*oceanSlope(.28, 1, ph)) * u[3] / 9
	n := Vec3(dz.Y*dx.Z-dz.Z*dx.Y, dz.Z*dx.X-dz.X*dx.Z, dz.X*dx.Y-dz.Y*dx.X)
	length := math.Sqrt(n.X*n.X + n.Y*n.Y + n.Z*n.Z)
	if length == 0 {
		length = 1
	}
	n.X /= length
	n.Y /= length
	n.Z /= length
	return OceanSample{Position: p, Normal: n, Velocity: v, TangentX: dx, TangentZ: dz, Parameter: Vector3{X: x, Z: z}}
}

// Sample inverts horizontal Gerstner displacement to find water above x,z.
// Two fixed-point steps are followed by at most eight Newton refinements.
func (q *OceanQuery) Sample(x, z, seconds float64) OceanSample {
	qx, qz := x, z
	for i := 0; i < 2; i++ {
		p := q.Evaluate(qx, qz, seconds).Position
		qx += x - p.X
		qz += z - p.Z
	}
	for i := 0; i < 8; i++ {
		p := q.Evaluate(qx, qz, seconds).Position
		ex, ez := p.X-x, p.Z-z
		if math.Hypot(ex, ez) < 1e-8 {
			break
		}
		const h = .001
		dx, dz := q.Evaluate(qx+h, qz, seconds).Position, q.Evaluate(qx, qz+h, seconds).Position
		a, b, c, d := (dx.X-p.X)/h, (dz.X-p.X)/h, (dx.Z-p.Z)/h, (dz.Z-p.Z)/h
		det := a*d - b*c
		if math.Abs(det) < 1e-8 {
			qx -= ex * .5
			qz -= ez * .5
		} else {
			qx -= (d*ex - b*ez) / det
			qz -= (a*ez - c*ex) / det
		}
	}
	return q.Evaluate(qx, qz, seconds)
}
