package gltfedit

import (
	"reflect"
	"testing"
)

func TestSparseAccessorRoundTrip(t *testing.T) {
	d := New("test")
	values := []float64{0, 0, 0, 1, 2, 3, 0, 0, 0, 0, 0, 0, -1, 0, .5}
	index, err := d.AddSparseAccessor(values, "VEC3")
	if err != nil {
		t.Fatal(err)
	}
	zeros, err := d.AddSparseAccessor(make([]float64, 6), "VEC3")
	if err != nil {
		t.Fatal(err)
	}
	glb, err := d.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(glb, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, components, err := back.ReadAccessor(index)
	if err != nil || components != 3 || !reflect.DeepEqual(got, values) {
		t.Fatalf("read %v (%d, %v), want %v", got, components, err, values)
	}
	if a := back.Accessors[index]; a.BufferView != nil || a.Count != 5 || a.Min[0] != -1 || a.Max[2] != 3 {
		t.Fatalf("accessor %+v", a)
	}
	got, _, err = back.ReadAccessor(zeros)
	if err != nil || !reflect.DeepEqual(got, make([]float64, 6)) || len(back.Accessors[zeros].Sparse) != 0 {
		t.Fatalf("all-zero accessor: %v %v %s", got, err, back.Accessors[zeros].Sparse)
	}
}
