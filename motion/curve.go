package motion

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// railSamplesPerSegment is how many orientation samples CameraRail emits per
// segment between two stops. Orientation is sampled (not interpolated as Euler
// angles) so the camera keeps looking at the interpolated look-at point.
const railSamplesPerSegment = 12

// CurveStop is one numeric sample of a curve signal.
type CurveStop struct {
	At    float64
	Value float64
}

// CurveValue evaluates a piecewise scalar curve at x. Outside the stops the
// curve holds its end values. With smooth false it interpolates linearly; with
// smooth true it uses a cubic Hermite spline with monotone-cubic tangents
// (Fritsch-Carlson), so the curve passes through every stop without kinks and
// never overshoots the values of the two stops around it. The JavaScript
// runtime implements the same formulas; testdata/curve_golden.json pins both.
func CurveValue(stops []CurveStop, smooth bool, x float64) float64 {
	n := len(stops)
	if n == 0 {
		return 0
	}
	if n == 1 || x <= stops[0].At {
		return stops[0].Value
	}
	if x >= stops[n-1].At {
		return stops[n-1].Value
	}
	i := 0
	for i < n-2 && x >= stops[i+1].At {
		i++
	}
	a, b := stops[i], stops[i+1]
	h := b.At - a.At
	t := (x - a.At) / h
	if !smooth {
		return a.Value + (b.Value-a.Value)*t
	}
	ma := curveTangent(stops, i)
	mb := curveTangent(stops, i+1)
	t2, t3 := t*t, t*t*t
	return (2*t3-3*t2+1)*a.Value + (t3-2*t2+t)*h*ma + (-2*t3+3*t2)*b.Value + (t3-t2)*h*mb
}

// curveTangent returns the monotone-cubic (Fritsch-Carlson) tangent at stop k.
// It is zero at local extrema and never lets the spline leave the value range
// of the two stops that bound a segment, so a smooth curve cannot overshoot.
func curveTangent(stops []CurveStop, k int) float64 {
	n := len(stops)
	if n < 2 {
		return 0
	}
	delta := func(i int) float64 { return (stops[i+1].Value - stops[i].Value) / (stops[i+1].At - stops[i].At) }
	if n == 2 {
		return delta(0)
	}
	if k > 0 && k < n-1 {
		d0, d1 := delta(k-1), delta(k)
		if d0*d1 <= 0 {
			return 0
		}
		h0, h1 := stops[k].At-stops[k-1].At, stops[k+1].At-stops[k].At
		w1, w2 := 2*h1+h0, h1+2*h0
		return (w1 + w2) / (w1/d0 + w2/d1)
	}
	// Endpoint: shape-preserving three-point estimate.
	var h0, h1, d0, d1 float64
	if k == 0 {
		h0, h1 = stops[1].At-stops[0].At, stops[2].At-stops[1].At
		d0, d1 = delta(0), delta(1)
	} else {
		h0, h1 = stops[n-1].At-stops[n-2].At, stops[n-2].At-stops[n-3].At
		d0, d1 = delta(n-2), delta(n-3)
	}
	m := ((2*h0+h1)*d0 - h0*d1) / (h0 + h1)
	if m*d0 <= 0 {
		return 0
	}
	if d0*d1 <= 0 && math.Abs(m) > 3*math.Abs(d0) {
		return 3 * d0
	}
	return m
}

// Curve adds a signal that maps an input signal through a piecewise curve. The
// stops' At values are in the input signal's units and must strictly increase.
func (p *Program) Curve(id string, input SignalRef, stops []CurveStop, smooth bool) SignalRef {
	frames := make([]MotionKeyframe, len(stops))
	for i, stop := range stops {
		frames[i] = MotionKeyframe{At: stop.At, Value: stop.Value}
	}
	return p.add(SignalSpec{ID: id, Kind: SignalCurve, Input: input, Frames: frames, Smooth: smooth})
}

// RailStop is one waypoint of a camera rail.
type RailStop struct {
	// At is the progress value (in the driving signal's units) at this stop.
	At       float64
	Position [3]float64
	LookAt   [3]float64
	// FOV is the vertical field of view in degrees. Set it on every stop or on
	// none; zero on every stop leaves the camera's field of view alone.
	FOV float64
}

// CameraRail drives a Scene3D camera along a spline through the stops. The
// progress signal (scroll, time, or any signal) selects the position on the
// rail. Position and FOV follow a smooth spline through the stops; the camera
// orientation always points at the look-at point interpolated the same way, so
// two stops with different look-at points pan smoothly between them. It adds
// signals named "<id>.x", ".y", ".z", ".rotationX", ".rotationY" and, when the
// stops set FOV, ".fov", and binds each to the camera of sceneSelector.
func (p *Program) CameraRail(id string, progress SignalRef, sceneSelector string, stops []RailStop) error {
	if p == nil {
		return errors.New("motion: nil program")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("motion: camera rail id is required")
	}
	if strings.TrimSpace(sceneSelector) == "" {
		return errors.New("motion: camera rail scene selector is required")
	}
	if len(stops) < 2 {
		return errors.New("motion: camera rail needs at least two stops")
	}
	withFOV := 0
	for i, stop := range stops {
		if i > 0 && !(stop.At > stops[i-1].At) {
			return fmt.Errorf("motion: camera rail stop %d must have a larger At than the stop before it", i)
		}
		for _, v := range append(append([]float64{stop.At, stop.FOV}, stop.Position[:]...), stop.LookAt[:]...) {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("motion: camera rail stop %d has a non-finite value", i)
			}
		}
		if stop.FOV != 0 {
			withFOV++
			if stop.FOV < 1 || stop.FOV > 179 {
				return fmt.Errorf("motion: camera rail stop %d FOV must be within 1 to 179 degrees", i)
			}
		}
	}
	if withFOV != 0 && withFOV != len(stops) {
		return errors.New("motion: camera rail FOV must be set on every stop or none")
	}

	component := func(pick func(RailStop) float64) []CurveStop {
		out := make([]CurveStop, len(stops))
		for i, stop := range stops {
			out[i] = CurveStop{At: stop.At, Value: pick(stop)}
		}
		return out
	}
	pos := [3][]CurveStop{
		component(func(s RailStop) float64 { return s.Position[0] }),
		component(func(s RailStop) float64 { return s.Position[1] }),
		component(func(s RailStop) float64 { return s.Position[2] }),
	}
	look := [3][]CurveStop{
		component(func(s RailStop) float64 { return s.LookAt[0] }),
		component(func(s RailStop) float64 { return s.LookAt[1] }),
		component(func(s RailStop) float64 { return s.LookAt[2] }),
	}

	// Sample orientation densely from the interpolated position and look-at.
	var pitch, yaw []CurveStop
	prevYaw, prevPitch, haveDir := 0.0, 0.0, false
	for seg := 0; seg < len(stops)-1; seg++ {
		for k := 0; k <= railSamplesPerSegment; k++ {
			if seg > 0 && k == 0 {
				continue
			}
			at := stops[seg].At + (stops[seg+1].At-stops[seg].At)*float64(k)/railSamplesPerSegment
			if k == railSamplesPerSegment {
				at = stops[seg+1].At
			}
			var d [3]float64
			for c := 0; c < 3; c++ {
				d[c] = CurveValue(look[c], true, at) - CurveValue(pos[c], true, at)
			}
			length := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
			rx, ry := prevPitch, prevYaw
			if length <= 1e-9 {
				return fmt.Errorf("motion: camera rail passes through its look-at point at progress %v; move a stop or the look-at point", at)
			}
			if length > 1e-9 {
				rx = math.Asin(math.Max(-1, math.Min(1, d[1]/length)))
				ry = math.Atan2(-d[0], -d[2])
				if haveDir {
					// Keep yaw continuous across the +-pi seam.
					for ry-prevYaw > math.Pi {
						ry -= 2 * math.Pi
					}
					for ry-prevYaw < -math.Pi {
						ry += 2 * math.Pi
					}
				}
				prevPitch, prevYaw, haveDir = rx, ry, true
			}
			if n := len(yaw); n > 0 && (math.Abs(ry-yaw[n-1].Value) > math.Pi/2 || math.Abs(rx-pitch[n-1].Value) > math.Pi/2) {
				return fmt.Errorf("motion: camera rail swings more than 90 degrees between progress %v and %v (it passes almost through its look-at point); add stops or move the look-at point", yaw[n-1].At, at)
			}
			pitch = append(pitch, CurveStop{At: at, Value: rx})
			yaw = append(yaw, CurveStop{At: at, Value: ry})
		}
	}

	x := p.Curve(id+".x", progress, pos[0], true)
	y := p.Curve(id+".y", progress, pos[1], true)
	z := p.Curve(id+".z", progress, pos[2], true)
	rx := p.Curve(id+".rotationX", progress, pitch, false)
	ry := p.Curve(id+".rotationY", progress, yaw, false)
	p.BindCamera(x, sceneSelector, "position.x")
	p.BindCamera(y, sceneSelector, "position.y")
	p.BindCamera(z, sceneSelector, "position.z")
	p.BindCamera(rx, sceneSelector, "rotation.x")
	p.BindCamera(ry, sceneSelector, "rotation.y")
	if withFOV != 0 {
		fov := p.Curve(id+".fov", progress, component(func(s RailStop) float64 { return s.FOV }), true)
		p.BindCamera(fov, sceneSelector, "fov")
	}
	return nil
}

func validateCurveSignal(signal SignalSpec) error {
	if strings.TrimSpace(string(signal.Input)) == "" {
		return fmt.Errorf("motion: curve %q needs an input signal", signal.ID)
	}
	if len(signal.Frames) < 2 {
		return fmt.Errorf("motion: curve %q needs at least two stops", signal.ID)
	}
	prev := math.Inf(-1)
	for i, frame := range signal.Frames {
		value, ok := frame.Value.(float64)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || math.IsNaN(frame.At) || math.IsInf(frame.At, 0) {
			return fmt.Errorf("motion: curve %q stop %d must be a finite number", signal.ID, i)
		}
		if !(frame.At > prev) {
			return fmt.Errorf("motion: curve %q stops must strictly increase", signal.ID)
		}
		prev = frame.At
	}
	return nil
}
