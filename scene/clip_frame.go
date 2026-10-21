package scene

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// MaxClipLayers is the most clip layers one model can blend in a ClipFrame.
const MaxClipLayers = 4

// ClipFrame poses skinned models from caller-supplied clip times. It is the
// opt-in "sampled" animation mode: a Model declared with ClipSampled has no
// client clock. Each ClipFrame states, for every model it names, up to
// MaxClipLayers clip layers, and the client samples exactly those clips at
// exactly those times and blends them. The same layers always produce the
// same pose, including after a jump backwards (a rollback) or forwards.
//
// A ClipFrame is complete state for the models it names; a model it does not
// name keeps its last pose. IDs must match mounted Models declared with
// ClipSampled. Send EncodeClipFrame's bytes (or call DispatchClipFrame in a
// browser build) to window.__gosx.scene3d.dispatchClipFrame(target, bytes).
//
// Time beyond the clip end follows ClipLayer.Loop: a looping layer wraps
// (time modulo clip duration), and a non-looping layer holds the last key.
// Negative time cannot be encoded.
//
// Root motion: by default the horizontal (X and Z) translation of a rig's
// root nodes is not applied, because the simulation owns position. Set
// Model.RootMotion to apply it. See Model.RootMotion for the exact rule.
type ClipFrame struct {
	Models []ClipModel
}

// ClipModel holds the layers for one sampled model, identified by Model.ID.
// An empty Layers slice poses the model in its authored bind pose.
type ClipModel struct {
	ID     string
	Layers []ClipLayer
}

// ClipLayer is one clip sampled at an exact time.
//
// Name selects the clip by its glTF animation name. When Name is empty, Index
// selects the glTF animation by position (zero-based). Time is the clip time
// in seconds. Weight is a relative blend weight: weights need not sum to one,
// and a layer with Weight 0 contributes nothing. Layers blend in slice order,
// as the existing mixer does. Loop makes time past the clip end wrap; the
// zero value clamps to the last key.
type ClipLayer struct {
	Name   string
	Index  int
	Time   float64
	Weight float64
	Loop   bool
}

// ClipFrameEncoder reuses its buffers and validation tables across frames.
// It is not safe for concurrent use. The returned bytes remain valid only
// until the next Encode call; dispatch copies them into a JS-owned array first.
type ClipFrameEncoder struct {
	out        []byte
	names      []string
	seenModels map[string]struct{}
}

// EncodeClipFrame writes the GSP4 little-endian wire format:
//
//	"GSP4"
//	u16 name count, then each clip name as u16 length + UTF-8 bytes
//	u16 model count, then for each model:
//	    u16 length + UTF-8 model ID
//	    u8  layer count (0 to 4)
//	    per layer: u16 clip ref, f32 time, f32 weight, u8 flags
//
// A clip ref with bit 15 clear is a zero-based position in the name list. A
// clip ref with bit 15 set carries a zero-based glTF animation index in its
// low 15 bits. Flag bit 0 is Loop. A layer costs 11 bytes.
func EncodeClipFrame(frame ClipFrame) ([]byte, error) {
	return new(ClipFrameEncoder).Encode(frame)
}

// Encode writes one clip frame, reusing storage held by the encoder.
func (e *ClipFrameEncoder) Encode(frame ClipFrame) ([]byte, error) {
	if len(frame.Models) > math.MaxUint16 {
		return nil, errors.New("scene clip frame has too many models")
	}
	e.names = e.names[:0]
	if e.seenModels == nil {
		e.seenModels = make(map[string]struct{}, len(frame.Models))
	} else {
		clear(e.seenModels)
	}
	// Pass 1: validate and collect the name dictionary. The dictionary is a
	// short slice (a handful of clips per frame), so a linear scan beats a map
	// and allocates nothing in steady state.
	size := 8
	for _, model := range frame.Models {
		if err := checkClipID(model.ID); err != nil {
			return nil, err
		}
		if _, dup := e.seenModels[model.ID]; dup {
			return nil, errors.New("scene clip frame has duplicate model ID")
		}
		e.seenModels[model.ID] = struct{}{}
		if len(model.Layers) > MaxClipLayers {
			return nil, errors.New("scene clip frame has too many layers for one model")
		}
		size += 3 + len(model.ID) + len(model.Layers)*11
		for _, layer := range model.Layers {
			if err := checkClipLayer(layer); err != nil {
				return nil, err
			}
			if layer.Name != "" && e.nameIndex(layer.Name) < 0 {
				if len(e.names) >= 0x8000 {
					return nil, errors.New("scene clip frame has too many clip names")
				}
				if err := checkClipID(layer.Name); err != nil {
					return nil, err
				}
				e.names = append(e.names, layer.Name)
				size += 2 + len(layer.Name)
			}
		}
	}
	if cap(e.out) < size {
		e.out = make([]byte, 0, size)
	}
	out := append(e.out[:0], "GSP4"...)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(e.names)))
	for _, name := range e.names {
		out = binary.LittleEndian.AppendUint16(out, uint16(len(name)))
		out = append(out, name...)
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(len(frame.Models)))
	for _, model := range frame.Models {
		out = binary.LittleEndian.AppendUint16(out, uint16(len(model.ID)))
		out = append(out, model.ID...)
		out = append(out, byte(len(model.Layers)))
		for _, layer := range model.Layers {
			ref := uint16(0x8000 | layer.Index)
			if layer.Name != "" {
				ref = uint16(e.nameIndex(layer.Name))
			}
			out = binary.LittleEndian.AppendUint16(out, ref)
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(layer.Time)))
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(layer.Weight)))
			if layer.Loop {
				out = append(out, 1)
			} else {
				out = append(out, 0)
			}
		}
	}
	e.out = out
	return out, nil
}

func (e *ClipFrameEncoder) nameIndex(name string) int {
	for i, candidate := range e.names {
		if candidate == name {
			return i
		}
	}
	return -1
}

func checkClipID(id string) error {
	if id == "" || len(id) > math.MaxUint16 || !utf8.ValidString(id) {
		return errors.New("scene clip frame requires a bounded nonempty ID")
	}
	return nil
}

func checkClipLayer(layer ClipLayer) error {
	if layer.Name == "" && (layer.Index < 0 || layer.Index >= 0x8000) {
		return errors.New("scene clip frame layer index must be in [0, 32768)")
	}
	time, weight := float32(layer.Time), float32(layer.Weight)
	if math.IsNaN(layer.Time) || math.IsInf(layer.Time, 0) || math.IsInf(float64(time), 0) || time < 0 {
		return errors.New("scene clip frame requires finite nonnegative layer time")
	}
	if math.IsNaN(layer.Weight) || math.IsInf(layer.Weight, 0) || math.IsInf(float64(weight), 0) || weight < 0 {
		return errors.New("scene clip frame requires finite nonnegative layer weight")
	}
	return nil
}
