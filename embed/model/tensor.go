package model

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Tensor holds a named multi-dimensional float32 array.
type Tensor struct {
	Name  string
	Shape []int
	Data  []float32
}

// Size returns the total number of elements.
func (t Tensor) Size() int {
	n := 1
	for _, d := range t.Shape {
		n *= d
	}
	return n
}

var tensorMagic = [4]byte{'G', 'S', 'X', 'T'}

// WriteTensor writes a single tensor in binary format.
func WriteTensor(w io.Writer, t Tensor) error {
	nameBytes := []byte(t.Name)
	if err := binary.Write(w, binary.LittleEndian, uint16(len(nameBytes))); err != nil {
		return err
	}
	if _, err := w.Write(nameBytes); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint8(len(t.Shape))); err != nil {
		return err
	}
	for _, d := range t.Shape {
		if err := binary.Write(w, binary.LittleEndian, uint32(d)); err != nil {
			return err
		}
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(t.Data))); err != nil {
		return err
	}
	for _, v := range t.Data {
		if err := binary.Write(w, binary.LittleEndian, math.Float32bits(v)); err != nil {
			return err
		}
	}
	return nil
}

// ReadTensor reads a single tensor from binary format.
func ReadTensor(r io.Reader) (Tensor, error) {
	var nameLen uint16
	if err := binary.Read(r, binary.LittleEndian, &nameLen); err != nil {
		return Tensor{}, err
	}
	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBytes); err != nil {
		return Tensor{}, err
	}
	var ndim uint8
	if err := binary.Read(r, binary.LittleEndian, &ndim); err != nil {
		return Tensor{}, err
	}
	shape := make([]int, ndim)
	for i := range shape {
		var d uint32
		if err := binary.Read(r, binary.LittleEndian, &d); err != nil {
			return Tensor{}, err
		}
		shape[i] = int(d)
	}
	var dataLen uint32
	if err := binary.Read(r, binary.LittleEndian, &dataLen); err != nil {
		return Tensor{}, err
	}
	data := make([]float32, dataLen)
	for i := range data {
		var bits uint32
		if err := binary.Read(r, binary.LittleEndian, &bits); err != nil {
			return Tensor{}, err
		}
		data[i] = math.Float32frombits(bits)
	}
	return Tensor{Name: string(nameBytes), Shape: shape, Data: data}, nil
}

// WriteTensorFile writes multiple tensors with a header.
func WriteTensorFile(w io.Writer, tensors []Tensor) error {
	if _, err := w.Write(tensorMagic[:]); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(tensors))); err != nil {
		return err
	}
	for _, t := range tensors {
		if err := WriteTensor(w, t); err != nil {
			return fmt.Errorf("tensor %q: %w", t.Name, err)
		}
	}
	return nil
}

// ReadTensorFile reads a tensor file.
func ReadTensorFile(r io.Reader) ([]Tensor, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, err
	}
	if magic != tensorMagic {
		return nil, fmt.Errorf("bad magic: %x", magic)
	}
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	tensors := make([]Tensor, count)
	for i := range tensors {
		var err error
		tensors[i], err = ReadTensor(r)
		if err != nil {
			return nil, fmt.Errorf("tensor %d: %w", i, err)
		}
	}
	return tensors, nil
}
