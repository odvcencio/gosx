package scene

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The mount's getFrameStats returns these names (client/js/scene3d-frame-meter.test.js
// pins the same list on the JavaScript side).
var frameStatsFields = []string{"backend", "adapter", "fallbackReason", "tier", "governed", "frames", "fps", "frameMS", "frameP95MS",
	"cpuMS", "cpuP95MS", "gpuMS", "gpuP95MS", "gpuSamples", "gpuTimer", "pixelRatio", "width", "height"}

func TestFrameStatsDecodesTheMountsFrameMeter(t *testing.T) {
	var names []string
	typ := reflect.TypeOf(FrameStats{})
	for i := 0; i < typ.NumField(); i++ {
		names = append(names, typ.Field(i).Tag.Get("json"))
	}
	if !reflect.DeepEqual(names, frameStatsFields) {
		t.Fatalf("FrameStats fields %v, the mount returns %v", names, frameStatsFields)
	}
	raw := `{"backend":"webgl","adapter":"ANGLE (Example GPU)","fallbackReason":"webgpu-unavailable","tier":"balanced","governed":true,
		"frames":120,"fps":58.5,"frameMS":17.1,"frameP95MS":21.0,"cpuMS":4.2,"cpuP95MS":6.0,"gpuMS":9.5,"gpuP95MS":11.0,
		"gpuSamples":40,"gpuTimer":"webgl-timer","pixelRatio":1.35,"width":1920,"height":1080}`
	var got FrameStats
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	want := FrameStats{Backend: "webgl", Adapter: "ANGLE (Example GPU)", FallbackReason: "webgpu-unavailable", Tier: "balanced", Governed: true,
		Frames: 120, FPS: 58.5, FrameMS: 17.1, FrameP95MS: 21, CPUMS: 4.2, CPUP95MS: 6, GPUMS: 9.5, GPUP95MS: 11,
		GPUSamples: 40, GPUTimer: "webgl-timer", PixelRatio: 1.35, Width: 1920, Height: 1080}
	if got != want {
		t.Fatalf("decoded %+v, want %+v", got, want)
	}
}

func TestFrameStatsOutsideTheBrowser(t *testing.T) {
	if _, err := NewSurface("arena").FrameStats(); err == nil {
		t.Fatal("FrameStats outside js/wasm returned no error")
	}
}
