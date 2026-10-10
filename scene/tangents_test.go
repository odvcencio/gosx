package scene

import (
	"math"
	"reflect"
	"testing"
)

func tangentQuad() BufferGeometry {
	return BufferGeometry{
		Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0},
		Normals:   []float64{0, 0, 2, 0, 0, 2, 0, 0, 2, 0, 0, 2},
		UVs:       []float64{0, 0, 1, 0, 0, 1, 1, 1}, Indices: []int{0, 1, 2, 1, 3, 2},
		Immutable: true, Revision: 7,
	}
}

func TestGenerateTangentsIndexedMirroredAndFlat(t *testing.T) {
	for _, mirrored := range []bool{false, true} {
		g := tangentQuad()
		if mirrored {
			for i := 0; i < len(g.UVs); i += 2 {
				g.UVs[i] = -g.UVs[i]
			}
		}
		got, err := GenerateTangents(g)
		if err != nil {
			t.Fatal(err)
		}
		want := []float64{1, 0, 0, 1}
		if mirrored {
			want = []float64{-1, 0, 0, -1}
		}
		for i := 0; i < 4; i++ {
			if !reflect.DeepEqual(got.Tangents[i*4:i*4+4], want) {
				t.Fatalf("frame %v want %v", got.Tangents, want)
			}
		}
		flat := BufferGeometry{}
		for _, i := range g.Indices {
			flat.Positions = append(flat.Positions, g.Positions[i*3:i*3+3]...)
			flat.Normals = append(flat.Normals, g.Normals[i*3:i*3+3]...)
			flat.UVs = append(flat.UVs, g.UVs[i*2:i*2+2]...)
		}
		flat, err = GenerateTangents(flat)
		if err != nil {
			t.Fatal(err)
		}
		for i, index := range g.Indices {
			if !reflect.DeepEqual(flat.Tangents[i*4:i*4+4], got.Tangents[index*4:index*4+4]) {
				t.Fatal("indexed/flat tangent mismatch")
			}
		}
		got.Tangents = nil
		if !reflect.DeepEqual(got, g) {
			t.Fatal("changed geometry or ownership metadata")
		}
	}
}

func TestGenerateTangentsDegenerateUVAndValidation(t *testing.T) {
	g := tangentQuad()
	g.UVs = make([]float64, 8)
	for i := 0; i < len(g.Normals); i += 3 {
		g.Normals[i], g.Normals[i+2] = 1, 0
	}
	got, err := GenerateTangents(g)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		v := got.Tangents[i*4 : i*4+4]
		if v[0] != 0 || math.Abs(v[1]*v[1]+v[2]*v[2]-1) > 1e-12 {
			t.Fatalf("nonorthogonal fallback %v", v)
		}
	}
	for _, corrupt := range []func(*BufferGeometry){
		func(g *BufferGeometry) { g.Indices[0] = -1 },
		func(g *BufferGeometry) { g.Indices[0] = 4 },
		func(g *BufferGeometry) { g.Indices = g.Indices[:2] },
		func(g *BufferGeometry) { g.UVs = nil },
		func(g *BufferGeometry) { g.Normals = g.Normals[:3] },
		func(g *BufferGeometry) { g.Normals = make([]float64, 12) },
		func(g *BufferGeometry) { g.Positions[0] = math.NaN() },
	} {
		g := tangentQuad()
		g.Tangents = []float64{99}
		corrupt(&g)
		out, err := GenerateTangents(g)
		if err == nil {
			t.Fatal("accepted malformed geometry")
		}
		if !reflect.DeepEqual(out.Tangents, []float64{99}) {
			t.Fatal("failed generation changed input")
		}
	}
}
