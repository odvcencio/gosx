package bc7

// This file holds the fixed data of the BC7 format. Every table comes from the
// BC7 block decode rules, which appear in the Direct3D 11 "BC7 Format" section
// and in the Vulkan specification appendix "BC7". Both give the same tables.
//
// Transcribe nothing here from an encoder. A wrong partition entry or a wrong
// anchor entry produces a plausible image with a few bad texels per block,
// which no eyeball review catches. tables_test.go cross-checks the partition
// tables against the anchor tables, so a single-entry slip in either fails.

// pbitKind names how a mode spends its parity bits.
//
// A parity bit joins the stored endpoint value as a new least significant bit
// after quantization. So a mode with 7 colour bits and one parity bit reaches
// 8 bits of effective endpoint precision, not 7. The encoder and the decoder
// must agree on that, bit for bit.
type pbitKind uint8

const (
	// pbitNone means the mode stores no parity bit.
	pbitNone pbitKind = iota
	// pbitPerEndpoint gives every endpoint its own parity bit.
	pbitPerEndpoint
	// pbitShared gives every subset one parity bit, shared by its two
	// endpoints.
	pbitShared
)

// modeInfo describes one BC7 mode.
//
// The field order in the block is fixed and the same for every mode:
//
//  1. mode bits, in unary: mode m writes m zero bits and then one 1 bit
//  2. partition bits
//  3. rotation bits
//  4. index selection bit
//  5. colour endpoints, channel major: every R, then every G, then every B
//  6. alpha endpoints, when the mode stores alpha
//  7. parity bits
//  8. primary index set
//  9. secondary index set, when the mode has one
type modeInfo struct {
	// subsets counts the partitions the block splits into, 1 to 3.
	subsets int
	// partitionBits sizes the partition selector. Mode 0 uses 4 bits, so it
	// reaches only the first 16 three-subset partitions.
	partitionBits int
	// rotationBits sizes the channel rotation selector, modes 4 and 5 only.
	rotationBits int
	// indexSelBits sizes the index selection bit, mode 4 only.
	indexSelBits int
	// colorBits is the stored precision of one colour endpoint channel,
	// before any parity bit.
	colorBits int
	// alphaBits is the stored precision of one alpha endpoint, before any
	// parity bit. Zero means the mode stores no alpha and decodes 255.
	alphaBits int
	// pbit names the parity bit layout.
	pbit pbitKind
	// indexBits sizes the primary index set.
	indexBits int
	// indexBits2 sizes the secondary index set, or zero when there is none.
	indexBits2 int
}

// modes holds the eight BC7 modes in mode order.
//
// The row widths add to exactly 128 bits per mode. TestModeBitBudget proves
// it, because an off-by-one in this table silently truncates the index data.
var modes = [8]modeInfo{
	0: {subsets: 3, partitionBits: 4, colorBits: 4, pbit: pbitPerEndpoint, indexBits: 3},
	1: {subsets: 2, partitionBits: 6, colorBits: 6, pbit: pbitShared, indexBits: 3},
	2: {subsets: 3, partitionBits: 6, colorBits: 5, pbit: pbitNone, indexBits: 2},
	3: {subsets: 2, partitionBits: 6, colorBits: 7, pbit: pbitPerEndpoint, indexBits: 2},
	4: {subsets: 1, rotationBits: 2, indexSelBits: 1, colorBits: 5, alphaBits: 6, pbit: pbitNone, indexBits: 2, indexBits2: 3},
	5: {subsets: 1, rotationBits: 2, colorBits: 7, alphaBits: 8, pbit: pbitNone, indexBits: 2, indexBits2: 2},
	6: {subsets: 1, colorBits: 7, alphaBits: 7, pbit: pbitPerEndpoint, indexBits: 4},
	7: {subsets: 2, partitionBits: 6, colorBits: 5, alphaBits: 5, pbit: pbitPerEndpoint, indexBits: 2},
}

// bits returns the total block cost of one mode, which must be 128.
func (m modeInfo) bits(mode, partition int) int {
	total := mode + 1
	total += m.partitionBits + m.rotationBits + m.indexSelBits
	endpoints := m.subsets * 2
	total += endpoints * 3 * m.colorBits
	total += endpoints * m.alphaBits
	switch m.pbit {
	case pbitPerEndpoint:
		total += endpoints
	case pbitShared:
		total += m.subsets
	}
	total += 16*m.indexBits - m.subsets
	if m.indexBits2 > 0 {
		total += 16*m.indexBits2 - 1
	}
	_ = partition
	return total
}

// weights2, weights3 and weights4 hold the interpolation weights for the three
// index widths, on a 0 to 64 scale.
//
// Every table is symmetric about 32: weights[max-i] == 64-weights[i]. The
// encoder relies on that symmetry when it swaps an endpoint pair to satisfy the
// anchor rule, because the swap must not change the decoded colours.
// TestWeightTablesAreSymmetric asserts it.
var (
	weights2 = [4]uint32{0, 21, 43, 64}
	weights3 = [8]uint32{0, 9, 18, 27, 37, 46, 55, 64}
	weights4 = [16]uint32{0, 4, 9, 13, 17, 21, 26, 30, 34, 38, 43, 47, 51, 55, 60, 64}
)

// weightsFor returns the interpolation weights for an index width.
func weightsFor(bits int) []uint32 {
	switch bits {
	case 2:
		return weights2[:]
	case 3:
		return weights3[:]
	case 4:
		return weights4[:]
	}
	return nil
}

// partition2 assigns every texel of the block to subset 0 or 1, for each of
// the 64 two-subset partitions. Texel order is raster order inside the 4 by 4
// block.
var partition2 = [64][16]uint8{
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1},
	{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1},
	{0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 1, 1},
	{0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1},
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1, 1},
	{0, 1, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0},
	{0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0},
	{0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1},
	{0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0},
	{0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0},
	{0, 0, 1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1, 1, 0, 0},
	{0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 1, 1, 1, 0, 0, 0, 1, 1, 0, 0, 0, 1, 1, 1, 0},
	{0, 0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0, 0},
	{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1},
	{0, 1, 0, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0},
	{0, 0, 1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0},
	{0, 1, 0, 1, 0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 1, 0},
	{0, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0, 0, 1},
	{0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 1, 0, 0, 1, 0, 1},
	{0, 1, 1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 1, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 0, 0},
	{0, 1, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 1, 1, 0},
	{0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1},
	{0, 1, 1, 0, 0, 1, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1},
	{0, 0, 0, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 0, 0, 0},
	{0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0},
	{0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0},
	{0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0},
	{0, 1, 1, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 1, 1, 0, 1, 1, 0, 0, 1, 0, 0, 1},
	{0, 1, 1, 0, 0, 0, 1, 1, 1, 0, 0, 1, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 0, 0, 1, 1, 1, 0, 0, 0, 1, 1, 0},
	{0, 1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 1},
	{0, 1, 1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0, 0, 1},
	{0, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0, 0, 0, 0, 0, 1},
	{0, 0, 0, 1, 1, 0, 0, 0, 1, 1, 1, 0, 0, 1, 1, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0},
	{0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1},
}

// partition3 assigns every texel to subset 0, 1 or 2, for each of the 64
// three-subset partitions. Mode 0 reaches only rows 0 to 15, because it stores
// 4 partition bits. Mode 2 reaches all 64.
var partition3 = [64][16]uint8{
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 2, 2, 1, 2, 2, 2, 2},
	{0, 0, 0, 1, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2, 2, 1},
	{0, 0, 0, 0, 2, 0, 0, 1, 2, 2, 1, 1, 2, 2, 1, 1},
	{0, 2, 2, 2, 0, 0, 2, 2, 0, 0, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2},
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 2, 2, 0, 0, 2, 2},
	{0, 0, 2, 2, 0, 0, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2},
	{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2},
	{0, 1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2},
	{0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2, 2},
	{0, 0, 1, 1, 0, 1, 1, 2, 1, 1, 2, 2, 1, 2, 2, 2},
	{0, 0, 1, 1, 2, 0, 0, 1, 2, 2, 0, 0, 2, 2, 2, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 2, 1, 1, 2, 2},
	{0, 1, 1, 1, 0, 0, 1, 1, 2, 0, 0, 1, 2, 2, 0, 0},
	{0, 0, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2, 1, 1, 2, 2},
	{0, 0, 2, 2, 0, 0, 2, 2, 0, 0, 2, 2, 1, 1, 1, 1},
	{0, 1, 1, 1, 0, 1, 1, 1, 0, 2, 2, 2, 0, 2, 2, 2},
	{0, 0, 0, 1, 0, 0, 0, 1, 2, 2, 2, 1, 2, 2, 2, 1},
	{0, 0, 0, 0, 0, 0, 1, 1, 0, 1, 2, 2, 0, 1, 2, 2},
	{0, 0, 0, 0, 1, 1, 0, 0, 2, 2, 1, 0, 2, 2, 1, 0},
	{0, 1, 2, 2, 0, 1, 2, 2, 0, 0, 1, 1, 0, 0, 0, 0},
	{0, 0, 1, 2, 0, 0, 1, 2, 1, 1, 2, 2, 2, 2, 2, 2},
	{0, 1, 1, 0, 1, 2, 2, 1, 1, 2, 2, 1, 0, 1, 1, 0},
	{0, 0, 0, 0, 0, 1, 1, 0, 1, 2, 2, 1, 1, 2, 2, 1},
	{0, 0, 2, 2, 1, 1, 0, 2, 1, 1, 0, 2, 0, 0, 2, 2},
	{0, 1, 1, 0, 0, 1, 1, 0, 2, 0, 0, 2, 2, 2, 2, 2},
	{0, 0, 1, 1, 0, 1, 2, 2, 0, 1, 2, 2, 0, 0, 1, 1},
	{0, 0, 0, 0, 2, 0, 0, 0, 2, 2, 1, 1, 2, 2, 2, 1},
	{0, 0, 0, 0, 0, 0, 0, 2, 1, 1, 2, 2, 1, 2, 2, 2},
	{0, 2, 2, 2, 0, 0, 2, 2, 0, 0, 1, 2, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 2, 0, 0, 2, 2, 0, 2, 2, 2},
	{0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2, 0},
	{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 0, 0, 0, 0},
	{0, 1, 2, 0, 1, 2, 0, 1, 2, 0, 1, 2, 0, 1, 2, 0},
	{0, 1, 2, 0, 2, 0, 1, 2, 1, 2, 0, 1, 0, 1, 2, 0},
	{0, 0, 1, 1, 2, 2, 0, 0, 1, 1, 2, 2, 0, 0, 1, 1},
	{0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 0, 0, 0, 0, 1, 1},
	{0, 1, 0, 1, 0, 1, 0, 1, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 0, 0, 0, 0, 0, 0, 0, 2, 1, 2, 1, 2, 1, 2, 1},
	{0, 0, 2, 2, 1, 1, 2, 2, 0, 0, 2, 2, 1, 1, 2, 2},
	{0, 0, 2, 2, 0, 0, 1, 1, 0, 0, 2, 2, 0, 0, 1, 1},
	{0, 2, 2, 0, 1, 2, 2, 1, 0, 2, 2, 0, 1, 2, 2, 1},
	{0, 1, 0, 1, 2, 2, 2, 2, 2, 2, 2, 2, 0, 1, 0, 1},
	{0, 0, 0, 0, 2, 1, 2, 1, 2, 1, 2, 1, 2, 1, 2, 1},
	{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 2, 2, 2, 2},
	{0, 2, 2, 2, 0, 1, 1, 1, 0, 2, 2, 2, 0, 1, 1, 1},
	{0, 0, 0, 2, 1, 1, 1, 2, 0, 0, 0, 2, 1, 1, 1, 2},
	{0, 0, 0, 0, 2, 1, 1, 2, 2, 1, 1, 2, 2, 1, 1, 2},
	{0, 2, 2, 2, 0, 1, 1, 1, 0, 1, 1, 1, 0, 2, 2, 2},
	{0, 0, 0, 2, 1, 1, 1, 2, 1, 1, 1, 2, 0, 0, 0, 2},
	{0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 2, 2, 2, 2},
	{0, 0, 0, 0, 0, 0, 0, 0, 2, 1, 1, 2, 2, 1, 1, 2},
	{0, 1, 1, 0, 0, 1, 1, 0, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 0, 2, 2, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 2, 2},
	{0, 0, 2, 2, 1, 1, 2, 2, 1, 1, 2, 2, 0, 0, 2, 2},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 1, 1, 2},
	{0, 0, 0, 2, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 1},
	{0, 2, 2, 2, 1, 2, 2, 2, 0, 2, 2, 2, 1, 2, 2, 2},
	{0, 1, 0, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 1, 1, 1, 2, 0, 1, 1, 2, 2, 0, 1, 2, 2, 2, 0},
}

// anchor2 holds the anchor texel of subset 1, for each two-subset partition.
//
// The anchor texel of a subset stores one fewer index bit, because its high bit
// is defined to be zero. Subset 0 always anchors at texel 0. Subsets 1 and 2
// anchor where these tables say, which is not the first texel of the subset.
// Partition 0 proves it: subset 1 starts at texel 2, but its anchor is texel 15.
var anchor2 = [64]uint8{
	15, 15, 15, 15, 15, 15, 15, 15,
	15, 15, 15, 15, 15, 15, 15, 15,
	15, 2, 8, 2, 2, 8, 8, 15,
	2, 8, 2, 2, 8, 8, 2, 2,
	15, 15, 6, 8, 2, 8, 15, 15,
	2, 8, 2, 2, 2, 15, 15, 6,
	6, 2, 6, 8, 15, 15, 2, 2,
	15, 15, 15, 15, 15, 2, 2, 15,
}

// anchor3a holds the anchor texel of subset 1, for each three-subset partition.
var anchor3a = [64]uint8{
	3, 3, 15, 15, 8, 3, 15, 15,
	8, 8, 6, 6, 6, 5, 3, 3,
	3, 3, 8, 15, 3, 3, 6, 10,
	5, 8, 8, 6, 8, 5, 15, 15,
	8, 15, 3, 5, 6, 10, 8, 15,
	15, 3, 15, 5, 15, 15, 15, 15,
	3, 15, 5, 5, 5, 8, 5, 10,
	5, 10, 8, 13, 15, 12, 3, 3,
}

// anchor3b holds the anchor texel of subset 2, for each three-subset partition.
var anchor3b = [64]uint8{
	15, 8, 8, 3, 15, 15, 3, 8,
	15, 15, 15, 15, 15, 15, 15, 8,
	15, 8, 15, 3, 15, 8, 15, 8,
	3, 15, 6, 10, 15, 15, 10, 8,
	15, 3, 15, 10, 10, 8, 9, 10,
	6, 15, 8, 15, 3, 6, 6, 8,
	15, 3, 15, 15, 15, 15, 15, 15,
	15, 15, 15, 15, 3, 15, 15, 8,
}

// partitionTable returns the texel to subset map for one partition.
func partitionTable(subsets, partition int) *[16]uint8 {
	if subsets == 3 {
		return &partition3[partition]
	}
	return &partition2[partition]
}

// anchorTexel returns the anchor texel of one subset.
func anchorTexel(subsets, partition, subset int) int {
	if subset == 0 {
		return 0
	}
	if subsets == 2 {
		return int(anchor2[partition])
	}
	if subset == 1 {
		return int(anchor3a[partition])
	}
	return int(anchor3b[partition])
}

// isAnchor reports whether one texel anchors its subset.
func isAnchor(subsets, partition, texel int) bool {
	if texel == 0 {
		return true
	}
	switch subsets {
	case 2:
		return texel == int(anchor2[partition])
	case 3:
		return texel == int(anchor3a[partition]) || texel == int(anchor3b[partition])
	}
	return false
}

// unquantize widens a stored endpoint value to 8 bits.
//
// The rule replicates the high bits into the low bits, so the full-scale stored
// value maps to 255 and zero maps to 0. A plain left shift would cap the
// brightest endpoint below white.
func unquantize(value uint32, bits int) uint32 {
	if bits >= 8 {
		return value & 0xFF
	}
	value <<= uint(8 - bits)
	return value | value>>uint(bits)
}

// interpolate blends two 8-bit endpoints with a 0 to 64 weight.
func interpolate(e0, e1, weight uint32) uint8 {
	return uint8(((64-weight)*e0 + weight*e1 + 32) >> 6)
}
