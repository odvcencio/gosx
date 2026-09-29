package scene

import (
	"fmt"
	"math"
)

// The physical sky follows Preetham, Shirley and Smits, "A Practical
// Analytic Model for Daylight" (SIGGRAPH 1999), in the single-scattering
// real-time form of Hoffman and Preetham, "Rendering Outdoor Light
// Scattering in Real Time" (GDC 2002). The browser shaders
// (sceneSkyPhysicalGLSL and sceneSkyPhysicalWGSL) and sceneSkyPhysicalParams
// in 16c-scene-shared-pbr.ts implement the same steps; keep them in step.

const (
	physicalSkyDefaultTurbidity = 10
	physicalSkyDefaultRayleigh  = 2
	physicalSkyDefaultMie       = 0.005
	physicalSkyDefaultMieG      = 0.8
	physicalSkyDefaultDiskDeg   = 0.53
	// Optical depth of the Rayleigh and Mie layers at the zenith, meters.
	physicalSkyRayleighZenith = 8400
	physicalSkyMieZenith      = 1250
	// Sun illuminance and its fall-off as the sun nears the horizon.
	physicalSkySunEE        = 1000
	physicalSkySunCutoff    = math.Pi / 1.95
	physicalSkySunSteepness = 1.5
)

// Total Rayleigh scattering coefficient at sea level for 680, 550 and
// 450 nm (1/m), and the Mie wavelength term pi*(2pi/lambda)^(v-2)*K with
// Junge exponent v = 4 and K = (0.686, 0.678, 0.666).
var (
	physicalSkyTotalRayleigh = [3]float64{5.804542996261093e-6, 1.3562911419845635e-5, 3.0265902468824876e-5}
	physicalSkyMieConst      = [3]float64{1.8399918514433978e14, 2.7798023919660528e14, 4.0790479543861094e14}
)

// SunDirectionFromAngles returns the unit direction toward a sun at the
// given elevation above the horizon and azimuth, in degrees. Azimuth 0
// faces -Z; positive azimuth turns toward +X.
func SunDirectionFromAngles(elevationDeg, azimuthDeg float64) Vector3 {
	el := elevationDeg * math.Pi / 180
	az := azimuthDeg * math.Pi / 180
	return Vector3{X: math.Sin(az) * math.Cos(el), Y: math.Sin(el), Z: -math.Cos(az) * math.Cos(el)}
}

func normalizeSunDirection(v Vector3) Vector3 {
	length := math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
	if length < 1e-9 || math.IsNaN(length) || math.IsInf(length, 0) {
		return SunDirectionFromAngles(6, 0)
	}
	return Vector3{X: v.X / length, Y: v.Y / length, Z: v.Z / length}
}

// physicalSkyParams holds the per-sky terms that do not depend on the view
// direction. The browser computes the same 16 values once per frame.
type physicalSkyParams struct {
	betaR, betaM [3]float64
	sunE         float64
	sunFade      float64
	sun          Vector3
	diskCos      float64
	g            float64
	intensity    float64
}

func physicalSkyDefault(v, def float64) float64 {
	if v == 0 {
		return def
	}
	return v
}

func newPhysicalSkyParams(s Sky) physicalSkyParams {
	sun := normalizeSunDirection(s.SunDirection)
	turbidity := physicalSkyDefault(s.Turbidity, physicalSkyDefaultTurbidity)
	rayleigh := physicalSkyDefault(s.Rayleigh, physicalSkyDefaultRayleigh)
	mie := physicalSkyDefault(s.MieCoefficient, physicalSkyDefaultMie)
	g := physicalSkyDefault(s.MieDirectionalG, physicalSkyDefaultMieG)
	disk := physicalSkyDefault(s.SunDiskRadius, physicalSkyDefaultDiskDeg)
	p := physicalSkyParams{sun: sun, g: g, intensity: physicalSkyDefault(s.Intensity, 1)}
	zenith := math.Acos(math.Max(-1, math.Min(1, sun.Y)))
	p.sunE = physicalSkySunEE * math.Max(0, 1-math.Exp(-((physicalSkySunCutoff-zenith)/physicalSkySunSteepness)))
	p.sunFade = 1 - math.Max(0, math.Min(1, 1-math.Exp(sun.Y)))
	rayleighCoefficient := rayleigh - (1 - p.sunFade)
	c := 0.2 * turbidity * 10e-18
	for i := 0; i < 3; i++ {
		p.betaR[i] = physicalSkyTotalRayleigh[i] * rayleighCoefficient
		p.betaM[i] = 0.434 * c * physicalSkyMieConst[i] * mie
	}
	if disk < 0 {
		p.diskCos = 2 // cos never exceeds 1: no disk
	} else {
		p.diskCos = math.Cos(disk * math.Pi / 180)
	}
	return p
}

// PhysicalRadiance returns the linear radiance of a physical-mode sky seen
// along dir (any length; it is normalized). includeSun adds the sun disk;
// leave it out when baking IBL, because the key DirectionalLight already
// carries the sun and a tiny, very bright disk makes prefiltered maps noisy.
// The result is display-referred for the ACES tone mapper at exposure
// about 0.5 to 1, like the reference real-time implementations.
func (s Sky) PhysicalRadiance(dir Vector3, includeSun bool) (r, g, b float64) {
	p := newPhysicalSkyParams(s)
	if !includeSun {
		p.diskCos = 2
	}
	out := p.radiance(normalizeSunDirection(dir))
	return out[0], out[1], out[2]
}

func (p physicalSkyParams) radiance(dir Vector3) [3]float64 {
	zenith := math.Acos(math.Max(0, math.Min(1, dir.Y)))
	inverse := 1 / (math.Cos(zenith) + 0.15*math.Pow(93.885-zenith*180/math.Pi, -1.253))
	sR := physicalSkyRayleighZenith * inverse
	sM := physicalSkyMieZenith * inverse
	cosTheta := dir.X*p.sun.X + dir.Y*p.sun.Y + dir.Z*p.sun.Z
	rPhase := 3 / (16 * math.Pi) * (1 + cosTheta*cosTheta)
	g2 := p.g * p.g
	mPhase := 1 / (4 * math.Pi) * (1 - g2) / math.Pow(1-2*p.g*cosTheta+g2, 1.5)
	horizonMix := math.Max(0, math.Min(1, math.Pow(1-p.sun.Y, 5)))
	disk := 0.0
	if p.diskCos <= 1 {
		disk = smoothstepSky(p.diskCos, p.diskCos+0.00002, cosTheta)
	}
	exponent := 1 / (1.2 + 1.2*p.sunFade)
	offset := [3]float64{0, 0.0003, 0.00075}
	var out [3]float64
	for i := 0; i < 3; i++ {
		fex := math.Exp(-(p.betaR[i]*sR + p.betaM[i]*sM))
		scatter := (p.betaR[i]*rPhase + p.betaM[i]*mPhase) / math.Max(p.betaR[i]+p.betaM[i], 1e-30)
		lin := math.Pow(p.sunE*scatter*(1-fex), 1.5)
		lin *= mix1(1, math.Pow(p.sunE*scatter*fex, 0.5), horizonMix)
		l0 := 0.1*fex + p.sunE*19000*fex*disk
		color := (lin+l0)*0.04 + offset[i]
		out[i] = math.Pow(math.Max(color, 0), exponent) * p.intensity
	}
	return out
}

func smoothstepSky(e0, e1, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-e0)/(e1-e0)))
	return t * t * (3 - 2*t)
}

func mix1(a, b, t float64) float64 { return a + (b-a)*t }

// fillPhysicalSkyGradient sets any unset gradient stop to the model's color
// at the zenith, the horizon (facing the sun's azimuth) and below the
// horizon, through ACES and the sRGB transfer, so a backend without the
// physical pass still shows a matching background.
func fillPhysicalSkyGradient(s *Sky) {
	p := newPhysicalSkyParams(*s)
	p.diskCos = 2
	flat := math.Hypot(p.sun.X, p.sun.Z)
	toward := Vector3{X: 0, Y: 0, Z: -1}
	if flat > 1e-6 {
		toward = Vector3{X: p.sun.X / flat, Z: p.sun.Z / flat}
	}
	hex := func(dir Vector3) string {
		c := p.radiance(normalizeSunDirection(dir))
		var b [3]int
		for i := range c {
			b[i] = int(math.Round(srgbEncodeSky(acesSky(c[i])) * 255))
		}
		return fmt.Sprintf("#%02x%02x%02x", b[0], b[1], b[2])
	}
	if s.TopColor == "" {
		s.TopColor = hex(Vector3{Y: 1})
	}
	if s.HorizonColor == "" {
		s.HorizonColor = hex(Vector3{X: toward.X, Y: 0.02, Z: toward.Z})
	}
	if s.BottomColor == "" {
		s.BottomColor = hex(Vector3{X: toward.X, Y: -0.2, Z: toward.Z})
	}
}

// acesSky is the Narkowicz fit of the ACES filmic curve.
func acesSky(x float64) float64 {
	v := (x * (2.51*x + 0.03)) / (x*(2.43*x+0.59) + 0.14)
	return math.Max(0, math.Min(1, v))
}

func srgbEncodeSky(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}
