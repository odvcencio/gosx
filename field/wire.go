package field

import (
	"encoding/binary"
	"fmt"
	"math"

	enc "m31labs.dev/gosx/crdt/encoding"
)

// Compact binary wire form for Quantized.
//
// The JSON transport in stream.go costs about 33% more bytes than the packed
// payload alone. Standard encoding/json base64-encodes every []byte, which
// turns 3 payload bytes into 4 characters, and it prints Mins and Maxs as
// decimal text. A 64-cube vec3 field at 6 bits packs to 589,824 bytes and
// leaves the socket as 786,625 JSON bytes.
//
// The binary form writes the header as ULEB128 varints and little-endian
// float32 values, then copies the packed payload byte for byte. Its size is the
// packed payload plus a header of about 30 to 60 bytes.
//
// The compression ratio of the codec itself is unchanged. Quantization is
// fixed-rate and scalar, so the ratio against float32 input is exactly
// 32/BitWidth. Delta encoding narrows the quantized range and lowers the
// reconstruction error; it does not change the byte count.

// quantizedWireVersion is the first byte of every binary Quantized payload.
const quantizedWireVersion byte = 0x01

// Wire flag bits.
const (
	wireFlagIsDelta    = 1 << 0 // Packed holds deltas, not values
	wireFlagHasPreview = 1 << 1 // a preview payload follows
)

// maxWireComponents guards the decoder against a hostile component count.
const maxWireComponents = 4

// MarshalBinary encodes q in the compact binary wire form. It implements
// encoding.BinaryMarshaler.
func (q *Quantized) MarshalBinary() ([]byte, error) {
	if q == nil {
		return nil, fieldError("field.MarshalBinary", "quantized field is nil")
	}
	if err := validateNewArgs("field.MarshalBinary", q.Resolution, q.Components); err != nil {
		return nil, err
	}
	if q.BitWidth < 4 || q.BitWidth > 8 {
		return nil, fieldError("field.MarshalBinary", "BitWidth must be 4..8, got %d", q.BitWidth)
	}
	if len(q.Mins) < q.Components || len(q.Maxs) < q.Components {
		return nil, fieldError("field.MarshalBinary",
			"mins/maxs length must be at least components (%d/%d < %d)",
			len(q.Mins), len(q.Maxs), q.Components)
	}

	// Header size: version, three resolutions, components, six bounds floats,
	// bit width, flags, and the per-component ranges.
	header := 1 + 3*5 + 1 + 24 + 1 + 1 + 8*q.Components + 10
	buf := make([]byte, 0, header+len(q.Packed)+len(q.Preview)+6)

	buf = append(buf, quantizedWireVersion)
	buf = enc.AppendULEB128(buf, uint64(q.Resolution[0]))
	buf = enc.AppendULEB128(buf, uint64(q.Resolution[1]))
	buf = enc.AppendULEB128(buf, uint64(q.Resolution[2]))
	buf = append(buf, byte(q.Components))
	for axis := 0; axis < 3; axis++ {
		buf = appendFloat32(buf, q.Bounds.Min[axis])
	}
	for axis := 0; axis < 3; axis++ {
		buf = appendFloat32(buf, q.Bounds.Max[axis])
	}
	buf = append(buf, byte(q.BitWidth))

	var flags byte
	if q.IsDelta {
		flags |= wireFlagIsDelta
	}
	hasPreview := len(q.Preview) > 0 && q.PreviewBits > 0
	if hasPreview {
		flags |= wireFlagHasPreview
	}
	buf = append(buf, flags)

	for c := 0; c < q.Components; c++ {
		buf = appendFloat32(buf, q.Mins[c])
	}
	for c := 0; c < q.Components; c++ {
		buf = appendFloat32(buf, q.Maxs[c])
	}

	buf = enc.AppendULEB128(buf, uint64(len(q.Packed)))
	buf = append(buf, q.Packed...)
	if hasPreview {
		buf = append(buf, byte(q.PreviewBits))
		buf = enc.AppendULEB128(buf, uint64(len(q.Preview)))
		buf = append(buf, q.Preview...)
	}
	return buf, nil
}

// UnmarshalBinary decodes the compact binary wire form into q. It implements
// encoding.BinaryUnmarshaler. It rejects a truncated or inconsistent payload
// with an error and never panics.
func (q *Quantized) UnmarshalBinary(data []byte) error {
	const op = "field.UnmarshalBinary"
	r := &wireReader{buf: data}

	version, err := r.byteVal()
	if err != nil {
		return fieldError(op, "%v", err)
	}
	if version != quantizedWireVersion {
		return fieldError(op, "unknown wire version %d", version)
	}

	var out Quantized
	for axis := 0; axis < 3; axis++ {
		value, err := r.uint()
		if err != nil {
			return fieldError(op, "%v", err)
		}
		if value == 0 || value > math.MaxInt32 {
			return fieldError(op, "resolution axis %d is %d", axis, value)
		}
		out.Resolution[axis] = int(value)
	}
	components, err := r.byteVal()
	if err != nil {
		return fieldError(op, "%v", err)
	}
	if components < 1 || components > maxWireComponents {
		return fieldError(op, "components must be 1..%d, got %d", maxWireComponents, components)
	}
	out.Components = int(components)

	for axis := 0; axis < 3; axis++ {
		if out.Bounds.Min[axis], err = r.float32(); err != nil {
			return fieldError(op, "%v", err)
		}
	}
	for axis := 0; axis < 3; axis++ {
		if out.Bounds.Max[axis], err = r.float32(); err != nil {
			return fieldError(op, "%v", err)
		}
	}

	bitWidth, err := r.byteVal()
	if err != nil {
		return fieldError(op, "%v", err)
	}
	if bitWidth < 4 || bitWidth > 8 {
		return fieldError(op, "BitWidth must be 4..8, got %d", bitWidth)
	}
	out.BitWidth = int(bitWidth)

	flags, err := r.byteVal()
	if err != nil {
		return fieldError(op, "%v", err)
	}
	out.IsDelta = flags&wireFlagIsDelta != 0

	out.Mins = make([]float32, out.Components)
	out.Maxs = make([]float32, out.Components)
	for c := 0; c < out.Components; c++ {
		if out.Mins[c], err = r.float32(); err != nil {
			return fieldError(op, "%v", err)
		}
	}
	for c := 0; c < out.Components; c++ {
		if out.Maxs[c], err = r.float32(); err != nil {
			return fieldError(op, "%v", err)
		}
	}

	packed, err := r.blob()
	if err != nil {
		return fieldError(op, "%v", err)
	}
	out.Packed = append([]byte(nil), packed...)

	if flags&wireFlagHasPreview != 0 {
		previewBits, err := r.byteVal()
		if err != nil {
			return fieldError(op, "%v", err)
		}
		if previewBits < 1 || previewBits >= bitWidth {
			return fieldError(op, "PreviewBits must be 1..%d, got %d", bitWidth-1, previewBits)
		}
		out.PreviewBits = int(previewBits)
		preview, err := r.blob()
		if err != nil {
			return fieldError(op, "%v", err)
		}
		out.Preview = append([]byte(nil), preview...)
	}

	*q = out
	return nil
}

// DecodeQuantized decodes a compact binary payload into a new Quantized.
func DecodeQuantized(data []byte) (*Quantized, error) {
	q := &Quantized{}
	if err := q.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	return q, nil
}

// BinarySize returns the exact number of bytes MarshalBinary produces. Use it
// to budget a socket without encoding the payload twice.
func (q *Quantized) BinarySize() int {
	if q == nil {
		return 0
	}
	size := 1 // version
	size += ulebLen(uint64(q.Resolution[0]))
	size += ulebLen(uint64(q.Resolution[1]))
	size += ulebLen(uint64(q.Resolution[2]))
	size++     // components
	size += 24 // bounds
	size++     // bit width
	size++     // flags
	size += 8 * q.Components
	size += ulebLen(uint64(len(q.Packed))) + len(q.Packed)
	if len(q.Preview) > 0 && q.PreviewBits > 0 {
		size++ // preview bits
		size += ulebLen(uint64(len(q.Preview))) + len(q.Preview)
	}
	return size
}

func ulebLen(value uint64) int {
	n := 1
	for value >= 0x80 {
		value >>= 7
		n++
	}
	return n
}

func appendFloat32(dst []byte, value float32) []byte {
	var scratch [4]byte
	binary.LittleEndian.PutUint32(scratch[:], math.Float32bits(value))
	return append(dst, scratch[:]...)
}

// wireReader walks a binary payload and reports truncation instead of panicking.
type wireReader struct {
	buf []byte
	pos int
}

func (r *wireReader) byteVal() (byte, error) {
	if r.pos >= len(r.buf) {
		return 0, fmt.Errorf("payload truncated")
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

func (r *wireReader) uint() (uint64, error) {
	value, n, err := enc.ReadULEB128(r.buf[r.pos:])
	if err != nil {
		return 0, err
	}
	r.pos += n
	return value, nil
}

func (r *wireReader) raw(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.buf) {
		return nil, fmt.Errorf("payload truncated")
	}
	out := r.buf[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func (r *wireReader) float32() (float32, error) {
	raw, err := r.raw(4)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(raw)), nil
}

func (r *wireReader) blob() ([]byte, error) {
	length, err := r.uint()
	if err != nil {
		return nil, err
	}
	if length > uint64(len(r.buf)-r.pos) {
		return nil, fmt.Errorf("payload length %d exceeds body", length)
	}
	return r.raw(int(length))
}
