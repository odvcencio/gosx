package bc7

// blockSpec holds everything one block stores, before bit packing.
type blockSpec struct {
	// mode selects the BC7 mode, 0 to 7.
	mode int
	// partition selects the partition, for a multi-subset mode.
	partition int
	// rotation selects the alpha channel swap, for mode 4 and mode 5.
	rotation int
	// indexSel swaps the two index sets, for mode 4.
	indexSel int
	// stored holds the endpoint values as the block stores them, before any
	// parity bit. Endpoint 2*s is the first endpoint of subset s.
	stored [6][4]uint32
	// parity holds one bit per endpoint, or one bit per subset for a shared
	// parity mode. Entries the mode does not use stay -1.
	parity [6]int8
	// idx holds the primary index set, one entry per texel.
	idx [16]uint8
	// idx2 holds the secondary index set, for mode 4 and mode 5.
	idx2 [16]uint8
}

// packBlock writes one block spec as 16 bytes.
//
// The field order is the one modeInfo documents, and it is the only order a
// conforming decoder reads. packBlock does not validate the spec, so a caller
// that hands it an index above the field width silently loses the high bits.
// TestPackConsumesExactlyOneBlock proves the total is 128 bits for every mode.
func packBlock(s *blockSpec) [BlockBytes]byte {
	var w bitWriter
	packInto(&w, s)
	return w.buf
}

// packInto writes one block spec into a bit writer and leaves the cursor at the
// end, so a caller can check the bit total.
func packInto(w *bitWriter, s *blockSpec) {
	m := modes[s.mode]

	// The mode is unary: s.mode zero bits, then one 1 bit.
	w.put(1<<uint(s.mode), s.mode+1)

	if m.partitionBits > 0 {
		w.put(uint32(s.partition), m.partitionBits)
	}
	if m.rotationBits > 0 {
		w.put(uint32(s.rotation), m.rotationBits)
	}
	if m.indexSelBits > 0 {
		w.put(uint32(s.indexSel), m.indexSelBits)
	}

	count := m.subsets * 2
	for c := 0; c < 3; c++ {
		for e := 0; e < count; e++ {
			w.put(s.stored[e][c], m.colorBits)
		}
	}
	if m.alphaBits > 0 {
		for e := 0; e < count; e++ {
			w.put(s.stored[e][3], m.alphaBits)
		}
	}

	switch m.pbit {
	case pbitPerEndpoint:
		for e := 0; e < count; e++ {
			w.put(uint32(s.parity[e]&1), 1)
		}
	case pbitShared:
		for sub := 0; sub < m.subsets; sub++ {
			w.put(uint32(s.parity[sub*2]&1), 1)
		}
	}

	for t := 0; t < 16; t++ {
		n := m.indexBits
		if isAnchor(m.subsets, s.partition, t) {
			n--
		}
		w.put(uint32(s.idx[t]), n)
	}
	if m.indexBits2 > 0 {
		for t := 0; t < 16; t++ {
			n := m.indexBits2
			if t == 0 {
				n--
			}
			w.put(uint32(s.idx2[t]), n)
		}
	}
}

// packedBits returns how many bits packBlock writes for one mode and partition.
// A test compares it with blockBits, so a field width slip fails loudly.
func packedBits(mode, partition int) int {
	var w bitWriter
	spec := blockSpec{mode: mode, partition: partition}
	packInto(&w, &spec)
	return w.pos
}
