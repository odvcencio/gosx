package scene

// OceanWaterFrame says where the drawn water is, for a host that moves a
// vessel's pose itself (an external-pose host). The page reads it from the
// renderer; it is for drawing only. Presentation never feeds a simulation: do
// not use a frame in a game rule, a save or a replay, because it depends on
// frame timing and on which backend drew the sea.
//
// X and Z are the water's origin in world metres: the drift the water has
// moved since the scene started. Time is the wave time in seconds. A point at
// world (x, z) sits in the drawn waves at water coordinates (x - X, z - Z) at
// wave time Time, which is what OceanQuery wants (see OceanQuery.SampleWater).
//
// Spectral is true when the spectral (FFT) sea is drawn this frame, on WebGPU
// or on WebGL2. It is false when the scene draws the Gerstner sea: an Ocean
// without a Spectrum, or a spectral Ocean whose graph could not run (the
// renderer fell back). A Gerstner sea has no drift: X and Z are zero and Time
// is the scene clock, and OceanQuery applies Ocean.Speed itself.
//
// TimeScale, DriftX and DriftZ are the rates the water moves at, in wave
// seconds per scene second and metres per scene second. They let a host
// carry a reading forward with Advance. Frame is the page's count of drawn
// frames with an ocean, and AtMS is the Unix time in milliseconds at which the
// page recorded the reading (the page's performance.timeOrigin plus
// performance.now()). A host compares Frame, or AtMS with its own clock, to tell
// a stale reading from a current one.
//
// Latency. A reading is the frame the renderer drew last, and the page does
// not run ahead of it. A pose the host sends is drawn by the next render, so
// for that draw the reading is 1 frame old: Frame is one lower and Time is one
// frame of dt behind (the tests pin both). On the main thread the reader sees
// the page's record in place, so it is never older than that. In an engine
// worker the page posts each frame as a message and the worker applies it
// between Go callbacks, so the reading is 1 frame plus the message hop, which
// the tests do not measure. Advance(age) closes the gap: take age from AtMS and
// the host's own Unix clock, or use one frame of dt.
type OceanWaterFrame struct {
	X, Z, Time float64
	Spectral   bool

	TimeScale, DriftX, DriftZ float64

	Frame uint64
	AtMS  float64
}

// OceanWaterFloats is the length of the wire record the page keeps for a
// scene: a Float64Array of this many values, in the order of the OceanWater
// index constants.
const OceanWaterFloats = 9

// Indices of the wire record.
const (
	OceanWaterIdxFrame = iota
	OceanWaterIdxTime
	OceanWaterIdxX
	OceanWaterIdxZ
	OceanWaterIdxSpectral
	OceanWaterIdxTimeScale
	OceanWaterIdxDriftX
	OceanWaterIdxDriftZ
	OceanWaterIdxAtMS
)

// OceanWaterFromWire decodes the page's wire record. It reports false when the
// page has not recorded a frame yet (Frame is zero).
func OceanWaterFromWire(v *[OceanWaterFloats]float64) (OceanWaterFrame, bool) {
	f := OceanWaterFrame{
		X: v[OceanWaterIdxX], Z: v[OceanWaterIdxZ], Time: v[OceanWaterIdxTime],
		Spectral:  v[OceanWaterIdxSpectral] != 0,
		TimeScale: v[OceanWaterIdxTimeScale], DriftX: v[OceanWaterIdxDriftX], DriftZ: v[OceanWaterIdxDriftZ],
		Frame: uint64(v[OceanWaterIdxFrame]), AtMS: v[OceanWaterIdxAtMS],
	}
	return f, f.Frame != 0
}

// Advance returns the frame carried forward by seconds of scene time at the
// rates it holds. Use it to cover the age of a reading (a negative age is
// refused: the frame is returned unchanged). It uses only addition and
// multiplication, each product rounded before the sum, so it gives the same
// value on every platform.
func (f OceanWaterFrame) Advance(seconds float64) OceanWaterFrame {
	if !(seconds > 0) {
		return f
	}
	f.Time += float64(seconds * f.TimeScale)
	f.X += float64(seconds * f.DriftX)
	f.Z += float64(seconds * f.DriftZ)
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
// The Go query sums the spectrum's strongest swell waves, not the full FFT
// surface, so the height is the swell's height, not the height of every
// ripple. The frame is exact: it is the offset and time the shader uses.
func (q *OceanQuery) SampleWater(f OceanWaterFrame, x, z float64) OceanSample {
	s := q.Sample(x-f.X, z-f.Z, f.Time)
	s.Position.X += f.X
	s.Position.Z += f.Z
	s.Parameter.X += f.X
	s.Parameter.Z += f.Z
	return s
}
