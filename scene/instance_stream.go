package scene

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// InstanceStreamEvent is the name a mounted Scene3D surface's lazy-loaded
// instance-stream bridge dispatches a decoded frame through. It is a
// SEPARATE, opt-in channel from MountCommandsEvent: MountCommandBatch still
// carries every scene mutation a page makes (geometry, material, count,
// membership); InstanceStreamFrame carries ONLY the per-instance transform
// (and, for a skinned batch, clip/time) data for a batch already registered
// through a normal CommandSetInstancedMeshes or CommandSetInstancedGLBMeshes
// command. See client/runtime/scene3d/instance-stream.ts.
const InstanceStreamEvent = "gosx:scene3d:instance-stream"

// InstanceStreamKind selects the per-instance record layout an
// InstanceStreamFrame carries. The numeric values are wire-stable: they are
// read directly off the byte stream by the browser runtime.
type InstanceStreamKind uint8

const (
	// InstanceStreamTransform carries one column-major 4x4 matrix per
	// instance (16 float32s), matching the flattened layout
	// IRInstancedMesh.Transforms already uses on the JSON path. This is the
	// kind a rigid InstancedMesh batch (Scene3D's box/sphere/capsule/etc.
	// primitives) streams every frame.
	InstanceStreamTransform InstanceStreamKind = iota
	// InstanceStreamTransformColor carries a 4x4 matrix plus an RGBA color
	// (20 float32s) per instance, for a batch whose per-instance color also
	// changes every frame.
	InstanceStreamTransformColor
	// InstanceStreamSkinnedPose carries a 4x4 matrix plus a clip index and a
	// clip time (18 float32s) per instance, for an InstancedGLBMesh batch
	// whose instances play independent animation clips. The wire layout is
	// defined for completeness, but there is intentionally no JS apply
	// target: PoseFrame (pose_frame.go) already owns skinned-pose streaming
	// for a retained InstancedGLBMesh batch, including clip/time/loop state
	// and the retained model-instance pose tracker's membership bookkeeping.
	// A caller streaming skinned poses uses PoseFrame, not this kind.
	InstanceStreamSkinnedPose
)

// String names the kind for diagnostics.
func (k InstanceStreamKind) String() string {
	switch k {
	case InstanceStreamTransform:
		return "transform"
	case InstanceStreamTransformColor:
		return "transform-color"
	case InstanceStreamSkinnedPose:
		return "skinned-pose"
	default:
		return fmt.Sprintf("instance-stream-kind(%d)", uint8(k))
	}
}

// Stride reports the float32 count of one instance's record for the kind, or
// 0 for an unrecognized kind.
func (k InstanceStreamKind) Stride() int {
	switch k {
	case InstanceStreamTransform:
		return 16
	case InstanceStreamTransformColor:
		return 20
	case InstanceStreamSkinnedPose:
		return 18
	default:
		return 0
	}
}

const (
	instanceStreamMagic       = "GSXI"
	instanceStreamVersion     = 1
	instanceStreamHeaderBytes = 24 // magic(4) + version(1) + kind(1) + reserved(2) + revision(8) + count(4) + idLen(2) + reserved(2)
)

// InstanceStreamFrame is one binary per-frame update for a single already
// registered instanced batch (InstancedMesh or InstancedGLBMesh), addressed
// by the batch's existing scene-node ID. It exists to give a Go/WASM game
// loop a typed, allocation-light alternative to re-marshaling a whole
// MountCommandBatch (JSON) every frame when only instance poses moved: see
// spec.m31labs-gosx.scene-binary-transport.
//
// Data is stride*Count float32s, one instance's record after another, laid
// out exactly as Kind.Stride documents. A transform is always the first 16
// floats of a record: a column-major 4x4 matrix, matching
// IRInstancedMesh.Transforms and mat4FromTRS's existing layout, so a browser
// runtime that already reads that layout on the JSON path reads this layout
// unchanged.
type InstanceStreamFrame struct {
	// BatchID is the InstancedMeshIR.ID / InstancedGLBMeshIR.ID the frame
	// updates. The batch must already be mounted (via a normal
	// CommandSetInstancedMeshes / CommandSetInstancedGLBMeshes command) with
	// this ID and Count; InstanceStreamFrame carries no geometry, material,
	// or membership data and cannot create a batch.
	BatchID string
	// Revision must be positive and strictly increase per BatchID, mirroring
	// MountCommandBatch.Revision. The browser runtime drops a stale or
	// replayed frame.
	Revision uint64
	Kind     InstanceStreamKind
	// Count is the instance count this frame carries. It must equal
	// len(Data) / Kind.Stride(), and the browser runtime rejects (fails
	// named, does not truncate or pad) a frame whose Count disagrees with
	// the batch's last-known instance count.
	Count int
	// Data is Count*Kind.Stride() float32s. See the type doc for layout.
	Data []float32
}

// Encode validates f and serializes it to the wire format
// client/runtime/scene3d/instance-stream.ts decodes:
//
//	offset  size  field
//	0       4     magic "GSXI"
//	4       1     version (1)
//	5       1     kind
//	6       2     reserved (0)
//	8       8     revision, uint64 LE
//	16      4     count, uint32 LE
//	20      2     idLen, uint16 LE (BatchID byte length, unpadded)
//	22      2     reserved (0)
//	24      idLen BatchID bytes (UTF-8)
//	...     pad   zero bytes up to the next 4-byte boundary
//	pad     N     Count*Kind.Stride() float32s, little-endian
//
// The id is padded so the float32 payload always starts 4-byte aligned,
// which lets the browser runtime view it as a Float32Array with zero copy.
func (f InstanceStreamFrame) Encode() ([]byte, error) {
	if f.Revision == 0 {
		return nil, errors.New("scene: instance stream revision must be positive")
	}
	if f.BatchID == "" {
		return nil, errors.New("scene: instance stream batch id must not be empty")
	}
	if len(f.BatchID) > 0xFFFF {
		return nil, fmt.Errorf("scene: instance stream batch id too long: %d bytes", len(f.BatchID))
	}
	stride := f.Kind.Stride()
	if stride == 0 {
		return nil, fmt.Errorf("scene: unknown instance stream kind %d", uint8(f.Kind))
	}
	if f.Count < 0 {
		return nil, fmt.Errorf("scene: instance stream count must not be negative: %d", f.Count)
	}
	if len(f.Data) != f.Count*stride {
		return nil, fmt.Errorf("scene: instance stream data length %d does not match count %d * stride %d", len(f.Data), f.Count, stride)
	}

	idLen := len(f.BatchID)
	idPadded := align4(idLen)
	payloadBytes := len(f.Data) * 4
	total := instanceStreamHeaderBytes + idPadded + payloadBytes

	buf := make([]byte, total)
	copy(buf[0:4], instanceStreamMagic)
	buf[4] = instanceStreamVersion
	buf[5] = byte(f.Kind)
	// buf[6:8] reserved, already zero.
	binary.LittleEndian.PutUint64(buf[8:16], f.Revision)
	binary.LittleEndian.PutUint32(buf[16:20], uint32(f.Count))
	binary.LittleEndian.PutUint16(buf[20:22], uint16(idLen))
	// buf[22:24] reserved, already zero.
	copy(buf[24:24+idLen], f.BatchID)
	// buf[24+idLen : instanceStreamHeaderBytes+idPadded] is the zero pad.

	payloadOffset := instanceStreamHeaderBytes + idPadded
	for i, v := range f.Data {
		binary.LittleEndian.PutUint32(buf[payloadOffset+i*4:payloadOffset+i*4+4], math.Float32bits(v))
	}
	return buf, nil
}

// DecodeInstanceStreamFrame parses the wire format Encode writes. It exists
// for round-trip testing and for a native (non-browser) consumer of the same
// wire format, such as a headless render/bundle test oracle; the browser
// runtime has its own JS decoder in instance-stream.ts.
func DecodeInstanceStreamFrame(b []byte) (InstanceStreamFrame, error) {
	if len(b) < instanceStreamHeaderBytes {
		return InstanceStreamFrame{}, fmt.Errorf("scene: instance stream frame too short: %d bytes", len(b))
	}
	if string(b[0:4]) != instanceStreamMagic {
		return InstanceStreamFrame{}, fmt.Errorf("scene: instance stream frame has bad magic %q", b[0:4])
	}
	if b[4] != instanceStreamVersion {
		return InstanceStreamFrame{}, fmt.Errorf("scene: instance stream frame has unsupported version %d", b[4])
	}
	kind := InstanceStreamKind(b[5])
	stride := kind.Stride()
	if stride == 0 {
		return InstanceStreamFrame{}, fmt.Errorf("scene: instance stream frame has unknown kind %d", b[5])
	}
	revision := binary.LittleEndian.Uint64(b[8:16])
	count := binary.LittleEndian.Uint32(b[16:20])
	idLen := int(binary.LittleEndian.Uint16(b[20:22]))
	idEnd := instanceStreamHeaderBytes + idLen
	if idEnd > len(b) {
		return InstanceStreamFrame{}, fmt.Errorf("scene: instance stream frame id length %d overruns %d-byte frame", idLen, len(b))
	}
	id := string(b[instanceStreamHeaderBytes:idEnd])
	payloadOffset := instanceStreamHeaderBytes + align4(idLen)
	wantFloats := int(count) * stride
	wantBytes := payloadOffset + wantFloats*4
	if wantBytes != len(b) {
		return InstanceStreamFrame{}, fmt.Errorf("scene: instance stream frame length %d does not match header (want %d)", len(b), wantBytes)
	}
	data := make([]float32, wantFloats)
	for i := range data {
		off := payloadOffset + i*4
		data[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[off : off+4]))
	}
	return InstanceStreamFrame{
		BatchID:  id,
		Revision: revision,
		Kind:     kind,
		Count:    int(count),
		Data:     data,
	}, nil
}

func align4(n int) int {
	if rem := n % 4; rem != 0 {
		return n + (4 - rem)
	}
	return n
}
