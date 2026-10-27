package scene

import (
	_ "embed"
	"encoding/json"
	"math"

	"m31labs.dev/gosx/det"
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
// or "high" (six). When the Ocean has a Spectrum, the surface is its swell waves
// (Ocean.SwellWaves, computed from the spectrum when absent), Quality is
// ignored, WaveHeight, WaveLength and Speed are ignored, and time runs at 1x.
// The waves are the strongest bins of the spectrum, not the full surface; see
// OceanSwellWaves. Floor must sample the ocean bathymetry texture in world
// coordinates, including its encoding; nil means deep water. Zero-valued Ocean
// properties use the public defaults, as they do after scene normalization.
type OceanQuery struct {
	data  [84]float64
	floor func(x, z float64) float64
	// swell is set when the Ocean has a Spectrum: the surface is then the sum of
	// these plane waves, and choppiness scales their horizontal displacement.
	swell []OceanWave
	chop  float64
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
	if o.Spectrum != nil {
		q.swell = o.SwellWaves
		if len(q.swell) == 0 {
			q.swell = OceanSwellWaves(*o.Spectrum, o.WindDirection, DefaultOceanSwellCount)
		}
		q.chop = defaulted(o.Choppiness, .6)
		q.data[0], q.data[3], q.data[19] = f32(o.Level), 1, f32(defaulted(o.Surf, .5))
		return q
	}
	q.data[0], q.data[3], q.data[19], q.data[27] = f32(o.Level), f32(defaulted(o.Speed, 1)), f32(defaulted(o.Surf, .5)), float64(count)
	h, l, c := defaulted(o.WaveHeight, .8), defaulted(o.WaveLength, 18), defaulted(o.Choppiness, .6)
	var weights float64
	for i := 0; i < count; i++ {
		weights += float64(oceanWaveTable.Weight[i] * oceanWaveTable.Weight[i])
	}
	for i := 0; i < count; i++ {
		b := 36 + i*8
		angle := o.WindDirection*math.Pi/180 + oceanWaveTable.Angle[i]
		k := 2 * math.Pi / (l * oceanWaveTable.Ratio[i])
		values := []float64{det.Sin(angle), det.Cos(angle), k, float64(math.Sqrt(float64(oceanWaveTable.Gravity*k)) * q.data[3]), oceanWaveTable.Weight[i] * h / math.Sqrt(8*weights), c / (k * float64(count)), float64(math.Mod(float64(float64(i)*oceanWaveTable.PhaseStep), 1) * 2 * math.Pi)}
		for j, v := range values {
			q.data[b+j] = f32(v)
		}
	}
	return q
}

func oceanSmooth(a, b, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-a)/(b-a)))
	return float64(t * t * (3 - 2*t))
}
func oceanSlope(a, b, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-a)/(b-a)))
	return 6 * t * (1 - t) / (b - a)
}

// Evaluate samples a material point in the undeformed ocean grid.
func (q *OceanQuery) Evaluate(x, z, seconds float64) OceanSample {
	u := q.data
	t := float64(float32(seconds))
	if q.swell != nil {
		t = seconds
	}
	depth := u[0] - q.floor(x, z)
	p, dx, dz, v := Vec3(x, u[0], z), Vec3(1, 0, 0), Vec3(0, 0, 1), Vector3{}
	for _, w := range q.swell {
		shoal := oceanSmooth(0, 1.2, float64(depth*w.K))
		a := float64(w.Amplitude * shoal)
		ch := float64(q.chop * a)
		th := float64(w.K*(float64(w.DirX*x)+float64(w.DirZ*z))) - float64(w.Omega*t) + w.Phase
		s, c := det.Sin(th), det.Cos(th)
		// y = a cos(th); horizontal displacement -chop a dir sin(th) moves points
		// toward the crests, as the FFT assemble pass does.
		p.X -= float64(ch * w.DirX * s)
		p.Y += float64(a * c)
		p.Z -= float64(ch * w.DirZ * s)
		dx.X -= float64(w.K * ch * w.DirX * w.DirX * c)
		dx.Y -= float64(w.K * a * w.DirX * s)
		dx.Z -= float64(w.K * ch * w.DirX * w.DirZ * c)
		dz.X -= float64(w.K * ch * w.DirX * w.DirZ * c)
		dz.Y -= float64(w.K * a * w.DirZ * s)
		dz.Z -= float64(w.K * ch * w.DirZ * w.DirZ * c)
		v.X += float64(ch * w.DirX * w.Omega * c)
		v.Y += float64(a * w.Omega * s)
		v.Z += float64(ch * w.DirZ * w.Omega * c)
	}
	for i := 0; i < int(u[27]); i++ {
		b := 36 + i*8
		wx, wz, k, omega := u[b], u[b+1], u[b+2], u[b+3]
		shoal := oceanSmooth(0, 1.2, float64(depth*k))
		a, qa := float64(u[b+4]*shoal), float64(u[b+5]*shoal)
		th := float64(k*(float64(wx*x)+float64(wz*z))) - float64(omega*t) + u[b+6]
		s, c := det.Sin(th), det.Cos(th)
		p.X += float64(qa * wx * c)
		p.Y += float64(a * s)
		p.Z += float64(qa * wz * c)
		dx.X -= float64(k * qa * wx * wx * s)
		dx.Y += float64(k * a * wx * c)
		dx.Z -= float64(k * qa * wx * wz * s)
		dz.X -= float64(k * qa * wx * wz * s)
		dz.Y += float64(k * a * wz * c)
		dz.Z -= float64(k * qa * wz * wz * s)
		v.X += float64(qa * wx * omega * s)
		v.Y -= float64(a * omega * c)
		v.Z += float64(qa * wz * omega * s)
	}
	ph := t*u[3]/9 + float64(.15*det.Sin(float64(x*.07)+1.3)) + float64(.08*det.Sin(float64(x*.19)))
	ph -= math.Floor(ph)
	up, down := oceanSmooth(0, .28, ph), oceanSmooth(.28, 1, ph)
	runup := float64(u[19] * .45 * (1 - oceanSmooth(.3, 4, depth)))
	p.Y += float64(runup * up * (1 - down))
	v.Y += runup * (float64(oceanSlope(0, .28, ph)*(1-down)) - float64(up*oceanSlope(.28, 1, ph))) * u[3] / 9
	n := Vec3(float64(dz.Y*dx.Z)-float64(dz.Z*dx.Y), float64(dz.Z*dx.X)-float64(dz.X*dx.Z), float64(dz.X*dx.Y)-float64(dz.Y*dx.X))
	length := math.Sqrt(float64(n.X*n.X) + float64(n.Y*n.Y) + float64(n.Z*n.Z))
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
		if det.Hypot(ex, ez) < 1e-8 {
			break
		}
		const h = .001
		dx, dz := q.Evaluate(qx+h, qz, seconds).Position, q.Evaluate(qx, qz+h, seconds).Position
		a, b, c, d := (dx.X-p.X)/h, (dz.X-p.X)/h, (dx.Z-p.Z)/h, (dz.Z-p.Z)/h
		det := float64(a*d) - float64(b*c)
		if math.Abs(det) < 1e-8 {
			qx -= ex * .5
			qz -= ez * .5
		} else {
			qx -= (float64(d*ex) - float64(b*ez)) / det
			qz -= (float64(a*ez) - float64(c*ex)) / det
		}
	}
	return q.Evaluate(qx, qz, seconds)
}
