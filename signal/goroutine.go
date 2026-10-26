package signal

// Goroutine identity for scoping the dependency tracker and the batch depth.
//
// Reactive tracking is inherently goroutine-scoped: a tracking window covers
// the dynamic extent of one function call on one goroutine. Go gives no
// goroutine-local storage, so the package derives the identity from the
// goroutine header that runtime.Stack writes.
//
// The lookup costs about one microsecond, so every caller must check a global
// atomic counter first and pay the lookup only while a window or a batch is
// open. Reads outside a tracking window and writes outside a batch stay free.
//
// On runtimes that do not emit the standard header (TinyGo, for example) the
// helper returns 0. Every goroutine then shares one scope, which matches the
// single-threaded browser target the package documents.
const goroutinePrefix = "goroutine "

func parseGoroutineID(header []byte) uint64 {
	if len(header) <= len(goroutinePrefix) {
		return 0
	}
	for i := 0; i < len(goroutinePrefix); i++ {
		if header[i] != goroutinePrefix[i] {
			return 0
		}
	}
	var id uint64
	for _, c := range header[len(goroutinePrefix):] {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}
