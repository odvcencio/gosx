// Package gltfedit reads a glTF 2.0 or GLB document, rewrites mesh
// primitives, and writes a new GLB. It exists so the asset pipeline can
// produce derived models, such as level-of-detail meshes, without a native
// glTF library.
package gltfedit

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Errors the reader and writer return.
var (
	ErrFormat      = errors.New("gltfedit: unrecognized document")
	ErrUnsupported = errors.New("gltfedit: unsupported feature")
)

// Component types from the glTF specification.
const (
	ComponentByte          = 5120
	ComponentUnsignedByte  = 5121
	ComponentShort         = 5122
	ComponentUnsignedShort = 5123
	ComponentUnsignedInt   = 5125
	ComponentFloat         = 5126
)

// Accessor describes one typed view over a buffer view.
type Accessor struct {
	BufferView    *int            `json:"bufferView,omitempty"`
	ByteOffset    int             `json:"byteOffset,omitempty"`
	ComponentType int             `json:"componentType"`
	Normalized    bool            `json:"normalized,omitempty"`
	Count         int             `json:"count"`
	Type          string          `json:"type"`
	Max           []float64       `json:"max,omitempty"`
	Min           []float64       `json:"min,omitempty"`
	Sparse        json.RawMessage `json:"sparse,omitempty"`
	Name          string          `json:"name,omitempty"`
	Extensions    json.RawMessage `json:"extensions,omitempty"`
	Extras        json.RawMessage `json:"extras,omitempty"`
}

// BufferView describes one slice of a buffer.
type BufferView struct {
	Buffer     int             `json:"buffer"`
	ByteOffset int             `json:"byteOffset,omitempty"`
	ByteLength int             `json:"byteLength"`
	ByteStride int             `json:"byteStride,omitempty"`
	Target     int             `json:"target,omitempty"`
	Name       string          `json:"name,omitempty"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
	Extras     json.RawMessage `json:"extras,omitempty"`
}

// Buffer names one binary payload.
type Buffer struct {
	URI        string          `json:"uri,omitempty"`
	ByteLength int             `json:"byteLength"`
	Name       string          `json:"name,omitempty"`
	Extras     json.RawMessage `json:"extras,omitempty"`
}

// Primitive is one drawable inside a mesh.
type Primitive struct {
	Attributes map[string]int   `json:"attributes"`
	Indices    *int             `json:"indices,omitempty"`
	Material   *int             `json:"material,omitempty"`
	Mode       *int             `json:"mode,omitempty"`
	Targets    []map[string]int `json:"targets,omitempty"`
	Extensions json.RawMessage  `json:"extensions,omitempty"`
	Extras     json.RawMessage  `json:"extras,omitempty"`
}

// Mesh groups primitives.
type Mesh struct {
	Primitives []Primitive     `json:"primitives"`
	Weights    []float64       `json:"weights,omitempty"`
	Name       string          `json:"name,omitempty"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
	Extras     json.RawMessage `json:"extras,omitempty"`
}

// Document is a parsed glTF document with resolved binary buffers. Fields the
// package does not model stay in root and survive a write unchanged.
type Document struct {
	root        map[string]json.RawMessage
	Accessors   []Accessor
	BufferViews []BufferView
	Buffers     []Buffer
	Meshes      []Mesh
	Nodes       []Node
	Scenes      []Scene
	Skins       []Skin
	Animations  []Animation

	// binary holds the resolved bytes of every buffer.
	binary [][]byte
	// staging is the index of the buffer that receives new bytes, or -1
	// before the writer creates it.
	staging int
	// ExtensionsUsed lists the extensions the document declares.
	ExtensionsUsed []string
	// ExtensionsRequired lists the extensions a loader must understand.
	ExtensionsRequired []string

	// The dirty flags tell the writer which sections changed. A section that
	// nothing changed keeps its original bytes, so a rewrite of one mesh does
	// not reformat the whole document.
	nodesDirty      bool
	scenesDirty     bool
	skinsDirty      bool
	animationsDirty bool
	extensionsDirty bool
}

// MaxDocumentBytes bounds one document read, including external buffers.
const MaxDocumentBytes = 512 << 20

// Parse reads a GLB or a glTF JSON document. resolve supplies external buffer
// files by relative URI, and may be nil when the document embeds its data.
func Parse(data []byte, resolve func(uri string) ([]byte, error)) (*Document, error) {
	if int64(len(data)) > MaxDocumentBytes {
		return nil, fmt.Errorf("%w: document is %d bytes", ErrUnsupported, len(data))
	}
	jsonChunk := data
	var binChunk []byte
	if len(data) >= 12 && string(data[:4]) == "glTF" {
		parsedJSON, parsedBin, err := splitGLB(data)
		if err != nil {
			return nil, err
		}
		jsonChunk, binChunk = parsedJSON, parsedBin
	}

	doc := &Document{staging: -1}
	if err := json.Unmarshal(jsonChunk, &doc.root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	if err := doc.decodeSection("accessors", &doc.Accessors); err != nil {
		return nil, err
	}
	if err := doc.decodeSection("bufferViews", &doc.BufferViews); err != nil {
		return nil, err
	}
	if err := doc.decodeSection("buffers", &doc.Buffers); err != nil {
		return nil, err
	}
	if err := doc.decodeSection("meshes", &doc.Meshes); err != nil {
		return nil, err
	}
	if err := doc.decodeSection("nodes", &doc.Nodes); err != nil {
		return nil, err
	}
	if err := doc.decodeSection("scenes", &doc.Scenes); err != nil {
		return nil, err
	}
	if err := doc.decodeSection("skins", &doc.Skins); err != nil {
		return nil, err
	}
	if err := doc.decodeSection("animations", &doc.Animations); err != nil {
		return nil, err
	}
	if err := doc.decodeSection("extensionsUsed", &doc.ExtensionsUsed); err != nil {
		return nil, err
	}
	if err := doc.decodeSection("extensionsRequired", &doc.ExtensionsRequired); err != nil {
		return nil, err
	}

	doc.binary = make([][]byte, len(doc.Buffers))
	total := len(data)
	for i, buffer := range doc.Buffers {
		switch {
		case buffer.URI == "":
			if binChunk == nil {
				return nil, fmt.Errorf("%w: buffer %d has no URI and the file has no binary chunk", ErrFormat, i)
			}
			doc.binary[i] = binChunk
		case strings.HasPrefix(buffer.URI, "data:"):
			payload, err := decodeDataURI(buffer.URI)
			if err != nil {
				return nil, err
			}
			doc.binary[i] = payload
			total += len(payload)
		default:
			if resolve == nil {
				return nil, fmt.Errorf("%w: buffer %d needs external file %q", ErrUnsupported, i, buffer.URI)
			}
			payload, err := resolve(buffer.URI)
			if err != nil {
				return nil, fmt.Errorf("gltfedit: resolve buffer %q: %w", buffer.URI, err)
			}
			doc.binary[i] = payload
			total += len(payload)
		}
		if int64(total) > MaxDocumentBytes {
			return nil, fmt.Errorf("%w: document with buffers exceeds %d bytes", ErrUnsupported, MaxDocumentBytes)
		}
	}
	return doc, nil
}

func (d *Document) decodeSection(key string, target any) error {
	raw, ok := d.root[key]
	if !ok {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrFormat, key, err)
	}
	return nil
}

func splitGLB(data []byte) ([]byte, []byte, error) {
	if len(data) < 20 {
		return nil, nil, fmt.Errorf("%w: glb header is short", ErrFormat)
	}
	if version := binary.LittleEndian.Uint32(data[4:]); version != 2 {
		return nil, nil, fmt.Errorf("%w: glb version %d", ErrUnsupported, version)
	}
	total := int(binary.LittleEndian.Uint32(data[8:]))
	if total > len(data) {
		return nil, nil, fmt.Errorf("%w: glb declares %d bytes, file holds %d", ErrFormat, total, len(data))
	}
	var jsonChunk, binChunk []byte
	offset := 12
	for offset+8 <= total {
		length := int(binary.LittleEndian.Uint32(data[offset:]))
		kind := binary.LittleEndian.Uint32(data[offset+4:])
		start := offset + 8
		if start+length > total {
			return nil, nil, fmt.Errorf("%w: glb chunk runs past the file", ErrFormat)
		}
		switch kind {
		case 0x4E4F534A: // JSON
			jsonChunk = data[start : start+length]
		case 0x004E4942: // BIN
			binChunk = data[start : start+length]
		}
		offset = start + length
		if pad := offset % 4; pad != 0 {
			offset += 4 - pad
		}
	}
	if jsonChunk == nil {
		return nil, nil, fmt.Errorf("%w: glb has no JSON chunk", ErrFormat)
	}
	return jsonChunk, binChunk, nil
}

func decodeDataURI(uri string) ([]byte, error) {
	_, payload, ok := strings.Cut(uri, ",")
	if !ok {
		return nil, fmt.Errorf("%w: malformed data URI", ErrFormat)
	}
	if !strings.Contains(uri[:len(uri)-len(payload)], ";base64") {
		return nil, fmt.Errorf("%w: only base64 data URIs are supported", ErrUnsupported)
	}
	return base64.StdEncoding.DecodeString(payload)
}

// componentSize returns the byte size of one component.
func componentSize(componentType int) (int, error) {
	switch componentType {
	case ComponentByte, ComponentUnsignedByte:
		return 1, nil
	case ComponentShort, ComponentUnsignedShort:
		return 2, nil
	case ComponentUnsignedInt, ComponentFloat:
		return 4, nil
	}
	return 0, fmt.Errorf("%w: componentType %d", ErrUnsupported, componentType)
}

// TypeComponents returns the component count of a glTF accessor type.
func TypeComponents(accessorType string) (int, error) {
	switch accessorType {
	case "SCALAR":
		return 1, nil
	case "VEC2":
		return 2, nil
	case "VEC3":
		return 3, nil
	case "VEC4":
		return 4, nil
	case "MAT2":
		return 4, nil
	case "MAT3":
		return 9, nil
	case "MAT4":
		return 16, nil
	}
	return 0, fmt.Errorf("%w: accessor type %q", ErrUnsupported, accessorType)
}

// ReadAccessor returns the accessor values as float64, one slice entry per
// component. Normalized integer accessors decode into their real range.
func (d *Document) ReadAccessor(index int) ([]float64, int, error) {
	if index < 0 || index >= len(d.Accessors) {
		return nil, 0, fmt.Errorf("%w: accessor %d", ErrFormat, index)
	}
	accessor := d.Accessors[index]
	if len(accessor.Sparse) > 0 {
		return nil, 0, fmt.Errorf("%w: sparse accessor %d", ErrUnsupported, index)
	}
	components, err := TypeComponents(accessor.Type)
	if err != nil {
		return nil, 0, err
	}
	size, err := componentSize(accessor.ComponentType)
	if err != nil {
		return nil, 0, err
	}
	out := make([]float64, accessor.Count*components)
	if accessor.BufferView == nil {
		return out, components, nil // A missing buffer view means zeros.
	}
	view, data, err := d.viewBytes(*accessor.BufferView)
	if err != nil {
		return nil, 0, err
	}
	stride := view.ByteStride
	if stride == 0 {
		stride = components * size
	}
	for i := 0; i < accessor.Count; i++ {
		base := accessor.ByteOffset + i*stride
		if base+components*size > len(data) {
			return nil, 0, fmt.Errorf("%w: accessor %d runs past its buffer view", ErrFormat, index)
		}
		for c := 0; c < components; c++ {
			out[i*components+c] = readComponent(data[base+c*size:], accessor.ComponentType, accessor.Normalized)
		}
	}
	return out, components, nil
}

// ReadIndices returns a triangle index list as uint32.
func (d *Document) ReadIndices(index int) ([]uint32, error) {
	values, components, err := d.ReadAccessor(index)
	if err != nil {
		return nil, err
	}
	if components != 1 {
		return nil, fmt.Errorf("%w: index accessor %d is not SCALAR", ErrFormat, index)
	}
	out := make([]uint32, len(values))
	for i, value := range values {
		out[i] = uint32(value)
	}
	return out, nil
}

func (d *Document) viewBytes(index int) (BufferView, []byte, error) {
	if index < 0 || index >= len(d.BufferViews) {
		return BufferView{}, nil, fmt.Errorf("%w: bufferView %d", ErrFormat, index)
	}
	view := d.BufferViews[index]
	if view.Buffer < 0 || view.Buffer >= len(d.binary) {
		return BufferView{}, nil, fmt.Errorf("%w: buffer %d", ErrFormat, view.Buffer)
	}
	data := d.binary[view.Buffer]
	if view.ByteOffset+view.ByteLength > len(data) {
		return BufferView{}, nil, fmt.Errorf("%w: bufferView %d runs past its buffer", ErrFormat, index)
	}
	return view, data[view.ByteOffset : view.ByteOffset+view.ByteLength], nil
}

func readComponent(data []byte, componentType int, normalized bool) float64 {
	switch componentType {
	case ComponentFloat:
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(data)))
	case ComponentUnsignedInt:
		return float64(binary.LittleEndian.Uint32(data))
	case ComponentUnsignedShort:
		value := float64(binary.LittleEndian.Uint16(data))
		if normalized {
			return value / 65535
		}
		return value
	case ComponentShort:
		value := float64(int16(binary.LittleEndian.Uint16(data)))
		if normalized {
			return math.Max(value/32767, -1)
		}
		return value
	case ComponentUnsignedByte:
		value := float64(data[0])
		if normalized {
			return value / 255
		}
		return value
	case ComponentByte:
		value := float64(int8(data[0]))
		if normalized {
			return math.Max(value/127, -1)
		}
		return value
	}
	return 0
}

func writeComponent(dst []byte, value float64, componentType int, normalized bool) {
	switch componentType {
	case ComponentFloat:
		binary.LittleEndian.PutUint32(dst, math.Float32bits(float32(value)))
	case ComponentUnsignedInt:
		binary.LittleEndian.PutUint32(dst, uint32(clampRange(value, 0, math.MaxUint32)))
	case ComponentUnsignedShort:
		if normalized {
			value = math.Round(clampRange(value, 0, 1) * 65535)
		}
		binary.LittleEndian.PutUint16(dst, uint16(clampRange(value, 0, 65535)))
	case ComponentShort:
		if normalized {
			value = math.Round(clampRange(value, -1, 1) * 32767)
		}
		binary.LittleEndian.PutUint16(dst, uint16(int16(clampRange(value, -32768, 32767))))
	case ComponentUnsignedByte:
		if normalized {
			value = math.Round(clampRange(value, 0, 1) * 255)
		}
		dst[0] = byte(clampRange(value, 0, 255))
	case ComponentByte:
		if normalized {
			value = math.Round(clampRange(value, -1, 1) * 127)
		}
		dst[0] = byte(int8(clampRange(value, -128, 127)))
	}
}

func clampRange(value, low, high float64) float64 {
	if math.IsNaN(value) {
		return 0
	}
	return math.Max(low, math.Min(high, value))
}
