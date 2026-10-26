package scene

import (
	"testing"
)

// benchInstancedMeshTransforms builds the flattened column-major 4x4
// transforms MountCommandBatch's JSON path already sends for a rigid
// InstancedMesh batch (see graphLowerer.lowerInstancedMesh), one distinct
// value per float so neither encoder can special-case a repeated value.
func benchInstancedMeshTransforms(count int) []float64 {
	transforms := make([]float64, count*16)
	for i := range transforms {
		transforms[i] = float64(i) * 0.125
	}
	return transforms
}

func benchInstancedMeshIR(count int) InstancedMeshIR {
	return InstancedMeshIR{
		ID:         "crowd-actors",
		Count:      count,
		Kind:       "box",
		Size:       1.2,
		Color:      "#8de1ff",
		Transforms: benchInstancedMeshTransforms(count),
	}
}

func benchInstanceStreamFrame(count int) InstanceStreamFrame {
	data := make([]float32, count*16)
	source := benchInstancedMeshTransforms(count)
	for i, v := range source {
		data[i] = float32(v)
	}
	return InstanceStreamFrame{
		BatchID:  "crowd-actors",
		Revision: 1,
		Kind:     InstanceStreamTransform,
		Count:    count,
		Data:     data,
	}
}

// BenchmarkInstancedMeshTransportJSON measures the status-quo per-frame
// path: build a MountCommandBatch carrying CommandSetInstancedMeshes and
// json.Marshal it, exactly what a Go/WASM game loop does today before
// handing the bytes to JSON.parse in JS (see chitin-choir client/render.go).
func BenchmarkInstancedMeshTransportJSON(b *testing.B) {
	for _, count := range []int{180, 2000} {
		mesh := benchInstancedMeshIR(count)
		b.Run(benchCountLabel(count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				batch := MountCommandBatch{
					Revision: uint64(i + 1),
					Commands: []Command{SetInstancedMeshesCommand([]InstancedMeshIR{mesh})},
				}
				out, err := batch.Marshal()
				if err != nil {
					b.Fatal(err)
				}
				if len(out) == 0 {
					b.Fatal("empty payload")
				}
			}
		})
	}
}

// BenchmarkInstancedMeshTransportBinary measures the InstanceStream fast
// path for the same instance count and the same transform values.
func BenchmarkInstancedMeshTransportBinary(b *testing.B) {
	for _, count := range []int{180, 2000} {
		frame := benchInstanceStreamFrame(count)
		b.Run(benchCountLabel(count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				frame.Revision = uint64(i + 1)
				out, err := frame.Encode()
				if err != nil {
					b.Fatal(err)
				}
				if len(out) == 0 {
					b.Fatal("empty payload")
				}
			}
		})
	}
}

func BenchmarkInstancedMeshTransportBinaryReuse(b *testing.B) {
	for _, count := range []int{180, 2000} {
		frame := benchInstanceStreamFrame(count)
		buffer, err := frame.Encode()
		if err != nil {
			b.Fatal(err)
		}
		b.Run(benchCountLabel(count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				frame.Revision = uint64(i + 1)
				buffer, err = frame.EncodeInto(buffer)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchCountLabel(count int) string {
	switch count {
	case 180:
		return "instances=180"
	case 2000:
		return "instances=2000"
	default:
		return "instances=n"
	}
}

// Compare changing count+color declarations to their retained binary snapshot.
// This isolates packing/encoding; it makes no renderer or application FPS claim.
func BenchmarkInstancedMeshCountColor(b *testing.B) {
	for _, count := range []int{180, 2000} {
		mesh := benchInstancedMeshIR(count)
		mesh.Colors = make([]string, count)
		records := make([]InstanceTransformColor, count)
		for i := range records {
			mesh.Colors[i] = "#ff8000"
			for c := range records[i].Transform {
				records[i].Transform[c] = float32(mesh.Transforms[i*16+c])
			}
			records[i].Color = [4]float32{1, 128.0 / 255, 0, 1}
		}
		b.Run(benchCountLabel(count), func(b *testing.B) {
			batch := MountCommandBatch{Revision: 1, Commands: []Command{SetInstancedMeshesCommand([]InstancedMeshIR{mesh})}}
			encoded, err := batch.Marshal()
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Declaration", func(b *testing.B) {
				b.ReportAllocs()
				b.ReportMetric(float64(len(encoded)), "B/frame")
				for i := 0; i < b.N; i++ {
					batch.Revision = uint64(i + 1)
					if _, err := batch.Marshal(); err != nil {
						b.Fatal(err)
					}
				}
			})
			frame := InstanceStreamFrame{BatchID: mesh.ID, Revision: 1}
			frame.SetTransformColors(records)
			buffer, err := frame.Encode()
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Retained", func(b *testing.B) {
				b.ReportAllocs()
				b.ReportMetric(float64(len(buffer)), "B/frame")
				for i := 0; i < b.N; i++ {
					frame.Revision = uint64(i + 1)
					frame.SetTransformColors(records)
					buffer, err = frame.EncodeInto(buffer)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
