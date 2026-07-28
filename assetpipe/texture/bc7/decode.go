package bc7

// This file holds the reference decoder. It follows the BC7 decode rules step
// for step, in the order the specification states them, and it takes no
// shortcut that the encoder could share. Keep it that way. A decoder that
// reuses encoder helpers proves only that the pair agrees with itself.
//
// The steps are:
//
//  1. read the mode in unary
//  2. read the partition, the rotation and the index selection bit
//  3. read the endpoints, channel major
//  4. append the parity bits to the endpoints
//  5. widen every endpoint to 8 bits
//  6. read the indices, one bit shorter at every anchor texel
//  7. interpolate, then undo the channel rotation

// DecodeBlock expands one 16 byte BC7 block to 16 RGBA texels.
//
// Texel order is raster order inside the 4 by 4 block, so texel 4 sits below
// texel 0. The result carries 8-bit codes in the space the encoder stored, so a
// block written for an sRGB VkFormat decodes to sRGB codes.
//
// A block whose first 8 bits are all zero selects no mode. The specification
// calls that reserved and requires a decode of zero in every channel,
// including alpha. DecodeBlock does exactly that rather than guessing a mode.
func DecodeBlock(block []byte) [16][4]uint8 {
	var out [16][4]uint8
	if len(block) < BlockBytes {
		return out
	}
	r := bitReader{buf: block[:BlockBytes]}

	mode := 0
	for mode < 8 && r.get(1) == 0 {
		mode++
	}
	if mode >= 8 {
		return out
	}
	m := modes[mode]

	partition := 0
	if m.partitionBits > 0 {
		partition = int(r.get(m.partitionBits))
	}
	rotation := 0
	if m.rotationBits > 0 {
		rotation = int(r.get(m.rotationBits))
	}
	indexSel := 0
	if m.indexSelBits > 0 {
		indexSel = int(r.get(m.indexSelBits))
	}

	// Endpoints arrive channel major: every red, then every green, then every
	// blue, then every alpha. Inside one channel the order is subset 0
	// endpoint 0, subset 0 endpoint 1, subset 1 endpoint 0, and so on.
	count := m.subsets * 2
	var ep [6][4]uint32
	for c := 0; c < 3; c++ {
		for e := 0; e < count; e++ {
			ep[e][c] = r.get(m.colorBits)
		}
	}
	if m.alphaBits > 0 {
		for e := 0; e < count; e++ {
			ep[e][3] = r.get(m.alphaBits)
		}
	}

	// A parity bit joins the endpoint as a new least significant bit, which
	// raises the effective precision by one. Read the parity bits after every
	// endpoint value, never before.
	chans := 3
	if m.alphaBits > 0 {
		chans = 4
	}
	colorPrec := m.colorBits
	alphaPrec := m.alphaBits
	switch m.pbit {
	case pbitPerEndpoint:
		for e := 0; e < count; e++ {
			p := r.get(1)
			for c := 0; c < chans; c++ {
				ep[e][c] = ep[e][c]<<1 | p
			}
		}
		colorPrec++
		if m.alphaBits > 0 {
			alphaPrec++
		}
	case pbitShared:
		for s := 0; s < m.subsets; s++ {
			p := r.get(1)
			for k := 0; k < 2; k++ {
				e := s*2 + k
				for c := 0; c < chans; c++ {
					ep[e][c] = ep[e][c]<<1 | p
				}
			}
		}
		colorPrec++
		if m.alphaBits > 0 {
			alphaPrec++
		}
	}

	for e := 0; e < count; e++ {
		for c := 0; c < 3; c++ {
			ep[e][c] = unquantize(ep[e][c], colorPrec)
		}
		if m.alphaBits > 0 {
			ep[e][3] = unquantize(ep[e][3], alphaPrec)
		} else {
			ep[e][3] = 255
		}
	}

	// Indices come in raster order. The anchor texel of every subset stores one
	// bit fewer, because its high bit is defined to be zero.
	var idx1, idx2 [16]uint32
	for t := 0; t < 16; t++ {
		n := m.indexBits
		if isAnchor(m.subsets, partition, t) {
			n--
		}
		idx1[t] = r.get(n)
	}
	if m.indexBits2 > 0 {
		// A mode with two index sets has one subset, so texel 0 is the only
		// anchor of the second set.
		for t := 0; t < 16; t++ {
			n := m.indexBits2
			if t == 0 {
				n--
			}
			idx2[t] = r.get(n)
		}
	}

	table := partitionTable(m.subsets, partition)
	w1 := weightsFor(m.indexBits)
	w2 := weightsFor(m.indexBits2)

	for t := 0; t < 16; t++ {
		subset := 0
		if m.subsets > 1 {
			subset = int(table[t])
		}
		e0 := &ep[subset*2]
		e1 := &ep[subset*2+1]

		// Pick which index set drives colour and which drives alpha. The index
		// selection bit of mode 4 swaps the two, so the 3-bit set can serve the
		// channel that needs the finer ramp.
		colorWeight := w1[idx1[t]]
		alphaWeight := colorWeight
		if m.indexBits2 > 0 {
			if indexSel == 0 {
				alphaWeight = w2[idx2[t]]
			} else {
				colorWeight = w2[idx2[t]]
				alphaWeight = w1[idx1[t]]
			}
		}

		var px [4]uint8
		for c := 0; c < 3; c++ {
			px[c] = interpolate(e0[c], e1[c], colorWeight)
		}
		px[3] = interpolate(e0[3], e1[3], alphaWeight)

		// The rotation swaps alpha with one colour channel after
		// interpolation. The encoder applies the same swap to its input, and a
		// swap is its own inverse.
		switch rotation {
		case 1:
			px[0], px[3] = px[3], px[0]
		case 2:
			px[1], px[3] = px[3], px[1]
		case 3:
			px[2], px[3] = px[3], px[2]
		}
		out[t] = px
	}
	return out
}

// BlockMode returns the mode a block selects, or -1 for the reserved encoding
// that sets no mode bit. Use it to audit a mode histogram.
func BlockMode(block []byte) int {
	if len(block) < BlockBytes {
		return -1
	}
	r := bitReader{buf: block[:BlockBytes]}
	mode := 0
	for mode < 8 && r.get(1) == 0 {
		mode++
	}
	if mode >= 8 {
		return -1
	}
	return mode
}
