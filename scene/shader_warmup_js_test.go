//go:build js && wasm

package scene

import (
	"syscall/js"
	"testing"
)

func TestSurfaceShaderWarmupProgressReadsTheMount(t *testing.T) {
	f := surfaceFixture(t)
	mount := f.Get("mount")
	attrs := map[string]any{}
	mount.Set("getAttribute", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if v, ok := attrs[args[0].String()]; ok {
			return v
		}
		return js.Null()
	}))
	surface := NewSurface("stage")
	defer surface.Dispose()
	for _, tc := range []struct {
		phase, done, total  any
		wantDone, wantTotal int
	}{
		{nil, nil, nil, 0, 1},          // nothing published yet
		{"compiling", "3", "7", 3, 7},  // mid-compile
		{"compiling", "7", "7", 6, 7},  // linked, no frame drawn yet
		{"ready", "7", "7", 7, 7},      // a frame drew with every program
		{"compiling", "x", "-2", 0, 1}, // unreadable counts stay unfinished
	} {
		attrs = map[string]any{}
		if tc.phase != nil {
			attrs["data-gosx-scene3d-shaders"] = tc.phase
			attrs["data-gosx-scene3d-shaders-done"] = tc.done
			attrs["data-gosx-scene3d-shaders-total"] = tc.total
		}
		done, total := surface.ShaderWarmupProgress()
		if done != tc.wantDone || total != tc.wantTotal {
			t.Errorf("phase %v done %v total %v: got %d/%d, want %d/%d", tc.phase, tc.done, tc.total, done, total, tc.wantDone, tc.wantTotal)
		}
	}
}
