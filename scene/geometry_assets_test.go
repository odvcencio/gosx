package scene

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

func TestSceneGeometryHashKeepsAssetIdentity(t *testing.T) {
	legacy := func(v *MeshVertices) string {
		var stream bytes.Buffer
		for _, values := range [][]float64{v.Positions, v.Normals, v.UVs, v.Tangents} {
			binary.Write(&stream, binary.LittleEndian, uint64(len(values)))
			binary.Write(&stream, binary.LittleEndian, values)
		}
		binary.Write(&stream, binary.LittleEndian, uint64(len(v.Indices)))
		for _, index := range v.Indices {
			binary.Write(&stream, binary.LittleEndian, uint64(index))
		}
		binary.Write(&stream, binary.LittleEndian, uint64(v.Count))
		for _, enabled := range []bool{v.Immutable, v.Dynamic, v.Revision != nil} {
			binary.Write(&stream, binary.LittleEndian, enabled)
		}
		if v.Revision != nil {
			binary.Write(&stream, binary.LittleEndian, *v.Revision)
		}
		hash := sha256.Sum256(stream.Bytes())
		return hex.EncodeToString(hash[:]) + ".json"
	}
	for _, size := range []int{0, 1, 506, 507, 508, 509, 510, 511, 512, 513, 4096} {
		v := &MeshVertices{Count: size, Indices: []uint32{0, 17, math.MaxUint32}, Tangents: []float64{math.Copysign(0, -1), math.Inf(1), math.Float64frombits(0x7ff8000000000001)}}
		for i := 0; i < size; i++ {
			v.Positions = append(v.Positions, float64(i)/7)
		}
		for _, mode := range []int{0, 1, 2} {
			v.Immutable, v.Dynamic = mode == 1, mode == 2
			if mode != 0 {
				revision := uint64(math.MaxUint64)
				v.Revision = &revision
			}
			if got, want := GeometryHash(v), legacy(v); got != want {
				t.Fatalf("size=%d mode=%d: hash %s, want %s", size, mode, got, want)
			}
		}
	}
}
