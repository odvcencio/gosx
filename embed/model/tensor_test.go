package model

import (
	"bytes"
	"testing"
)

func TestTensorRoundTrip(t *testing.T) {
	orig := Tensor{
		Name:  "layer.0.weight",
		Shape: []int{384, 384},
		Data:  make([]float32, 384*384),
	}
	for i := range orig.Data {
		orig.Data[i] = float32(i) * 0.001
	}
	var buf bytes.Buffer
	if err := WriteTensor(&buf, orig); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTensor(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != orig.Name {
		t.Errorf("name: got %q want %q", got.Name, orig.Name)
	}
	if len(got.Data) != len(orig.Data) {
		t.Fatalf("data len: got %d want %d", len(got.Data), len(orig.Data))
	}
	for i := range got.Data {
		if got.Data[i] != orig.Data[i] {
			t.Fatalf("data[%d]: got %f want %f", i, got.Data[i], orig.Data[i])
		}
	}
}

func TestTensorFileRoundTrip(t *testing.T) {
	tensors := []Tensor{
		{Name: "embed.weight", Shape: []int{30522, 384}, Data: make([]float32, 8)},
		{Name: "layer.0.attn.qkv.weight", Shape: []int{1152, 384}, Data: make([]float32, 8)},
	}
	for i := range tensors[0].Data {
		tensors[0].Data[i] = float32(i)
	}
	var buf bytes.Buffer
	if err := WriteTensorFile(&buf, tensors); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTensorFile(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("tensor count: got %d want 2", len(got))
	}
	if got[0].Name != "embed.weight" {
		t.Errorf("tensor 0 name: got %q want %q", got[0].Name, "embed.weight")
	}
}
