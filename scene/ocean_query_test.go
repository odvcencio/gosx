package scene

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestOceanQueryCrossLanguageParity(t *testing.T) {
	data, err := os.ReadFile("testdata/ocean-query-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Ocean                      Ocean
		Quality                    string
		Floor, X, Z, Time          float64
		Position, Normal, Velocity Vector3
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		p := NewOceanQuery(c.Ocean, c.Quality, func(x, z float64) float64 { return c.Floor }).Sample(c.X, c.Z, c.Time)
		for j, pair := range [][2]Vector3{{p.Position, c.Position}, {p.Normal, c.Normal}, {p.Velocity, c.Velocity}} {
			for _, d := range []float64{pair[0].X - pair[1].X, pair[0].Y - pair[1].Y, pair[0].Z - pair[1].Z} {
				if math.Abs(d) > 1e-7 {
					t.Fatalf("sample %d vector %d differs by %g", i, j, d)
				}
			}
		}
	}
	t.Logf("%d Go/JS parity samples", len(cases))
}

// Reference port of the GPU vertex formula, with independently packed waves.
func gpuOceanReference(o Ocean, low bool, floor, x, z, time float64) (Vector3, Vector3) {
	ratios := []float64{1, .73, .53, .39, .28, .21}
	angles := []float64{0, .38, -.46, .83, -.95, 1.4}
	weights := []float64{1, .62, .42, .28, .19, .13}
	count := 6
	if low {
		count = 4
	}
	f := func(x float64) float64 { return float64(float32(x)) }
	sum := 0.
	smooth := func(a, b, x float64) float64 { v := math.Max(0, math.Min(1, (x-a)/(b-a))); return v * v * (3 - 2*v) }
	for _, w := range weights[:count] {
		sum += w * w
	}
	p, dx, dz := Vec3(x, f(o.Level), z), Vec3(1, 0, 0), Vec3(0, 0, 1)
	for i := 0; i < count; i++ {
		angle := o.WindDirection*math.Pi/180 + angles[i]
		k0 := 2 * math.Pi / (o.WaveLength * ratios[i])
		wx, wz, k, omega := f(math.Sin(angle)), f(math.Cos(angle)), f(k0), f(math.Sqrt(9.81*k0)*f(o.Speed))
		shoal := smooth(0, 1.2, (f(o.Level)-floor)*k)
		a, qa := f(weights[i]*o.WaveHeight/math.Sqrt(8*sum))*shoal, f(o.Choppiness/(k0*float64(count)))*shoal
		phase := k*(wx*x+wz*z) - omega*f(time) + f(math.Mod(float64(i)*.6180339887, 1)*2*math.Pi)
		s, c := math.Sin(phase), math.Cos(phase)
		p.X += qa * wx * c
		p.Y += a * s
		p.Z += qa * wz * c
		dx.X -= k * qa * wx * wx * s
		dx.Y += k * a * wx * c
		dx.Z -= k * qa * wx * wz * s
		dz.X -= k * qa * wx * wz * s
		dz.Y += k * a * wz * c
		dz.Z -= k * qa * wz * wz * s
	}
	ph := f(time)*f(o.Speed)/9 + .15*math.Sin(x*.07+1.3) + .08*math.Sin(x*.19)
	ph -= math.Floor(ph)
	p.Y += f(o.Surf) * .45 * smooth(0, .28, ph) * (1 - smooth(.28, 1, ph)) * (1 - smooth(.3, 4, f(o.Level)-floor))
	n := Vec3(dz.Y*dx.Z-dz.Z*dx.Y, dz.Z*dx.X-dz.X*dx.Z, dz.X*dx.Y-dz.Y*dx.X)
	l := math.Sqrt(n.X*n.X + n.Y*n.Y + n.Z*n.Z)
	n.X /= l
	n.Y /= l
	n.Z /= l
	return p, n
}

func TestOceanQueryGPUParityGrid(t *testing.T) {
	o := Ocean{WaveHeight: .9, WaveLength: 17, Choppiness: .7, WindDirection: 8, Speed: 1, Surf: .6}
	samples := 0
	for _, quality := range []string{"low", "high"} {
		for _, floor := range []float64{-10000, -.2, -3} {
			q := NewOceanQuery(o, quality, func(x, z float64) float64 { return floor })
			for _, time := range []float64{0, .7, 9.1, 63} {
				for x := -20.; x <= 20; x += 5 {
					for z := -20.; z <= 20; z += 5 {
						p := q.Evaluate(x, z, time)
						r, n := gpuOceanReference(o, quality == "low", floor, x, z, time)
						for _, d := range []float64{p.Position.X - r.X, p.Position.Y - r.Y, p.Position.Z - r.Z, p.Normal.X - n.X, p.Normal.Y - n.Y, p.Normal.Z - n.Z} {
							if math.Abs(d) > 1e-4 {
								t.Fatalf("GPU mismatch %g", d)
							}
						}
						s := q.Sample(x, z, time)
						if math.Hypot(s.Position.X-x, s.Position.Z-z) > 1e-4 {
							t.Fatalf("inverse residual at %g,%g", x, z)
						}
						samples++
					}
				}
			}
		}
	}
	t.Logf("%d reference samples; tolerance 1e-4 m", samples)
}
