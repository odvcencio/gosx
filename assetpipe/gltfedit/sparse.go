package gltfedit

import (
	"encoding/json"
	"fmt"
)

// AddSparseAccessor appends a float accessor that stores only its nonzero
// elements, as a glTF sparse accessor with no base buffer view: every element
// it does not list reads as zero. It suits morph targets that move a small
// part of a mesh. values holds every element, zeros included; min and max
// cover all of them, as glTF asks of a POSITION accessor.
//
// A document with a sparse accessor keeps every buffer view when it is
// written, because the sparse JSON names its own views.
func (d *Document) AddSparseAccessor(values []float64, accessorType string) (int, error) {
	components, err := TypeComponents(accessorType)
	if err != nil {
		return 0, err
	}
	if components == 0 || len(values)%components != 0 {
		return 0, fmt.Errorf("%w: %d values do not fill %s elements", ErrFormat, len(values), accessorType)
	}
	count := len(values) / components
	var indices []int32
	var stored []float64
	for i := 0; i < count; i++ {
		element := values[i*components : (i+1)*components]
		for _, v := range element {
			if v != 0 {
				indices = append(indices, int32(i))
				stored = append(stored, element...)
				break
			}
		}
	}
	accessor := Accessor{ComponentType: ComponentFloat, Count: count, Type: accessorType}
	accessor.Min, accessor.Max = bounds(values, components)
	if len(indices) > 0 {
		indexType := IndexComponentType(count)
		size, err := componentSize(indexType)
		if err != nil {
			return 0, err
		}
		indexBytes := make([]byte, len(indices)*size)
		for i, v := range indices {
			writeComponent(indexBytes[i*size:], float64(v), indexType, false)
		}
		valueBytes := make([]byte, len(stored)*4)
		for i, v := range stored {
			writeComponent(valueBytes[i*4:], v, ComponentFloat, false)
		}
		indexView := d.appendView(indexBytes, 0)
		valueView := d.appendView(valueBytes, 0)
		accessor.Sparse, err = json.Marshal(map[string]any{
			"count":   len(indices),
			"indices": map[string]int{"bufferView": indexView, "componentType": indexType},
			"values":  map[string]int{"bufferView": valueView},
		})
		if err != nil {
			return 0, err
		}
	}
	d.Accessors = append(d.Accessors, accessor)
	return len(d.Accessors) - 1, nil
}

// sparseJSON is a sparse accessor's description.
type sparseJSON struct {
	Count   int `json:"count"`
	Indices struct {
		BufferView    int `json:"bufferView"`
		ByteOffset    int `json:"byteOffset"`
		ComponentType int `json:"componentType"`
	} `json:"indices"`
	Values struct {
		BufferView int `json:"bufferView"`
		ByteOffset int `json:"byteOffset"`
	} `json:"values"`
}

// readSparse decodes a sparse accessor: its base (zeros without a buffer
// view) with the listed elements replaced.
func (d *Document) readSparse(index int) ([]float64, int, error) {
	accessor := d.Accessors[index]
	var sp sparseJSON
	if err := json.Unmarshal(accessor.Sparse, &sp); err != nil {
		return nil, 0, fmt.Errorf("%w: sparse accessor %d: %v", ErrFormat, index, err)
	}
	base := accessor
	base.Sparse = nil
	d.Accessors[index] = base
	out, components, err := d.ReadAccessor(index)
	d.Accessors[index] = accessor
	if err != nil {
		return nil, 0, err
	}
	if sp.Count < 0 || sp.Count > accessor.Count {
		return nil, 0, fmt.Errorf("%w: sparse accessor %d count %d", ErrFormat, index, sp.Count)
	}
	indexSize, err := componentSize(sp.Indices.ComponentType)
	if err != nil {
		return nil, 0, err
	}
	valueSize, err := componentSize(accessor.ComponentType)
	if err != nil {
		return nil, 0, err
	}
	_, indexData, err := d.viewBytes(sp.Indices.BufferView)
	if err != nil {
		return nil, 0, err
	}
	_, valueData, err := d.viewBytes(sp.Values.BufferView)
	if err != nil {
		return nil, 0, err
	}
	for s := 0; s < sp.Count; s++ {
		ia := sp.Indices.ByteOffset + s*indexSize
		va := sp.Values.ByteOffset + s*components*valueSize
		if ia+indexSize > len(indexData) || va+components*valueSize > len(valueData) {
			return nil, 0, fmt.Errorf("%w: sparse accessor %d runs past its views", ErrFormat, index)
		}
		at := int(readComponent(indexData[ia:], sp.Indices.ComponentType, false))
		if at < 0 || at >= accessor.Count {
			return nil, 0, fmt.Errorf("%w: sparse accessor %d index %d", ErrFormat, index, at)
		}
		for c := 0; c < components; c++ {
			out[at*components+c] = readComponent(valueData[va+c*valueSize:], accessor.ComponentType, accessor.Normalized)
		}
	}
	return out, components, nil
}
