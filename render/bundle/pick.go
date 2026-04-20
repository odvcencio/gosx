package bundle

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/odvcencio/gosx/render/gpu"
)

// PickCallback receives the object ID under the requested pixel. ID 0
// means the cursor was over background (no pickable surface).
type PickCallback func(id uint32)

// pickRowAlignment is WebGPU's minimum bytesPerRow alignment for
// copyTextureToBuffer. The spec guarantees 256.
const pickRowAlignment = 256

// pickRequest tracks one queued pick from QueuePick until its staging
// buffer has been copied to + read back.
type pickRequest struct {
	x, y       int
	cb         PickCallback
	staging    gpu.Buffer
	inFlight   bool
	submitFrame bool // flagged on the frame we enqueued the copy
}

// QueuePick schedules a one-pixel readback from the id buffer at the given
// window coordinates. The callback runs on the frame AFTER the read-back
// buffer is available — typically 1–2 frames of latency. Only one pick may
// be in flight at a time; subsequent calls replace the pending request.
func (r *Renderer) QueuePick(x, y int, cb PickCallback) {
	if cb == nil {
		return
	}
	r.pickMu.Lock()
	defer r.pickMu.Unlock()
	if r.pendingPick != nil && r.pendingPick.staging != nil {
		// Drop the existing request — its callback never fires. The caller
		// should only keep the most recent pick anyway (mouse hover etc.).
		r.pendingPick.staging.Destroy()
	}
	r.pendingPick = &pickRequest{x: x, y: y, cb: cb}
}

// recordPickCopy, if a pick is queued and hasn't been submitted yet,
// allocates a staging buffer and records a 1×1 texture→buffer copy from
// the id buffer at the pick coordinates. Called between the main pass and
// the present pass so the id buffer has just been written.
func (r *Renderer) recordPickCopy(enc gpu.CommandEncoder, surfaceWidth, surfaceHeight int) {
	r.pickMu.Lock()
	req := r.pendingPick
	r.pickMu.Unlock()
	if req == nil || req.submitFrame {
		return
	}
	if req.x < 0 || req.x >= surfaceWidth || req.y < 0 || req.y >= surfaceHeight {
		// Out-of-bounds coordinates — synthesize a background hit.
		req.cb(0)
		r.pickMu.Lock()
		if r.pendingPick == req {
			r.pendingPick = nil
		}
		r.pickMu.Unlock()
		return
	}

	staging, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  pickRowAlignment, // 256 bytes — smallest valid copy target
		Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst,
		Label: "bundle.pick.staging",
	})
	if err != nil {
		// Staging allocation failed. Treat as background and drop.
		req.cb(0)
		r.pickMu.Lock()
		if r.pendingPick == req {
			r.pendingPick = nil
		}
		r.pickMu.Unlock()
		return
	}
	enc.CopyTextureToBuffer(
		gpu.TextureCopyInfo{Texture: r.idBufferTex, Origin: [3]int{req.x, req.y, 0}},
		gpu.BufferCopyInfo{Buffer: staging, BytesPerRow: pickRowAlignment, RowsPerImage: 1},
		1, 1, 1,
	)

	r.pickMu.Lock()
	req.staging = staging
	req.submitFrame = true
	r.pickMu.Unlock()
}

// finishPickReadback, if the queued pick has been submitted to the GPU,
// kicks off an async readback in a dedicated goroutine. The goroutine
// blocks on ReadAsync (WebGPU mapAsync), decodes the u32, fires the
// callback, and disposes the staging buffer.
func (r *Renderer) finishPickReadback() {
	r.pickMu.Lock()
	req := r.pendingPick
	r.pickMu.Unlock()
	if req == nil || !req.submitFrame || req.inFlight || req.staging == nil {
		return
	}
	req.inFlight = true
	staging := req.staging
	cb := req.cb

	go func() {
		data, err := staging.ReadAsync(4) // 4 bytes = one u32
		defer staging.Destroy()
		defer func() {
			r.pickMu.Lock()
			if r.pendingPick == req {
				r.pendingPick = nil
			}
			r.pickMu.Unlock()
		}()
		if err != nil {
			cb(0)
			return
		}
		id := binary.LittleEndian.Uint32(data)
		cb(id)
	}()
}

// pickState holds synchronous access to the single in-flight pick request.
type pickState struct {
	mu          sync.Mutex
	pendingPick *pickRequest
}

// describePick is a helper for diagnostics; kept public to the package so
// test harnesses can inspect pending state without reaching into the mutex.
func (r *Renderer) describePick() string {
	r.pickMu.Lock()
	defer r.pickMu.Unlock()
	if r.pendingPick == nil {
		return "none"
	}
	return fmt.Sprintf("pending@(%d,%d) submitted=%v inFlight=%v",
		r.pendingPick.x, r.pendingPick.y,
		r.pendingPick.submitFrame, r.pendingPick.inFlight)
}
