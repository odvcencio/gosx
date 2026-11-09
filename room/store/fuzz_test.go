package store

import (
	"bytes"
	"testing"
)

// FuzzScanLog feeds arbitrary bytes to the WAL decoder. It must never panic,
// must report a valid prefix no longer than the input, and must decode that
// prefix to the same frames again.
func FuzzScanLog(f *testing.F) {
	f.Add([]byte{})
	f.Add(appendFrame(nil, []op{{kind: opPut, key: "k", val: []byte("v")}}))
	two := appendFrame(nil, []op{{kind: opAppend, key: "l", val: []byte("x")}, {kind: opDel, key: "k"}})
	two = appendFrame(two, []op{{kind: opLogHead, key: "l", val: []byte{3}}})
	f.Add(two)
	f.Add(two[:len(two)-2])
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		st := newState(100)
		valid, frames, err := scanLog(data, func(ops []op) error { st.applyAll(ops); return nil })
		if err != nil {
			t.Fatal(err)
		}
		if valid < 0 || valid > len(data) {
			t.Fatalf("valid %d outside 0..%d", valid, len(data))
		}
		// Re-encoding what was decoded and decoding again is stable.
		var again []byte
		n := 0
		_, _, _ = scanLog(data[:valid], func(ops []op) error {
			again = appendFrame(again, ops)
			n++
			return nil
		})
		if n != frames || !bytes.Equal(again, data[:valid]) {
			t.Fatalf("prefix does not round-trip: %d vs %d frames", n, frames)
		}
		if st.bytes < 0 {
			t.Fatalf("negative byte count %d", st.bytes)
		}
	})
}

// FuzzLoadSnapshot checks the snapshot loader rejects or accepts without panic.
func FuzzLoadSnapshot(f *testing.F) {
	f.Add([]byte(snapMagic))
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = loadSnapshot(data, newState(10))
	})
}
