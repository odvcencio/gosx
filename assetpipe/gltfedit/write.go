package gltfedit

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Buffer view targets from the glTF specification.
const (
	TargetArrayBuffer        = 34962
	TargetElementArrayBuffer = 34963
)

// image models the fields the specification defines for an image. The writer
// needs the bufferView reference so garbage collection keeps it alive.
type image struct {
	URI        string          `json:"uri,omitempty"`
	MimeType   string          `json:"mimeType,omitempty"`
	BufferView *int            `json:"bufferView,omitempty"`
	Name       string          `json:"name,omitempty"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
	Extras     json.RawMessage `json:"extras,omitempty"`
}

// accessorSafeExtensions never reference a buffer view of their own, so the
// writer can still drop unreachable views when a document declares them.
var accessorSafeExtensions = map[string]bool{
	"KHR_materials_clearcoat":         true,
	"KHR_materials_sheen":             true,
	"KHR_materials_iridescence":       true,
	"KHR_materials_transmission":      true,
	"KHR_materials_anisotropy":        true,
	"KHR_materials_unlit":             true,
	"KHR_materials_emissive_strength": true,
	"KHR_materials_ior":               true,
	"KHR_materials_specular":          true,
	"KHR_materials_volume":            true,
	"KHR_texture_transform":           true,
	"KHR_lights_punctual":             true,
	"KHR_mesh_quantization":           true,
	"KHR_materials_variants":          true,
	"KHR_texture_basisu":              true,
	// EXT_mesh_gpu_instancing names accessors, never a buffer view of its own,
	// and the writer keeps the view of every accessor.
	"EXT_mesh_gpu_instancing": true,
}

// stagingBuffer returns the index of the buffer that receives new bytes.
func (d *Document) stagingBuffer() int {
	if d.staging >= 0 {
		return d.staging
	}
	d.Buffers = append(d.Buffers, Buffer{Name: "gosx-assetpipe"})
	d.binary = append(d.binary, nil)
	d.staging = len(d.Buffers) - 1
	return d.staging
}

// appendView copies data into the staging buffer and returns a new buffer
// view index.
func (d *Document) appendView(data []byte, target int) int {
	buffer := d.stagingBuffer()
	// Keep every view four-byte aligned so float accessors stay legal.
	for len(d.binary[buffer])%4 != 0 {
		d.binary[buffer] = append(d.binary[buffer], 0)
	}
	offset := len(d.binary[buffer])
	d.binary[buffer] = append(d.binary[buffer], data...)
	d.Buffers[buffer].ByteLength = len(d.binary[buffer])
	d.BufferViews = append(d.BufferViews, BufferView{
		Buffer:     buffer,
		ByteOffset: offset,
		ByteLength: len(data),
		Target:     target,
	})
	return len(d.BufferViews) - 1
}

// SetAccessorData replaces the payload of an accessor in place. The caller
// supplies values in component order. Use it when exactly one primitive reads
// the accessor, so no other reference changes meaning.
func (d *Document) SetAccessorData(index int, values []float64, accessorType string, componentType int, normalized bool, target int) error {
	if index < 0 || index >= len(d.Accessors) {
		return fmt.Errorf("%w: accessor %d", ErrFormat, index)
	}
	components, err := TypeComponents(accessorType)
	if err != nil {
		return err
	}
	if components == 0 || len(values)%components != 0 {
		return fmt.Errorf("%w: %d values do not fill %s elements", ErrFormat, len(values), accessorType)
	}
	size, err := componentSize(componentType)
	if err != nil {
		return err
	}
	payload := make([]byte, len(values)*size)
	for i, value := range values {
		writeComponent(payload[i*size:], value, componentType, normalized)
	}
	view := d.appendView(payload, target)
	count := len(values) / components

	accessor := d.Accessors[index]
	accessor.BufferView = &view
	accessor.ByteOffset = 0
	accessor.ComponentType = componentType
	accessor.Normalized = normalized
	accessor.Count = count
	accessor.Type = accessorType
	accessor.Sparse = nil
	accessor.Min, accessor.Max = bounds(values, components)
	d.Accessors[index] = accessor
	return nil
}

// SetAccessorInts replaces the payload of an accessor with integer component
// values. The caller supplies the exact stored numbers, so no normalization
// runs on the way in.
//
// The accessor keeps min and max in stored units, which is what the
// specification asks for and what the glTF validator checks.
func (d *Document) SetAccessorInts(index int, values []int32, accessorType string, componentType int, normalized bool, target int) error {
	if index < 0 || index >= len(d.Accessors) {
		return fmt.Errorf("%w: accessor %d", ErrFormat, index)
	}
	components, err := TypeComponents(accessorType)
	if err != nil {
		return err
	}
	if components == 0 || len(values)%components != 0 {
		return fmt.Errorf("%w: %d values do not fill %s elements", ErrFormat, len(values), accessorType)
	}
	size, err := componentSize(componentType)
	if err != nil {
		return err
	}
	payload := make([]byte, len(values)*size)
	for i, value := range values {
		// Pass normalized as false so the writer stores the integer unchanged.
		writeComponent(payload[i*size:], float64(value), componentType, false)
	}
	view := d.appendView(payload, target)

	stored := make([]float64, len(values))
	for i, value := range values {
		stored[i] = float64(value)
	}
	accessor := d.Accessors[index]
	accessor.BufferView = &view
	accessor.ByteOffset = 0
	accessor.ComponentType = componentType
	accessor.Normalized = normalized
	accessor.Count = len(values) / components
	accessor.Type = accessorType
	accessor.Sparse = nil
	accessor.Min, accessor.Max = bounds(stored, components)
	d.Accessors[index] = accessor
	return nil
}

// AddAccessorInts appends a new integer accessor and returns its index.
func (d *Document) AddAccessorInts(values []int32, accessorType string, componentType int, normalized bool, target int) (int, error) {
	d.Accessors = append(d.Accessors, Accessor{ComponentType: componentType, Type: accessorType})
	index := len(d.Accessors) - 1
	if err := d.SetAccessorInts(index, values, accessorType, componentType, normalized, target); err != nil {
		d.Accessors = d.Accessors[:index]
		return 0, err
	}
	return index, nil
}

// AddAccessor appends a new accessor with its own buffer view and returns the
// accessor index.
func (d *Document) AddAccessor(values []float64, accessorType string, componentType int, normalized bool, target int) (int, error) {
	d.Accessors = append(d.Accessors, Accessor{ComponentType: componentType, Type: accessorType})
	index := len(d.Accessors) - 1
	if err := d.SetAccessorData(index, values, accessorType, componentType, normalized, target); err != nil {
		d.Accessors = d.Accessors[:index]
		return 0, err
	}
	return index, nil
}

// IndexComponentType picks the smallest legal index component type for a
// vertex count.
func IndexComponentType(vertexCount int) int {
	switch {
	case vertexCount <= 0xFF:
		return ComponentUnsignedByte
	case vertexCount <= 0xFFFF:
		return ComponentUnsignedShort
	default:
		return ComponentUnsignedInt
	}
}

func bounds(values []float64, components int) ([]float64, []float64) {
	if len(values) == 0 || components <= 0 {
		return nil, nil
	}
	minimum := make([]float64, components)
	maximum := make([]float64, components)
	for c := 0; c < components; c++ {
		minimum[c] = math.Inf(1)
		maximum[c] = math.Inf(-1)
	}
	for i := 0; i < len(values); i += components {
		for c := 0; c < components; c++ {
			minimum[c] = math.Min(minimum[c], values[i+c])
			maximum[c] = math.Max(maximum[c], values[i+c])
		}
	}
	return minimum, maximum
}

// AccessorUseCount counts how many primitive slots read each accessor. The
// LOD writer uses it to decide between an in-place rewrite and a new
// accessor.
func (d *Document) AccessorUseCount() map[int]int {
	counts := map[int]int{}
	for _, mesh := range d.Meshes {
		for _, primitive := range mesh.Primitives {
			for _, accessor := range primitive.Attributes {
				counts[accessor]++
			}
			if primitive.Indices != nil {
				counts[*primitive.Indices]++
			}
			for _, target := range primitive.Targets {
				for _, accessor := range target {
					counts[accessor]++
				}
			}
		}
	}
	return counts
}

// WriteGLB serializes the document as a binary glTF file. The writer keeps
// only the buffer views the document still reaches, and it packs them into a
// single embedded buffer.
func (d *Document) WriteGLB() ([]byte, error) {
	var images []image
	if err := d.decodeSection("images", &images); err != nil {
		return nil, err
	}

	keep := make([]bool, len(d.BufferViews))
	gcSafe := d.bufferViewGCSafe()
	if !gcSafe {
		for i := range keep {
			keep[i] = true
		}
	} else {
		for _, accessor := range d.Accessors {
			if accessor.BufferView != nil && *accessor.BufferView >= 0 && *accessor.BufferView < len(keep) {
				keep[*accessor.BufferView] = true
			}
		}
		for _, img := range images {
			if img.BufferView != nil && *img.BufferView >= 0 && *img.BufferView < len(keep) {
				keep[*img.BufferView] = true
			}
		}
	}

	var payload []byte
	remap := make([]int, len(d.BufferViews))
	newViews := make([]BufferView, 0, len(d.BufferViews))
	// packed maps the content hash of a view payload to the offset it already
	// occupies. Two views that hold the same bytes then share those bytes, so a
	// duplicated attribute costs only its own view record.
	packed := map[string]int{}
	for index, view := range d.BufferViews {
		remap[index] = -1
		if !keep[index] {
			continue
		}
		if view.Buffer < 0 || view.Buffer >= len(d.binary) {
			return nil, fmt.Errorf("%w: bufferView %d names buffer %d", ErrFormat, index, view.Buffer)
		}
		source := d.binary[view.Buffer]
		if view.ByteOffset+view.ByteLength > len(source) {
			return nil, fmt.Errorf("%w: bufferView %d runs past its buffer", ErrFormat, index)
		}
		bytesOfView := source[view.ByteOffset : view.ByteOffset+view.ByteLength]
		digest := viewDigest(bytesOfView)
		offset, reused := packed[digest]
		if !reused {
			for len(payload)%4 != 0 {
				payload = append(payload, 0)
			}
			offset = len(payload)
			payload = append(payload, bytesOfView...)
			packed[digest] = offset
		}
		copied := view
		copied.Buffer = 0
		copied.ByteOffset = offset
		remap[index] = len(newViews)
		newViews = append(newViews, copied)
	}

	accessors := make([]Accessor, len(d.Accessors))
	copy(accessors, d.Accessors)
	for i := range accessors {
		if accessors[i].BufferView == nil {
			continue
		}
		old := *accessors[i].BufferView
		if old < 0 || old >= len(remap) || remap[old] < 0 {
			return nil, fmt.Errorf("%w: accessor %d lost its buffer view", ErrFormat, i)
		}
		mapped := remap[old]
		accessors[i].BufferView = &mapped
	}
	for i := range images {
		if images[i].BufferView == nil {
			continue
		}
		old := *images[i].BufferView
		if old < 0 || old >= len(remap) || remap[old] < 0 {
			return nil, fmt.Errorf("%w: image %d lost its buffer view", ErrFormat, i)
		}
		mapped := remap[old]
		images[i].BufferView = &mapped
	}

	root := make(map[string]json.RawMessage, len(d.root)+4)
	for key, value := range d.root {
		root[key] = value
	}
	if err := setSection(root, "accessors", accessors); err != nil {
		return nil, err
	}
	if err := setSection(root, "bufferViews", newViews); err != nil {
		return nil, err
	}
	if err := setSection(root, "meshes", d.Meshes); err != nil {
		return nil, err
	}
	if len(images) > 0 {
		if err := setSection(root, "images", images); err != nil {
			return nil, err
		}
	}
	if err := d.writeChangedSections(root); err != nil {
		return nil, err
	}
	if err := setSection(root, "buffers", []Buffer{{ByteLength: len(payload)}}); err != nil {
		return nil, err
	}

	jsonChunk, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("gltfedit: marshal document: %w", err)
	}
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	for len(payload)%4 != 0 {
		payload = append(payload, 0)
	}

	total := 12 + 8 + len(jsonChunk)
	if len(payload) > 0 {
		total += 8 + len(payload)
	}
	out := make([]byte, 0, total)
	out = append(out, 'g', 'l', 'T', 'F')
	out = appendU32(out, 2)
	out = appendU32(out, uint32(total))
	out = appendU32(out, uint32(len(jsonChunk)))
	out = appendU32(out, 0x4E4F534A)
	out = append(out, jsonChunk...)
	if len(payload) > 0 {
		out = appendU32(out, uint32(len(payload)))
		out = appendU32(out, 0x004E4942)
		out = append(out, payload...)
	}
	return out, nil
}

// bufferViewGCSafe reports whether the writer knows every place a buffer view
// can be referenced from. An unknown extension may hold a reference the
// writer cannot see, so the writer then keeps every view.
func (d *Document) bufferViewGCSafe() bool {
	for _, extension := range d.ExtensionsUsed {
		if !accessorSafeExtensions[strings.TrimSpace(extension)] {
			return false
		}
	}
	for _, mesh := range d.Meshes {
		for _, primitive := range mesh.Primitives {
			if len(primitive.Extensions) > 0 {
				return false
			}
		}
	}
	// A sparse accessor names its own buffer views inside JSON the package
	// keeps opaque, so garbage collection cannot see those references.
	for _, accessor := range d.Accessors {
		if len(accessor.Sparse) > 0 {
			return false
		}
	}
	return true
}

// writeChangedSections serializes only the sections a caller changed. A
// section nobody touched keeps its original bytes.
func (d *Document) writeChangedSections(root map[string]json.RawMessage) error {
	if d.nodesDirty {
		if err := setSection(root, "nodes", d.Nodes); err != nil {
			return err
		}
	}
	if d.scenesDirty {
		if err := setSection(root, "scenes", d.Scenes); err != nil {
			return err
		}
	}
	if d.skinsDirty {
		if err := setSection(root, "skins", d.Skins); err != nil {
			return err
		}
	}
	if d.animationsDirty {
		if err := setSection(root, "animations", d.Animations); err != nil {
			return err
		}
	}
	if d.extensionsDirty {
		if err := setSection(root, "extensionsUsed", d.ExtensionsUsed); err != nil {
			return err
		}
	}
	return nil
}

// viewDigest keys a buffer view payload by its length and its content hash.
func viewDigest(data []byte) string {
	sum := sha256.Sum256(data)
	var scratch [8]byte
	binary.LittleEndian.PutUint64(scratch[:], uint64(len(data)))
	return string(scratch[:]) + string(sum[:])
}

func setSection(root map[string]json.RawMessage, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("gltfedit: marshal %s: %w", key, err)
	}
	root[key] = encoded
	return nil
}

func appendU32(dst []byte, value uint32) []byte {
	var scratch [4]byte
	binary.LittleEndian.PutUint32(scratch[:], value)
	return append(dst, scratch[:]...)
}

// AccessorInfo returns a copy of one accessor record.
func (d *Document) AccessorInfo(index int) Accessor {
	if index < 0 || index >= len(d.Accessors) {
		return Accessor{}
	}
	return d.Accessors[index]
}
