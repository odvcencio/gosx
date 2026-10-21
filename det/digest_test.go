package det

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"math"
)

type digest struct{ h hash.Hash }

func newDigest() *digest { return &digest{sha256.New()} }

func (d *digest) add(f float64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(f))
	d.h.Write(b[:])
}

func (d *digest) sum() string { return hex.EncodeToString(d.h.Sum(nil)) }
