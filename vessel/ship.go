package vessel

import "m31labs.dev/gosx/det"

// Input is the command for one tick.
type Input struct {
	// Power is the power setting, -1 (full astern) to +1 (full ahead).
	Power float64
	// Heading is the commanded heading in radians, 0 facing -Z, positive
	// turning toward -X. The autopilot steers toward it.
	Heading float64
	// Stabilizers switches the roll fins on.
	Stabilizers bool
	// RudderOverride makes the ship follow Rudder instead of the autopilot.
	RudderOverride bool
	// Rudder is the rudder command from -1 to +1 of RudderMax, positive
	// turning toward increasing heading. Used only with RudderOverride.
	Rudder float64
}

// State is everything that changes while the ship runs. It is the full
// simulation state: restore it and the ship continues bit-for-bit.
type State struct {
	X, Y, Z                        float64 // position of the hull origin at the waterline, metres
	Heading, Pitch, Roll           float64 // radians
	Speed                          float64 // forward speed through the water, m/s, negative astern
	YawRate                        float64 // rad/s, positive toward increasing heading
	Drift                          float64 // drift angle of the velocity relative to the heading, rad
	Rudder                         float64 // current rudder angle, rad
	Power                          float64 // current power setting after engine ramp, -1..1
	Stabilizer                     float64 // fin effect 0..1
	HeaveRate, PitchRate, RollRate float64
	Time                           float64 // simulation seconds
}

// Ship is one powered hull. Create it with [NewShip]. The Sea field may be set
// or changed at any time; nil means a flat sea at height 0.
type Ship struct {
	Class HullClass
	State State
	Sea   WaveField

	k, kLin, uRef float64
	thrust, drawn float64
}

// NewShip returns a ship of the given class at rest at (x, z), heading
// radians, floating at the waterline given by the sea (or 0).
func NewShip(c HullClass, x, z, heading float64, sea WaveField) *Ship {
	s := &Ship{Class: c, Sea: sea}
	k := c.resistanceCoefficient()
	s.k = k
	s.kLin = float64(float64(linearShare*k) * c.ServiceSpeed)
	s.uRef = float64(bollardShare * c.ServiceSpeed)
	s.State = State{X: x, Z: z, Heading: wrapAngle(heading)}
	if sea != nil {
		s.State.Y = sea.Height(x, z, 0)
	}
	return s
}

// Pose is the output a renderer needs.
type Pose struct {
	X, Y, Z, Heading, Pitch, Roll float64
}

// Pose returns the current pose.
func (s *Ship) Pose() Pose {
	t := &s.State
	return Pose{t.X, t.Y, t.Z, t.Heading, t.Pitch, t.Roll}
}

// Speed returns the forward speed in m/s.
func (s *Ship) Speed() float64 { return s.State.Speed }

// PowerDrawn returns the power drawn by propulsion and stabilizers in watts,
// as of the last step.
func (s *Ship) PowerDrawn() float64 { return s.drawn }

// Thrust returns the propulsion thrust in newtons as of the last step.
func (s *Ship) Thrust() float64 { return s.thrust }

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// wrapAngle maps a to (-pi, pi].
func wrapAngle(a float64) float64 {
	if a > -pi && a <= pi {
		return a
	}
	a -= float64(twoPi * det.Floor(float64(a/twoPi)+0.5))
	if a <= -pi {
		a += twoPi
	}
	return a
}

// Step advances the ship by dt seconds. Use one constant dt for a replay.
func (s *Ship) Step(in Input, dt float64) {
	c := &s.Class
	st := &s.State

	// Engines ramp toward the commanded power.
	cmd := clamp(in.Power, -1, 1)
	if cmd < 0 {
		cmd = float64(cmd * c.AsternFraction)
	}
	st.Power += clamp(cmd-st.Power, -float64(c.PowerRamp*dt), float64(c.PowerRamp*dt))
	u := st.Speed

	// Surge.
	flowRef := det.Max(det.Abs(u), s.uRef)
	s.thrust = float64(st.Power*c.Efficiency*c.InstalledPower) / flowRef
	rudderShare := float64(st.Rudder / c.RudderMax)
	drag := float64(float64(float64(s.k*u)*det.Abs(u))*(1+float64(0.3*float64(rudderShare*rudderShare)))) + float64(s.kLin*u)
	acc := (s.thrust - drag) / float64(c.Displacement*addedMass)
	next := u + float64(acc*dt)
	if float64(next*u) < 0 && det.Abs(st.Power) < 1e-3 {
		next = 0
	}
	st.Speed = next
	u = next

	// Steering.
	flow := u + float64(float64(c.Wash*c.ServiceSpeed)*st.Power)
	flowAbs := det.Max(det.Abs(flow), minFlow)
	var want float64
	if in.RudderOverride {
		want = float64(clamp(in.Rudder, -1, 1) * c.RudderMax)
	} else {
		err := wrapAngle(in.Heading - st.Heading)
		kp := c.SteerLag / float64(4*c.SteerGain)
		kd := det.Max(0.2, float64(1.1*c.SteerLag)-1) * c.Length / float64(c.SteerGain*flowAbs)
		lim := float64(c.AutopilotRudder * c.RudderMax)
		want = clamp(float64(kp*err)-float64(kd*st.YawRate), -lim, lim)
		if flow < 0 {
			want = -want
		}
	}
	slew := float64(c.RudderRate * dt)
	st.Rudder += clamp(want-st.Rudder, -slew, slew)
	yawSteady := float64(float64(c.SteerGain*st.Rudder)*flow) / c.Length
	tn := float64(c.SteerLag*c.Length) / flowAbs
	st.YawRate += float64(float64(yawSteady-st.YawRate) * det.Min(1, dt/tn))
	st.Heading = wrapAngle(st.Heading + float64(st.YawRate*dt))
	driftSteady := clamp(-float64(0.15*float64(st.YawRate*c.Length))/det.Max(det.Abs(u), minFlow), -0.3, 0.3)
	st.Drift += float64(float64(driftSteady-st.Drift) * det.Min(1, dt/tn))

	// Position: velocity points along heading plus drift.
	course := st.Heading + st.Drift
	st.X -= float64(float64(det.Sin(course)*u) * dt)
	st.Z -= float64(float64(det.Cos(course)*u) * dt)
	st.Time += dt

	s.seaMotion(in, dt)

	s.drawn = float64(det.Abs(st.Power)*c.InstalledPower) + float64(st.Stabilizer*c.StabilizerPower)
}

// seaMotion updates heave, pitch, roll and the fins from the wave samples.
func (s *Ship) seaMotion(in Input, dt float64) {
	c := &s.Class
	st := &s.State

	// Fins ramp over about ten seconds and lose strength below half service speed.
	finGoal := 0.0
	if in.Stabilizers {
		r := det.Min(1, det.Abs(st.Speed)/float64(0.5*c.ServiceSpeed))
		finGoal = float64(r * r)
	}
	st.Stabilizer += clamp(finGoal-st.Stabilizer, -float64(0.1*dt), float64(0.1*dt))

	var level, bow, stern, port, starboard float64
	if s.Sea != nil {
		sin, cos := det.Sin(st.Heading), det.Cos(st.Heading)
		at := func(lx, lz float64) float64 {
			wx := st.X + float64(cos*lx) + float64(sin*lz)
			wz := st.Z - float64(sin*lx) + float64(cos*lz)
			return s.Sea.Height(wx, wz, st.Time)
		}
		along, across := float64(0.4*c.Length), float64(0.45*c.Beam)
		bow, stern = at(0, -along), at(0, along)
		port, starboard = at(-across, 0), at(across, 0)
		level = at(0, 0)
		level = float64(0.2 * (level + bow + stern + port + starboard))
	}

	// Heave.
	wh := twoPi / c.HeavePeriod
	st.HeaveRate += float64((float64(float64(wh*wh)*(level-st.Y)) - float64(float64(2*c.HeaveDamping)*float64(wh*st.HeaveRate))) * dt)
	st.Y += float64(st.HeaveRate * dt)

	// Pitch: positive lifts the bow.
	pitchGoal := clamp(det.Atan2(bow-stern, float64(2*float64(0.4*c.Length))), -0.2, 0.2)
	wp := twoPi / c.PitchPeriod
	st.PitchRate += float64((float64(float64(wp*wp)*(pitchGoal-st.Pitch)) - float64(float64(2*c.PitchDamping)*float64(wp*st.PitchRate))) * dt)
	st.Pitch = clamp(st.Pitch+float64(st.PitchRate*dt), -0.25, 0.25)

	// Roll: positive lifts the starboard side. Turning heels the ship outward.
	cut := 1 - float64(c.StabilizerCut*st.Stabilizer)
	wave := det.Atan2(starboard-port, float64(2*float64(0.45*c.Beam)))
	heel := -float64(float64(c.HeelPerAccel*st.Speed) * st.YawRate)
	rollGoal := clamp(float64(cut*(wave+heel)), -0.35, 0.35)
	zeta := c.RollDamping + float64(c.StabilizerDamping*st.Stabilizer)
	wr := twoPi / c.RollPeriod
	st.RollRate += float64((float64(float64(wr*wr)*(rollGoal-st.Roll)) - float64(float64(2*zeta)*float64(wr*st.RollRate))) * dt)
	st.Roll = clamp(st.Roll+float64(st.RollRate*dt), -0.4, 0.4)
}
