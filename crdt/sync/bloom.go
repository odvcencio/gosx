package sync

import "encoding/binary"

// BloomFilter implements the Automerge V1 bloom parameters used by sync state.
type BloomFilter struct {
	bits []byte
	size uint32
}

func NewBloomFilter(entries int) *BloomFilter {
	if entries <= 0 {
		entries = 1
	}
	size := uint32(entries * 10)
	bytes := int((size + 7) / 8)
	return &BloomFilter{
		bits: make([]byte, bytes),
		size: size,
	}
}

func NewBloomFilterFromBytes(bits []byte, size uint32) *BloomFilter {
	if len(bits) == 0 || size == 0 {
		return nil
	}
	maxSize := uint32(len(bits) * 8)
	if size > maxSize {
		size = maxSize
	}
	return &BloomFilter{
		bits: append([]byte(nil), bits...),
		size: size,
	}
}

func NewBloomFilterForHashes(hashes [][32]byte) *BloomFilter {
	filter := NewBloomFilter(len(hashes))
	for _, hash := range hashes {
		filter.Add(hash)
	}
	return filter
}

func (f *BloomFilter) Add(hash [32]byte) {
	if f == nil || f.size == 0 {
		return
	}
	for _, probe := range f.probes(hash) {
		byteIndex := probe / 8
		bitIndex := probe % 8
		f.bits[byteIndex] |= 1 << bitIndex
	}
}

func (f *BloomFilter) MaybeContains(hash [32]byte) bool {
	if f == nil || f.size == 0 {
		return false
	}
	for _, probe := range f.probes(hash) {
		byteIndex := probe / 8
		bitIndex := probe % 8
		if f.bits[byteIndex]&(1<<bitIndex) == 0 {
			return false
		}
	}
	return true
}

func (f *BloomFilter) Bytes() []byte {
	if f == nil {
		return nil
	}
	return append([]byte(nil), f.bits...)
}

func (f *BloomFilter) Size() uint32 {
	if f == nil {
		return 0
	}
	return f.size
}

func (f *BloomFilter) probes(hash [32]byte) [7]uint32 {
	modulo := f.size
	x := binary.LittleEndian.Uint32(hash[0:4]) % modulo
	y := binary.LittleEndian.Uint32(hash[4:8]) % modulo
	z := binary.LittleEndian.Uint32(hash[8:12]) % modulo

	var out [7]uint32
	out[0] = x
	for i := 1; i < len(out); i++ {
		x = (x + y) % modulo
		y = (y + z) % modulo
		out[i] = x
	}
	return out
}
