package vessel

import "m31labs.dev/gosx/det"

// Propulsion names how the ship is pushed and steered.
type Propulsion int

const (
	// Shafts are fixed propeller shafts with rudders.
	Shafts Propulsion = iota
	// Pods are azimuthing electric pods: more steering authority, more wash.
	Pods
)

func (p Propulsion) String() string {
	if p == Pods {
		return "pods"
	}
	return "shafts"
}

// HullClass holds the fixed parameters of one kind of ship. Lengths are metres,
// mass is kilograms, power is watts, speeds are metres per second, angles are
// radians and periods are seconds.
type HullClass struct {
	Name         string
	Length       float64 // overall length
	Beam         float64
	Draft        float64
	Displacement float64 // mass in kg
	Decks        int     // passenger and crew decks, for deck plans and cutaway

	Propulsion     Propulsion
	InstalledPower float64 // shaft power at power setting 1
	ServiceSpeed   float64 // steady speed at full power in calm water
	Efficiency     float64 // propulsive efficiency, 0..1
	AsternFraction float64 // astern power limit as a fraction of ahead power
	PowerRamp      float64 // power setting change per second

	RudderMax       float64 // maximum rudder or pod angle
	RudderRate      float64 // rudder slew rate, radians per second
	SteerGain       float64 // Nomoto gain K: turning radius at steady state is Length/(K*rudder)
	SteerLag        float64 // Nomoto time factor T': yaw time constant is T'*Length/flow
	Wash            float64 // propeller-wash flow past the rudder at full power, as a fraction of ServiceSpeed
	AutopilotRudder float64 // fraction of RudderMax that the autopilot may use

	RollPeriod   float64 // natural roll period
	RollDamping  float64 // damping ratio of the bare hull
	PitchPeriod  float64
	PitchDamping float64
	HeavePeriod  float64
	HeaveDamping float64
	HeelPerAccel float64 // outward heel in radians per m/s^2 of turning acceleration

	StabilizerDamping float64 // extra roll damping ratio at full stabilizer effect
	StabilizerCut     float64 // fraction of wave and turn roll forcing removed at full effect
	StabilizerPower   float64 // watts drawn by the fins at full effect
}

// Expedition is a 125 m, 7-deck expedition ship with azimuth pods.
func Expedition() HullClass {
	return HullClass{
		Name: "expedition", Length: 125, Beam: 21, Draft: 5.2, Displacement: 8.4e6, Decks: 7,
		Propulsion: Pods, InstalledPower: 8e6, ServiceSpeed: 8.2, Efficiency: 0.7, AsternFraction: 0.6, PowerRamp: 1.0 / 45,
		RudderMax: 0.70, RudderRate: 0.06, SteerGain: 1.1, SteerLag: 1.3, Wash: 0.25, AutopilotRudder: 0.5,
		RollPeriod: 9, RollDamping: 0.06, PitchPeriod: 6.5, PitchDamping: 0.25, HeavePeriod: 6, HeaveDamping: 0.5, HeelPerAccel: 0.2,
		StabilizerDamping: 0.3, StabilizerCut: 0.6, StabilizerPower: 0.15e6,
	}
}

// Liner is a 290 m, 14-deck ocean liner with shafts and rudders.
func Liner() HullClass {
	return HullClass{
		Name: "liner", Length: 290, Beam: 36, Draft: 8.3, Displacement: 6.2e7, Decks: 14,
		Propulsion: Shafts, InstalledPower: 48e6, ServiceSpeed: 12, Efficiency: 0.7, AsternFraction: 0.5, PowerRamp: 1.0 / 60,
		RudderMax: 0.61, RudderRate: 0.04, SteerGain: 1.0, SteerLag: 1.5, Wash: 0.2, AutopilotRudder: 0.5,
		RollPeriod: 16, RollDamping: 0.05, PitchPeriod: 8, PitchDamping: 0.25, HeavePeriod: 8, HeaveDamping: 0.5, HeelPerAccel: 0.2,
		StabilizerDamping: 0.3, StabilizerCut: 0.6, StabilizerPower: 0.4e6,
	}
}

// Megaship is a 360 m, 20-deck megaship with azimuth pods.
func Megaship() HullClass {
	return HullClass{
		Name: "megaship", Length: 360, Beam: 48, Draft: 9, Displacement: 1.2e8, Decks: 20,
		Propulsion: Pods, InstalledPower: 62e6, ServiceSpeed: 11.8, Efficiency: 0.7, AsternFraction: 0.5, PowerRamp: 1.0 / 75,
		RudderMax: 0.61, RudderRate: 0.035, SteerGain: 1.0, SteerLag: 1.5, Wash: 0.25, AutopilotRudder: 0.5,
		RollPeriod: 18, RollDamping: 0.05, PitchPeriod: 9, PitchDamping: 0.25, HeavePeriod: 9, HeaveDamping: 0.5, HeelPerAccel: 0.2,
		StabilizerDamping: 0.3, StabilizerCut: 0.6, StabilizerPower: 0.8e6,
	}
}

// Presets returns the three built-in classes in order of size.
func Presets() []HullClass { return []HullClass{Expedition(), Liner(), Megaship()} }

const (
	// linearShare sets kLin as a share of k*ServiceSpeed.
	linearShare = 0.04
	// addedMass is the surge added-mass factor.
	addedMass = 1.1
	// bollardShare is the speed, as a fraction of ServiceSpeed, below which
	// thrust stops growing as speed falls.
	bollardShare = 0.35
	// minFlow keeps steering time constants finite at rest.
	minFlow = 1.0
)

// resistanceCoefficient returns k, in N*s^2/m^2.
func (c HullClass) resistanceCoefficient() float64 {
	v := c.ServiceSpeed
	return c.Efficiency * c.InstalledPower / float64(float64(v*v)*v)
}

// Resistance returns the hull resistance in newtons at speed u (m/s, signed).
func (c HullClass) Resistance(u float64) float64 {
	k := c.resistanceCoefficient()
	return float64(float64(k*u)*det.Abs(u)) + float64(float64(linearShare*k)*float64(c.ServiceSpeed*u))
}

// PowerForSpeed returns the power setting (0..1) that holds speed u in calm
// water.
func (c HullClass) PowerForSpeed(u float64) float64 {
	u = det.Abs(u)
	return float64(c.Resistance(u)*u) / (c.Efficiency * c.InstalledPower)
}

// SteadySpeed returns the calm-water speed that a power setting holds. It
// solves PowerForSpeed(u) = p by bisection, so it is exact for the model and
// close to ServiceSpeed*p^(1/3).
func (c HullClass) SteadySpeed(p float64) float64 {
	if p <= 0 {
		return 0
	}
	lo, hi := 0.0, float64(2*c.ServiceSpeed)
	for i := 0; i < 60; i++ {
		mid := float64(0.5 * (lo + hi))
		if c.PowerForSpeed(mid) < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return float64(0.5 * (lo + hi))
}
