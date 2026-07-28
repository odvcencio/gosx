package bc7

// This file turns an 8-bit target into the stored endpoint value a mode can
// hold, and back again. Every mode quantizes to a coarser grid than 8 bits, and
// a parity bit pins the least significant bit of the widened value. So the
// reachable set of 8-bit endpoint values depends on both the precision and the
// parity bit, and the encoder must search inside that set, not around it.

// maxPrec bounds the endpoint precision the tables cover. Mode 5 stores 8-bit
// alpha, which is the widest field in the format.
const maxPrec = 8

// quantPlain[total][target] holds the stored value in 0 to 2^total-1 whose
// widened 8-bit value sits nearest to target. Use it when the mode stores no
// parity bit.
var quantPlain [maxPrec + 1][256]uint8

// quantParity[total][parity][target] holds the high part of the stored value,
// in 0 to 2^(total-1)-1, whose widened 8-bit value sits nearest to target when
// the given parity bit joins it as the low bit.
var quantParity [maxPrec + 1][2][256]uint8

func init() {
	for total := 2; total <= maxPrec; total++ {
		buildNearest(total, func(v uint32) uint32 { return v }, 1<<uint(total), &quantPlain[total])
		for p := uint32(0); p < 2; p++ {
			parity := p
			buildNearest(total, func(v uint32) uint32 { return v<<1 | parity },
				1<<uint(total-1), &quantParity[total][p])
		}
	}
}

// buildNearest fills one nearest-value table by brute force over the reachable
// stored values. Brute force is exact, and it runs once for a handful of tables.
func buildNearest(total int, store func(uint32) uint32, count int, out *[256]uint8) {
	for target := 0; target < 256; target++ {
		best, bestErr := 0, 1<<30
		for v := 0; v < count; v++ {
			got := int(unquantize(store(uint32(v)), total))
			d := got - target
			if d < 0 {
				d = -d
			}
			if d < bestErr {
				best, bestErr = v, d
				if d == 0 {
					break
				}
			}
		}
		out[target] = uint8(best)
	}
}

// quantizeEndpoint picks the stored value nearest to a real-valued target.
//
// prec is the stored precision before any parity bit. parity is 0 or 1 when the
// mode appends a parity bit, and negative when it does not.
func quantizeEndpoint(target float64, prec int, parity int8) uint32 {
	t := int(target + 0.5)
	if t < 0 {
		t = 0
	}
	if t > 255 {
		t = 255
	}
	if parity < 0 {
		return uint32(quantPlain[prec][t])
	}
	return uint32(quantParity[prec+1][parity][t])
}

// widenEndpoint returns the 8-bit value a decoder will read back.
func widenEndpoint(stored uint32, prec int, parity int8) uint32 {
	if parity < 0 {
		return unquantize(stored, prec)
	}
	return unquantize(stored<<1|uint32(parity), prec+1)
}

// nearestWeightRes samples the 0 to 1 position along an endpoint line. The
// tables below map a sampled position to the index whose weight sits nearest.
//
// A resolution of 1024 steps is finer than one part in a thousand. The closest
// pair of 4-bit weights sits 4 steps apart on the 0 to 64 scale, which is 1 part
// in 16, so the table only ever mis-picks at an exact tie.
const nearestWeightRes = 1024

// nearestWeight[bits][position] holds the index whose weight sits nearest to
// position/nearestWeightRes of the way from endpoint 0 to endpoint 1.
var nearestWeight [5][nearestWeightRes + 1]uint8

func init() {
	for bits := 2; bits <= 4; bits++ {
		w := weightsFor(bits)
		for p := 0; p <= nearestWeightRes; p++ {
			pos := float64(p) * 64 / nearestWeightRes
			best, bestErr := 0, 1e30
			for k, wk := range w {
				d := pos - float64(wk)
				if d < 0 {
					d = -d
				}
				if d < bestErr {
					best, bestErr = k, d
				}
			}
			nearestWeight[bits][p] = uint8(best)
		}
	}
}
