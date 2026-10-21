package vessel

import (
	"encoding/binary"
	"errors"

	"m31labs.dev/gosx/det"
)

// stateFields is the number of float64 values in an encoded [State].
const stateFields = 16

// StateSize is the byte length of an encoded [State].
const StateSize = 8 * stateFields

func (t *State) fields() [stateFields]float64 {
	return [stateFields]float64{t.X, t.Y, t.Z, t.Heading, t.Pitch, t.Roll, t.Speed, t.YawRate, t.Drift,
		t.Rudder, t.Power, t.Stabilizer, t.HeaveRate, t.PitchRate, t.RollRate, t.Time}
}

// AppendState appends the exact bit patterns of the state to dst, in a fixed
// order. Use it for save files, replay checkpoints and parity hashes.
func (s *Ship) AppendState(dst []byte) []byte {
	for _, v := range s.State.fields() {
		dst = binary.LittleEndian.AppendUint64(dst, det.Float64bits(v))
	}
	return dst
}

// RestoreState loads a state written by [Ship.AppendState]. The ship then
// continues bit-for-bit as the saved one would have.
func (s *Ship) RestoreState(b []byte) error {
	if len(b) != StateSize {
		return errors.New("vessel: state has the wrong length")
	}
	var f [stateFields]float64
	for i := range f {
		f[i] = det.Float64frombits(binary.LittleEndian.Uint64(b[8*i:]))
	}
	t := &s.State
	t.X, t.Y, t.Z, t.Heading, t.Pitch, t.Roll, t.Speed, t.YawRate, t.Drift = f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7], f[8]
	t.Rudder, t.Power, t.Stabilizer, t.HeaveRate, t.PitchRate, t.RollRate, t.Time = f[9], f[10], f[11], f[12], f[13], f[14], f[15]
	return nil
}
