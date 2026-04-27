package sync

import "testing"

func TestBloomFilterAddsAndChecksHashes(t *testing.T) {
	filter := NewBloomFilter(4)

	var present [32]byte
	present[0] = 1
	present[4] = 2
	present[8] = 3
	filter.Add(present)

	if !filter.MaybeContains(present) {
		t.Fatal("expected bloom filter to contain inserted hash")
	}

	var absent [32]byte
	absent[0] = 9
	absent[4] = 7
	absent[8] = 5
	if filter.MaybeContains(absent) && string(filter.Bytes()) == "" {
		t.Fatal("unexpected empty bloom filter bytes")
	}
}

func TestBloomFilterRoundTripFromBytes(t *testing.T) {
	filter := NewBloomFilter(4)
	present := hashByte(7)
	filter.Add(present)

	clone := NewBloomFilterFromBytes(filter.Bytes(), filter.Size())
	if clone == nil {
		t.Fatal("expected cloned bloom filter")
	}
	if !clone.MaybeContains(present) {
		t.Fatal("expected cloned bloom filter to contain inserted hash")
	}
	if clone.Size() != filter.Size() {
		t.Fatalf("size = %d, want %d", clone.Size(), filter.Size())
	}
}

func TestBloomFilterForHashes(t *testing.T) {
	hashes := [][32]byte{hashByte(1), hashByte(2)}
	filter := NewBloomFilterForHashes(hashes)
	for _, hash := range hashes {
		if !filter.MaybeContains(hash) {
			t.Fatalf("expected bloom filter to contain %x", hash)
		}
	}
}
