//go:build js && wasm

package main

import (
	"errors"

	"m31labs.dev/gosx/client/bridge"
)

// directBatch is the state behind the direct batch entry: a buffer the page
// writes a batch into, and the bridge that applies it. batch_direct_tinygo.go
// exports it to JavaScript.
//
// A call from JavaScript through js.FuncOf makes TinyGo start a goroutine with
// a heap-allocated stack (stack_scrub_tinygo.go says what that costs). A batch
// arrives about twenty times a second and is the hottest call, so the page can
// hand it over through exports instead, which run on the system stack the call
// already has: no goroutine, no stack block. The page falls back to
// __gosx_set_input_batch when the exports are not there.
//
// The whole batch applies on that stack: the decoding of its JSON and the render
// of every island it wakes, which uses about 2 KiB of stack for each level of
// nesting. TinyGo's -stack-size sizes goroutine stacks only, and the linker's own
// default for the system stack is 64 KiB, which a batch that re-renders an island
// about 30 levels deep overflows. cmd/gosx tinyGoBuildArgs therefore links the system
// stack at the size of a goroutine stack, and client/js/signal-batch-depth.mjs
// sends a deep batch to every published runtime. Do not build a runtime that has
// these exports without that flag.
type directBatch struct {
	bridge  *bridge.Bridge
	staging []byte
	failure []byte
}

var direct directBatch

// registerDirectBatch points the direct entry at the bridge the runtime serves.
func registerDirectBatch(b *bridge.Bridge) { direct.bridge = b }

// maxStagingBytes is the largest buffer kept between batches; a bigger one is
// dropped as soon as its batch has been copied out.
const maxStagingBytes = 1 << 20

const minStagingBytes = 4 << 10

// reserve returns a buffer of at least n bytes. The page writes the batch into
// the start of it and then calls apply. The buffer moves when the next reserve
// needs more room, so the page asks again for every batch.
func (d *directBatch) reserve(n int) []byte {
	if n < minStagingBytes {
		n = minStagingBytes
	}
	if cap(d.staging) < n {
		size := minStagingBytes
		for size < n {
			size <<= 1
		}
		d.staging = make([]byte, size)
	}
	d.staging = d.staging[:cap(d.staging)]
	return d.staging
}

// apply applies the first n bytes of the buffer as a signal batch, as
// __gosx_set_input_batch does. The text is copied before anything runs, because
// a subscriber the batch wakes may send a batch of its own and reuse the buffer.
func (d *directBatch) apply(n int, changedOnly bool) error {
	if d.bridge == nil {
		return errors.New("signal batch entry is not registered")
	}
	if n < 0 || n > len(d.staging) {
		return errors.New("signal batch length is outside the reserved buffer")
	}
	text := string(d.staging[:n])
	if cap(d.staging) > maxStagingBytes {
		d.staging = nil
	}
	if changedOnly {
		return d.bridge.SetChangedSharedSignalBatchJSON(text)
	}
	return d.bridge.SetSharedSignalBatchJSON(text)
}
