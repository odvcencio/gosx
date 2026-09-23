//go:build !(js && wasm)

package storage

func defaultBackend() Backend { return NewMemoryBackend() }
