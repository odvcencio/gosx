//go:build !js || !wasm

package bundle

import (
	"fmt"
	"testing"

	"m31labs.dev/gosx/engine"
)

func diagnosticScaleBundle(count int) engine.RenderBundle {
	b := engine.RenderBundle{
		Materials:   make([]engine.RenderMaterial, count),
		MeshObjects: make([]engine.RenderObject, count),
	}
	for i := range b.Materials {
		b.Materials[i] = engine.RenderMaterial{Kind: "custom", ShaderLayout: map[string]any{
			"programs": map[string]any{"metal": engine.ShaderProgram{Source: "authored metal"}},
		}}
		b.MeshObjects[i] = engine.RenderObject{MaterialIndex: i, VertexCount: 3}
	}
	return b
}

func BenchmarkMaterialDiagnostics(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("materials=%d/steady-frame", count), func(b *testing.B) {
			bundle := diagnosticScaleBundle(count)
			var cache materialDiagnosticsCache
			var stats frameStatsRecorder
			stats.setMaterialFallbacks(cache.update(bundle))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				stats.setMaterialFallbacks(cache.update(bundle))
			}
		})
		b.Run(fmt.Sprintf("materials=%d/one-shot", count), func(b *testing.B) {
			bundle := diagnosticScaleBundle(count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := MaterialDiagnostics(bundle); len(got) != count {
					b.Fatalf("diagnostics: %d, want %d", len(got), count)
				}
			}
		})
	}
}
