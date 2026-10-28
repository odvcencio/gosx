//gosx:deterministic

// Package vessel is a deterministic model of a large powered displacement
// ship: a cruise liner, an expedition ship, a megaship.
//
// The model gives the same bits on js/wasm, amd64 and arm64, so a game can use
// it for replays, ghost runs and shared worlds. It follows the rules in
// docs/deterministic-simulation.md: every float product that feeds a sum is
// rounded by an explicit conversion, all trigonometry comes from package det,
// and nothing reads a clock or ranges over a map. Run `make lint-deterministic`
// after editing.
//
// # What it models
//
// A [Ship] has a hull class ([HullClass]), a state ([State]) and an optional
// wave field ([WaveField]). Each call to [Ship.Step] takes one [Input] and a
// fixed time step in seconds:
//
//   - Power is a setting from -1 (full astern) to +1 (full ahead). The engines
//     ramp toward it at a limited rate.
//   - Heading is a compass-style command in radians. An autopilot turns it into
//     a rudder (or azimuth pod) angle. Set Input.RudderOverride to command the
//     rudder directly, for example for a turning-circle test.
//   - Stabilizers switch the roll fins on or off.
//
// The outputs are the pose (position, heading, pitch, roll), speed, the power
// drawn and the thrust. The pose uses the same axes as the Scene3D sailing
// vessel: heading 0 faces -Z and the bow is toward -Z, +X is starboard, Y is
// up, positive pitch lifts the bow and positive roll lifts the starboard side.
//
// # The power curve
//
// Hull resistance grows with the square of speed, so the power needed to hold
// a speed grows with its cube:
//
//	R(u) = k*u*|u| + kLin*u            (newtons; u is speed in m/s)
//	P(u) = R(u)*u / eta               (watts at the shaft; eta is propulsive efficiency)
//	k    = eta * InstalledPower / ServiceSpeed^3
//	kLin = 0.04 * k * ServiceSpeed
//
// k is set so that full installed power holds exactly ServiceSpeed in calm
// water, to within the small linear term kLin. For a power setting p the
// steady speed is therefore close to ServiceSpeed * p^(1/3): half power gives
// about 79 percent of top speed, and a 10 percent speed cut saves about 27
// percent of the power. [SteadySpeed] and [PowerForSpeed] give the exact
// values for a class.
//
// Thrust is delivered power divided by speed, capped at low speed by a
// bollard-pull limit: T = eta*P/max(|u|, 0.35*ServiceSpeed). The ship
// accelerates by (T - R)/m, where m is the displacement plus 10 percent
// added mass.
//
// # Steering
//
// Steering uses a first-order Nomoto model. At steady state the yaw rate is
//
//	r = Gain * rudder * flow / Length
//
// so the turning radius is Length/(Gain*rudder), whatever the speed. The
// flow past the rudder is the ship speed plus a propeller-wash term that lets
// a ship with power on turn slowly at low speed. The yaw rate approaches r with
// a time constant of 1.5*Length/flow (about 45 s for a megaship). The hull
// drifts outward in a turn, loses speed to rudder drag, and heels outward. See
// [TurningCircle] and [StoppingDistance] for measured turning and stopping
// numbers.
//
// # Waves
//
// Each step samples the water surface at five points under the hull, with the
// pose heading applied. Heave, pitch and roll follow what those samples give
// through second-order springs with the hull's own natural periods. A long hull
// averages short waves out, as a real one does. Stabilizers add roll damping
// and cut the wave and turn forcing on roll.
//
// [Sea] is a deterministic wave field built from the same six-component table
// as the Scene3D ocean or, when [SeaConfig].Swell is set, from the swell waves
// of a spectral ocean (scene.OceanSwellWaves). Any other type with a Height
// method works too.
package vessel
