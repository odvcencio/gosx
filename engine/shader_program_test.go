//go:build !js || !wasm

package engine

import "testing"

func TestShaderProgramRejectsMissingMalformedAndIncompleteArtifacts(t *testing.T) {
	for _, layout := range []map[string]any{nil, {"programs": "invalid"}, {"programs": map[string]any{"gles": map[string]any{"vertex": "only vertex"}}}, {"programs": map[string]any{"gles": make(chan int)}}} {
		if _, ok := ShaderProgramFromLayout(layout, "gles"); ok {
			t.Fatal("accepted incomplete shader")
		}
	}
}
