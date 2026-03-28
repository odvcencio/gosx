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
