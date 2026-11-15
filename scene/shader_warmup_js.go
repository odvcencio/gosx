//go:build js && wasm

package scene

import (
	"strconv"
	"syscall/js"
)

// ShaderWarmupProgress reports how many of the mount's shader programs have
// compiled, for a mount with Props.ShaderWarmup. It has the signature
// boot.CountTask takes:
//
//	gate.Add(boot.CountTask("shaders", 1, surface.ShaderWarmupProgress))
//
// It returns (0, 1) until the mount publishes progress, and done stays below
// total until the first frame with every listed variant has drawn, so the
// task finishes exactly when the warm-up does. A mount without
// Props.ShaderWarmup never publishes progress. In an engine worker, which
// cannot read the page, it returns (1, 1) so it never holds a gate open.
func (s *Surface) ShaderWarmupProgress() (done, total int) {
	if _, ok := workerBridge(); ok {
		return 1, 1
	}
	mount, err := s.element()
	if err != nil {
		return 0, 1
	}
	return shaderWarmupCounts(mount.Call("getAttribute", "data-gosx-scene3d-shaders"),
		mount.Call("getAttribute", "data-gosx-scene3d-shaders-done"),
		mount.Call("getAttribute", "data-gosx-scene3d-shaders-total"))
}

func shaderWarmupCounts(phase, doneAttr, totalAttr js.Value) (done, total int) {
	if phase.Type() != js.TypeString {
		return 0, 1
	}
	done = jsAttrInt(doneAttr)
	total = jsAttrInt(totalAttr)
	return shaderWarmupGateCounts(phase.String(), done, total)
}

func jsAttrInt(value js.Value) int {
	if value.Type() != js.TypeString {
		return 0
	}
	n, err := strconv.Atoi(value.String())
	if err != nil || n < 0 {
		return 0
	}
	return n
}
