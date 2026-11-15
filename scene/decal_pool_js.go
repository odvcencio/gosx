//go:build js && wasm

package scene

import (
	"syscall/js"
	"unsafe"
)

// decalPoolBridge holds the browser objects for one published pool.
type decalPoolBridge struct {
	store js.Value // window.__gosx_scene3d_decal_pools[name]
	bytes js.Value // Uint8Array view over the Float32Array storage
}

var decalPoolBridges = map[*DecalPool]*decalPoolBridge{}

// Publish sends the pool's records to the browser stream named by its config.
// The first call creates a browser typed array sized to Capacity; later calls
// copy bytes into it and update the count, without allocating Go memory. The
// call returns false when there is no window. It publishes only when the
// pool's revision changed since the last call.
func (p *DecalPool) Publish() bool {
	global := js.Global()
	if p == nil || p.cfg.Name == "" || !global.Truthy() {
		return false
	}
	records := p.Records()
	bridge := decalPoolBridges[p]
	if bridge == nil {
		pools := global.Get("__gosx_scene3d_decal_pools")
		if !pools.Truthy() {
			pools = js.Global().Get("Object").New()
			global.Set("__gosx_scene3d_decal_pools", pools)
		}
		buffer := global.Get("ArrayBuffer").New(p.cfg.Capacity * DecalRecordStride * 4)
		store := js.Global().Get("Object").New()
		store.Set("data", global.Get("Float32Array").New(buffer))
		store.Set("count", 0)
		store.Set("revision", 0)
		pools.Set(p.cfg.Name, store)
		bridge = &decalPoolBridge{store: store, bytes: global.Get("Uint8Array").New(buffer)}
		decalPoolBridges[p] = bridge
	}
	if bridge.store.Get("revision").Int() == int(p.revision) {
		return true
	}
	if len(records) > 0 {
		js.CopyBytesToJS(bridge.bytes, unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*4))
	}
	bridge.store.Set("count", p.n)
	bridge.store.Set("revision", int(p.revision))
	return true
}
