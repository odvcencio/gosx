//go:build e2e

package e2e

import (
	"testing"
	"time"

	"m31labs.dev/gosx/examples/game-stress/stage"
)

// crowdCullDeviceHook records how the page's WebGPU device ends.
const crowdCullDeviceHook = `
window.__crowdCullLost = "";
if (window.GPUAdapter) {
  const request = GPUAdapter.prototype.requestDevice;
  GPUAdapter.prototype.requestDevice = async function (desc) {
    const device = await request.call(this, desc);
    device.lost.then((info) => { window.__crowdCullLost = (info.reason || "lost") + ": " + info.message; });
    return device;
  };
}
`

// TestCrowdGPUCullingKeepsTheWebGPUDevice draws the stress scene's crowd with
// scene.GPUDriven culling on WebGPU and checks that the device survives the
// culled frames. On Metal (Apple M4 Pro, Chrome 157) a command buffer that held
// the crowd's cull pass and the renderer's frame-end timestamp failed with
// "Insufficient Memory" and lost the device within a second of the first culled
// frame. Run it on both GPU runners:
//
//	gpu-chrome.sh run -- go test -tags e2e -run CrowdGPUCulling ./e2e
//	mac-gpu-run <worktree> go test -tags e2e -run CrowdGPUCulling ./e2e
func TestCrowdGPUCullingKeepsTheWebGPUDevice(t *testing.T) {
	if !gpuRunnerAvailable() {
		t.Skip("needs a GPU runner: gpu-chrome.sh run or mac-gpu-run")
	}
	base := startStressServer(t)
	for _, mode := range []struct{ name, query string }{{"frustum", ""}, {"occlusion", "&occlusion=1"}} {
		t.Run(mode.name, func(t *testing.T) {
			flags := map[string]any{"enable-unsafe-webgpu": true, "ignore-gpu-blocklist": true}
			page := newBrowserPage(t, e2eChromePath(t), flags, 1000, 640, crowdCullDeviceHook, 3*time.Minute)
			page.navigate(t, base+"/?backend=webgpu&thread=main&crowd=4000&sections=8&width=960&height=540&audio=off"+mode.query)
			read := `(function () {
  const el = document.getElementById("` + stage.MountID + `"), s = el && el.__gosxScene3DWebGPUStats;
  return { lost: window.__crowdCullLost, backend: el ? el.getAttribute("data-gosx-scene3d-renderer") || "" : "",
    fallback: el ? el.getAttribute("data-gosx-scene3d-renderer-fallback") || "" : "",
    culling: !!(s && s.crowdCulling), occlusion: !!(s && s.crowdOcclusion), agents: s ? s.crowdAgents || 0 : 0, early: s ? s.crowdEarlyVisible || 0 : 0 };
})()`
			var state struct {
				Lost, Backend, Fallback string
				Culling, Occlusion      bool
				Agents, Early           float64
			}
			// Cull for 5 s after culling starts (its kernels compile first).
			var culling time.Time
			for deadline := time.Now().Add(120 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
				page.eval(t, read, &state)
				if state.Lost != "" || state.Fallback != "" || !culling.IsZero() && time.Since(culling) > 5*time.Second {
					break
				}
				if state.Culling && culling.IsZero() {
					culling = time.Now()
				}
			}
			t.Logf("%s: %+v", mode.name, state)
			if state.Lost != "" || state.Fallback != "" {
				t.Fatalf("the WebGPU device did not survive GPU crowd culling: lost %q, fallback %q", state.Lost, state.Fallback)
			}
			if culling.IsZero() || !state.Culling {
				t.Fatalf("GPU crowd culling never ran: %+v", state)
			}
			if state.Agents < 4000 || state.Early <= 0 {
				t.Fatalf("the culled crowd drew no figure: %+v", state)
			}
			if mode.query != "" && !state.Occlusion {
				t.Fatalf("occlusion culling never switched on: %+v", state)
			}
		})
	}
}
