package signal

import (
	"runtime"
	"testing"
)

func BenchmarkGoroutineID(b *testing.B) {
	b.ReportAllocs()
	var sink uint64
	for i := 0; i < b.N; i++ {
		sink += goroutineID()
	}
	_ = sink
}

func BenchmarkGoroutineIDStackBuf(b *testing.B) {
	b.ReportAllocs()
	var sink uint64
	for i := 0; i < b.N; i++ {
		var buf [32]byte
		n := runtime.Stack(buf[:], false)
		sink += parseGoroutineID(buf[:n])
	}
	_ = sink
}
