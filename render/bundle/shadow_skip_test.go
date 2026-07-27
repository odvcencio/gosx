package bundle

import (
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu"
)

// The shadow pass costs a clear whether or not anything casts.
//
// The cascades are 2048 squares, three of them, so the clear alone touches about
// twelve million depth values. A profile of an empty sixteen-pixel frame put most
// of its samples there. The clear is a memmove now, but the pass still ran on
// every frame of every scene, including the twenty-four golden matrix cases that
// author no shadow at all.
//
// recordShadowPass now skips a caster-free frame, but only once the cascades are
// known clear. The tests below pin both halves: the skip happens, and it never
// leaves a stale occluder behind.

// countShadowPasses reports how many shadow cascade passes one encoder recorded.
func countShadowPasses(enc *fakeEncoder) int {
	count := 0
	for _, pass := range enc.passes {
		if pass.desc.Label == "bundle.shadow.cascade" {
			count++
		}
	}
	return count
}

// shadowSkipBundle builds one sphere that either casts or does not.
func shadowSkipBundle(castShadow bool) engine.RenderBundle {
	return engine.RenderBundle{
		Camera:    engine.RenderCamera{Z: 6, FOV: 1, Near: 0.1, Far: 100},
		Materials: []engine.RenderMaterial{{Kind: "standard", Color: "#ffffff"}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID: "probe", Kind: "sphere", MaterialIndex: 0,
			InstanceCount: 1, Transforms: identityTransform(),
			CastShadow: castShadow,
		}},
	}
}

// TestShadowPassSkipsWhenNothingCasts pins the skip and the state it depends on.
//
// A PASS PROVES: a caster-free frame records no shadow pass once the cascades are
// clear, the first caster-free frame still records one pass per cascade to clear
// them, and the frame after a caster returns records the passes again.
//
// A PASS DOES NOT PROVE: that the skip is free of a stale shadow at the pixel
// level. TestShadowSkipDoesNotLeaveAStaleOccluder in render/gpu/headless proves
// that against real pixels.
func TestShadowPassSkipsWhenNothingCasts(t *testing.T) {
	device := newFakeDevice()
	renderer, err := New(Config{Device: device, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer renderer.Destroy()

	frame := func(castShadow bool) int {
		before := len(device.encoders)
		if err := renderer.Frame(shadowSkipBundle(castShadow), 320, 180, 0); err != nil {
			t.Fatalf("Frame: %v", err)
		}
		if len(device.encoders) != before+1 {
			t.Fatalf("frame recorded %d encoders, want one", len(device.encoders)-before)
		}
		return countShadowPasses(device.encoders[len(device.encoders)-1])
	}

	// The first caster-free frame must still clear. The renderer cannot know what
	// the cascades hold before it has cleared them once.
	if got := frame(false); got != cascadeCount {
		t.Fatalf("the first caster-free frame recorded %d shadow passes, want %d; "+
			"the cascades have never been cleared, so the pass cannot be skipped yet", got, cascadeCount)
	}
	// Every later caster-free frame skips.
	for index := 0; index < 3; index++ {
		if got := frame(false); got != 0 {
			t.Fatalf("caster-free frame %d recorded %d shadow passes, want 0; "+
				"recordShadowPass no longer skips a frame with nothing to draw", index+2, got)
		}
	}
	// A caster returns, so the passes must return with it.
	if got := frame(true); got != cascadeCount {
		t.Fatalf("a frame with a caster recorded %d shadow passes, want %d", got, cascadeCount)
	}
	// The caster goes away. The cascades now hold that caster, so the next frame
	// must run the passes once more to clear them.
	if got := frame(false); got != cascadeCount {
		t.Fatalf("the first caster-free frame after a caster recorded %d shadow passes, want %d; "+
			"skipping here would leave the previous frame's occluder in the map", got, cascadeCount)
	}
	if got := frame(false); got != 0 {
		t.Fatalf("the second caster-free frame after a caster recorded %d shadow passes, want 0", got)
	}
}

// TestShadowPassSkipKeepsTheClearValue pins what a skipped pass would have
// written.
//
// A skipped pass is only safe because the attachment it would have used carries a
// clear-to-one load operation and no draw. This test reads that descriptor from
// the frame that does run, so a change from clear to load shows up here rather
// than as a shadow that never goes away.
func TestShadowPassSkipKeepsTheClearValue(t *testing.T) {
	device := newFakeDevice()
	renderer, err := New(Config{Device: device, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer renderer.Destroy()
	if err := renderer.Frame(shadowSkipBundle(false), 320, 180, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	found := 0
	for _, pass := range device.encoders[0].passes {
		if pass.desc.Label != "bundle.shadow.cascade" {
			continue
		}
		found++
		attachment := pass.desc.DepthStencilAttachment
		if attachment == nil {
			t.Fatalf("a shadow pass carries no depth attachment")
		}
		if attachment.DepthLoadOp != gpu.LoadOpClear {
			t.Errorf("a shadow pass loads its depth instead of clearing it; "+
				"the skip in recordShadowPass assumes a clear, so it would keep a stale occluder (%v)",
				attachment.DepthLoadOp)
		}
		if attachment.DepthClearValue != 1.0 {
			t.Errorf("a shadow pass clears its depth to %v, want 1.0; "+
				"any other value shadows the whole scene when nothing casts",
				attachment.DepthClearValue)
		}
	}
	if found != cascadeCount {
		t.Fatalf("found %d shadow passes, want %d", found, cascadeCount)
	}
}
