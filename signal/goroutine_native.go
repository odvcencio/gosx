//go:build !tinygo || !wasm

package signal

import (
	"runtime"
	"sync"
)

// stackBufPool reuses the small scratch buffer that runtime.Stack fills. The
// argument escapes to the heap, so a pool keeps the lookup allocation free.
var stackBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32)
		return &buf
	},
}

func goroutineID() uint64 {
	bufPtr := stackBufPool.Get().(*[]byte)
	buf := *bufPtr
	n := runtime.Stack(buf, false)
	id := parseGoroutineID(buf[:n])
	stackBufPool.Put(bufPtr)
	return id
}
