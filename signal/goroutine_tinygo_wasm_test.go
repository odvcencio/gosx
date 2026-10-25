//go:build tinygo && wasm

package signal

import (
	"runtime"
	"testing"
)

func TestTinyGoSharedScopeMatchesRuntimeIdentity(t *testing.T) {
	var header [32]byte
	if n := runtime.Stack(header[:], false); n != 0 {
		t.Fatal("TinyGo now emits a stack header; revisit shared-scope identity specialization")
	}
	if goroutineID() != 0 {
		t.Fatal("TinyGo browser reactive scope must be shared")
	}
}
