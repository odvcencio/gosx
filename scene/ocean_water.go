package scene

// OceanWaterFrame says where the drawn water is: its drift offset and wave
// time, in the renderer's own terms. It is for a host that moves a vessel's
// pose itself (an external-pose host). The page reads it from the renderer, and
// it is for drawing only. Presentation never feeds a simulation: do not use a
// frame in a game rule, a save or a replay, because it depends on frame timing
// and on which backend drew the sea.
//
// X and Z are the water's origin in world metres: the drift the water has
// moved since the graph started. Time is the wave time in seconds. A point at
// world (x, z) sits in the drawn waves at water coordinates (x - X, z - Z) at
// wave time Time, which is what OceanQuery wants (see OceanQuery.SampleWater).
//
// What the frame is and is not. It is exact: the offset and time the shader
// draws with. It gives the phase and drift of the drawn sea. It does not give
// the drawn heights. OceanQuery sums only the strongest swell waves of a
// spectrum (24 bins of the first two tiles), and the drawn sea is the sum of
// about 12,000 bins on four tiles. Measured against the full sum, the swell
// waves leave a heave error at a hull-sized footprint of about 93% of the
// signal in a calm sea, 82% in a moderate sea, 67% in a rough sea and 57% in a
// storm, and an error of 97% to 99% in the slope at a point. Use the frame to
// stay in phase with the drawn sea; do not expect SampleWater to reproduce its
// heights. A GPU readback of the drawn heights at host-given points is a
// later step.
//
// Spectral is true when the spectral (FFT) sea is drawn, on WebGPU or on
// WebGL2. It is false when the scene draws the Gerstner sea: an Ocean without
// a Spectrum, or a spectral Ocean whose graph could not run (still building,
// failed, or torn down). A Gerstner sea has no drift: X and Z are zero and Time
// is the scene clock, and OceanQuery applies Ocean.Speed itself.
//
// TimeScale, WaterDriftX and WaterDriftZ are the rates the water moves at, in
// wave seconds per scene second and metres per scene second. They let a host
// carry a reading forward with Advance. Seq counts the frames the renderer
// submitted with an ocean, so it grows only for frames that were drawn.
// PageEpochMS is the page's Unix time in milliseconds when it recorded the
// reading (performance.timeOrigin plus performance.now()). Generation changes
// when the graph is rebuilt (a tier change, a lost device, a switch between
// spectral and Gerstner); Time, X and Z restart at that point. Do not use Time
// as a monotonic clock for a filter: it can restart, and it stops while the
// scene is paused. Keep the host's own clock for that.
//
// A reader keeps returning the last frame after the ocean is removed or the
// renderer stops drawing: Seq and PageEpochMS stop moving. A host that must
// notice should treat a reading whose Seq has not changed for a few frames as
// absent.
//
// Latency. A reading is the last drawn frame, and the page does not run ahead
// of it. A pose the host sends is drawn by the next render, so for that draw
// the reading is 1 frame old: Seq is one lower and Time is one frame of dt
// behind (the tests pin both). On the main thread the reader sees the page's
// record in place, so it is never older than that. In an engine worker the page
// posts each frame as a message and the worker applies it between Go callbacks,
// so the reading is 1 frame plus the message hop, which the tests do not
// measure. Advance(age) closes the gap. Cap the age at about one frame (a
// fraction of a second): the worker's clock and the page's clocks are not
// synchronized, so a large age from comparing PageEpochMS with the host's own
// time is not a latency, it is skew.
type OceanWaterFrame struct {
	X, Z, Time float64
	Spectral   bool

	TimeScale, WaterDriftX, WaterDriftZ float64

	Seq, Generation uint64
	PageEpochMS     float64
}

// Advance returns the frame carried forward by seconds of scene time at the
// rates it holds. Use it to cover the age of a reading (a zero, negative or NaN
// age is refused: the frame is returned unchanged). It uses only addition and
// multiplication, each product rounded before the sum, so it gives the same
// value on every platform.
func (f OceanWaterFrame) Advance(seconds float64) OceanWaterFrame {
	if !(seconds > 0) {
		return f
	}
	f.Time += float64(seconds * f.TimeScale)
	f.X += float64(seconds * f.WaterDriftX)
	f.Z += float64(seconds * f.WaterDriftZ)
	return f
}

// ToWater maps a world position to water coordinates.
func (f OceanWaterFrame) ToWater(x, z float64) (float64, float64) { return x - f.X, z - f.Z }

// FromWater maps water coordinates back to a world position.
func (f OceanWaterFrame) FromWater(x, z float64) (float64, float64) { return x + f.X, z + f.Z }

// SampleWater samples the ocean above world (x, z) in the water the frame
// describes: it moves the point into water coordinates, queries at the frame's
// wave time and moves the result back, so Position.X, Position.Z and
// Parameter are world coordinates. It does what the page does for a vessel
// that GoSX floats itself.
//
// The query sums the spectrum's strongest swell waves, not the drawn surface;
// see OceanWaterFrame for how far that is from the drawn heights and slopes.
func (q *OceanQuery) SampleWater(f OceanWaterFrame, x, z float64) OceanSample {
	s := q.Sample(x-f.X, z-f.Z, f.Time)
	s.Position.X += f.X
	s.Position.Z += f.Z
	s.Parameter.X += f.X
	s.Parameter.Z += f.Z
	return s
}
