package vessel

import "m31labs.dev/gosx/det"

// TurnReport is the result of [TurningCircle]. Distances are metres.
type TurnReport struct {
	Advance          float64 // distance gained along the original heading when the heading has changed 90 degrees
	Transfer         float64 // sideways distance at 90 degrees
	TacticalDiameter float64 // sideways distance at 180 degrees
	SteadyRadius     float64 // speed divided by yaw rate once the turn is steady
	Time180          float64 // seconds to turn 180 degrees
	SteadySpeed      float64 // speed at the end of the turn, m/s
}

// TurningCircle holds the power setting steady at the speed it gives, puts the
// rudder hard over toward increasing heading, and measures the turn in calm
// water. It runs at the fixed step dt (use 0.5 if unsure).
func TurningCircle(c HullClass, power, dt float64) TurnReport {
	s := NewShip(c, 0, 0, 0, nil)
	s.State.Speed = c.SteadySpeed(power)
	s.State.Power = power
	var r TurnReport
	got90, got180 := false, false
	in := Input{Power: power, RudderOverride: true, Rudder: 1}
	for i := 0; i < int(float64(4*3600)/dt); i++ {
		s.Step(in, dt)
		t := &s.State
		sideX, sideZ := -det.Cos(0), det.Sin(0) // turn-side unit vector at heading 0
		fwdX, fwdZ := -det.Sin(0), -det.Cos(0)
		along := float64(t.X*fwdX) + float64(t.Z*fwdZ)
		across := float64(t.X*sideX) + float64(t.Z*sideZ)
		if !got90 && t.Heading >= pi/2 {
			got90, r.Advance, r.Transfer = true, along, across
		}
		if !got180 && (t.Heading >= pi-0.001 || t.Heading < 0) {
			got180, r.TacticalDiameter, r.Time180 = true, across, t.Time
			break
		}
	}
	r.SteadySpeed = s.State.Speed
	if s.State.YawRate > 0 {
		r.SteadyRadius = s.State.Speed / s.State.YawRate
	}
	return r
}

// StopReport is the result of [StoppingDistance].
type StopReport struct {
	Distance float64 // metres travelled from the astern order to rest
	Time     float64 // seconds from the astern order to rest
}

// StoppingDistance runs the ship at the speed that power gives, then orders
// full astern with the rudder amidships, and measures the track to rest in
// calm water. It runs at the fixed step dt (use 0.5 if unsure). If power is 0
// or negative, the ship coasts (no astern order).
func StoppingDistance(c HullClass, power, dt float64) StopReport {
	s := NewShip(c, 0, 0, 0, nil)
	s.State.Speed = c.SteadySpeed(power)
	s.State.Power = power
	order := -1.0
	if power <= 0 {
		order = 0
	}
	in := Input{Power: order, RudderOverride: true}
	for i := 0; i < int(float64(4*3600)/dt); i++ {
		s.Step(in, dt)
		if s.State.Speed < 0.1 {
			break
		}
	}
	return StopReport{Distance: -s.State.Z, Time: s.State.Time}
}
