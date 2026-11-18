//go:build e2e

package e2e

import (
	"testing"

	"m31labs.dev/gosx/scene"
)

// shadowAllocationProbe records every texImage3D allocation, which is how the
// WebGL renderer creates a shadow map: one depth array per shadow slot.
const shadowAllocationProbe = `window.__gosx_scene3d_render_truth=true;(() => {
  const p = WebGL2RenderingContext.prototype, allocate = p.texImage3D;
  window.__depthArrays = [];
  p.texImage3D = function(target, level, internal, width, height, depth) {
    if (internal === this.DEPTH_COMPONENT24) window.__depthArrays.push(width);
    return allocate.apply(this, arguments);
  };
})()`

// TestScene3DShadowMapSizeFollowsTheSceneCap checks that Props.Shadows.MaxPixels
// decides how large a shadow map the renderer allocates. The scene state used
// to drop the cap, so every map was 1024 pixels a side: a light that asked for
// 4096 under a 4096 cap got 1024, and so did a 2048 light under a 512 cap.
func TestScene3DShadowMapSizeFollowsTheSceneCap(t *testing.T) {
	for _, c := range []struct {
		name            string
		cap, size, want int
	}{
		{"default-cap", 0, 4096, 1024},
		{"cap-raised-to-4096", scene.ShadowMaxPixels4096, 4096, 4096},
		{"cap-lowered-to-512", scene.ShadowMaxPixels512, 2048, 512},
		{"light-below-the-cap", scene.ShadowMaxPixels4096, 2048, 2048},
	} {
		t.Run(c.name, func(t *testing.T) {
			props := shadowStage(t, []stagePost{{X: -1}, {X: 1, Z: -0.5}}, scene.Vec3(0.6, -1, -0.5), stageFeatures{})
			props.Shadows = scene.Shadows{MaxPixels: c.cap}
			nodes := props.Graph.Nodes
			light := nodes[0].(scene.DirectionalLight)
			light.ShadowSize = c.size
			nodes[0] = light
			props.Graph = scene.NewGraph(nodes...)

			shadowStageInit = shadowAllocationProbe
			t.Cleanup(func() { shadowStageInit = `window.__gosx_scene3d_render_truth=true` })
			page := shadowStagePage(t, props)
			stableStageShot(t, page)
			var sizes []int
			page.eval(t, `window.__depthArrays`, &sizes)
			// The renderer also keeps a 1 pixel placeholder for an unused slot.
			largest := 0
			for _, size := range sizes {
				largest = max(largest, size)
			}
			if largest != c.want {
				t.Fatalf("a %d pixel light under a cap of %d pixels got a %d pixel shadow map, want %d (depth arrays: %v)", c.size, c.cap, largest, c.want, sizes)
			}
		})
	}
}
