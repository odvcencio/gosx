// Package parity records and compares per-step state hashes of a simulation,
// so that one run on js/wasm, amd64 and arm64 can be shown to give the same
// bits.
//
// A caller supplies an encoder that appends the simulation state to a byte
// slice. The recorder hashes the encoded bytes after each step with SHA-256.
// Two runs agree when every step hash agrees. When they do not, [Compare]
// names the first step that differs, which is the step to debug.
//
// Encode floats with [AppendFloat64] so the hash covers exact bit patterns,
// not printed decimals. Do not encode Go maps by ranging over them: map
// iteration order is random. Encode in a fixed order.
//
// Typical use in a test:
//
//	rec := parity.NewRecorder("my-sim", func(dst []byte) []byte {
//		for _, b := range world.Bodies { // slice, fixed order
//			dst = parity.AppendFloat64(dst, b.X)
//			dst = parity.AppendFloat64(dst, b.Y)
//		}
//		return dst
//	})
//	for i := 0; i < 600; i++ {
//		world.Step()
//		rec.Step()
//	}
//	if d := parity.Compare(golden, rec.Trace()); d != nil {
//		t.Fatal(d)
//	}
//
// The golden trace is recorded once, stored as JSON with [Trace.MarshalJSON],
// and checked on every platform.
package parity

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
)

// Hash is the SHA-256 of one step's encoded state.
type Hash [sha256.Size]byte

// String returns the hash in hexadecimal.
func (h Hash) String() string { return hex.EncodeToString(h[:]) }

// MarshalText implements encoding.TextMarshaler.
func (h Hash) MarshalText() ([]byte, error) { return []byte(h.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (h *Hash) UnmarshalText(b []byte) error {
	if len(b) != 2*sha256.Size {
		return fmt.Errorf("parity: hash has %d characters, want %d", len(b), 2*sha256.Size)
	}
	_, err := hex.Decode(h[:], b)
	return err
}

// Trace is the ordered list of step hashes from one run.
type Trace struct {
	// Name identifies the scenario. Compare does not use it.
	Name string `json:"name"`
	// Steps holds one hash per recorded step, in order.
	Steps []Hash `json:"steps"`
}

// Digest returns one hash that covers every step hash, for a short summary.
func (t Trace) Digest() Hash {
	h := sha256.New()
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(len(t.Steps)))
	h.Write(n[:])
	for _, s := range t.Steps {
		h.Write(s[:])
	}
	var out Hash
	copy(out[:], h.Sum(nil))
	return out
}

// Marshal returns the trace as indented JSON.
func (t Trace) Marshal() ([]byte, error) {
	return json.MarshalIndent(t, "", " ")
}

// Unmarshal parses a trace written by [Trace.Marshal].
func Unmarshal(data []byte) (Trace, error) {
	var t Trace
	err := json.Unmarshal(data, &t)
	return t, err
}

// Recorder hashes simulation state after each step.
type Recorder struct {
	name   string
	encode func(dst []byte) []byte
	buf    []byte
	steps  []Hash
}

// NewRecorder returns a recorder. encode must append the full state that the
// run claims to reproduce, in a fixed order, and return the extended slice.
func NewRecorder(name string, encode func(dst []byte) []byte) *Recorder {
	return &Recorder{name: name, encode: encode}
}

// Step encodes the current state and appends its hash to the trace.
func (r *Recorder) Step() {
	r.buf = r.encode(r.buf[:0])
	r.steps = append(r.steps, sha256.Sum256(r.buf))
}

// Trace returns a copy of the steps recorded so far.
func (r *Recorder) Trace() Trace {
	return Trace{Name: r.name, Steps: append([]Hash(nil), r.steps...)}
}

// Divergence describes the first difference between two traces.
type Divergence struct {
	// Step is the index of the first step whose hashes differ, or the length
	// of the shorter trace if the traces differ only in length.
	Step int
	// Want and Got are the hashes at Step. They are zero when a trace ends
	// before Step.
	Want, Got Hash
	// WantLen and GotLen are the trace lengths.
	WantLen, GotLen int
}

// Error implements error, so a *Divergence can be passed to t.Fatal.
func (d *Divergence) Error() string {
	if d.WantLen != d.GotLen && d.Step >= min(d.WantLen, d.GotLen) {
		return fmt.Sprintf("parity: traces have different lengths: want %d steps, got %d", d.WantLen, d.GotLen)
	}
	return fmt.Sprintf("parity: first divergence at step %d:\n  want %s\n  got  %s", d.Step, d.Want, d.Got)
}

// Compare returns nil if the traces are identical. Otherwise it returns the
// first difference.
func Compare(want, got Trace) *Divergence {
	n := min(len(want.Steps), len(got.Steps))
	for i := 0; i < n; i++ {
		if want.Steps[i] != got.Steps[i] {
			return &Divergence{Step: i, Want: want.Steps[i], Got: got.Steps[i], WantLen: len(want.Steps), GotLen: len(got.Steps)}
		}
	}
	if len(want.Steps) != len(got.Steps) {
		return &Divergence{Step: n, WantLen: len(want.Steps), GotLen: len(got.Steps)}
	}
	return nil
}

// AppendFloat64 appends the IEEE 754 bit pattern of f in little-endian order.
// NaN payloads and the sign of zero are part of the encoding.
func AppendFloat64(dst []byte, f float64) []byte {
	return binary.LittleEndian.AppendUint64(dst, math.Float64bits(f))
}

// AppendFloat32 appends the IEEE 754 bit pattern of f in little-endian order.
func AppendFloat32(dst []byte, f float32) []byte {
	return binary.LittleEndian.AppendUint32(dst, math.Float32bits(f))
}

// AppendUint64 appends v in little-endian order.
func AppendUint64(dst []byte, v uint64) []byte {
	return binary.LittleEndian.AppendUint64(dst, v)
}

// AppendInt64 appends v in little-endian order.
func AppendInt64(dst []byte, v int64) []byte {
	return binary.LittleEndian.AppendUint64(dst, uint64(v))
}
