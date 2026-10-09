package budget

import (
	"errors"
	"math/big"
)

const maxSearchBytes int64 = 64 << 20

func ratio(n, d int64) *big.Rat { return new(big.Rat).SetFrac(big.NewInt(n), big.NewInt(d)) }

// transferMicros charges ACK-limited flights and serializes the final flight.
// The planning connection excludes setup and first-byte delays, charged by caller.
func transferMicros(bytes int64, network Network, initial int64) (elapsed, slowStart *big.Rat, err error) {
	if bytes < 0 || bytes > maxSearchBytes || network.DownBytesPerSec <= 0 || network.RTTMicros < 0 || initial <= 0 || initial > maxSearchBytes {
		return nil, nil, errors.New("invalid transfer input")
	}
	slowStart = new(big.Rat)
	bdp := new(big.Rat).Mul(ratio(network.DownBytesPerSec, 1000000), ratio(network.RTTMicros, 1))
	delivered, cwnd := int64(0), initial
	for delivered < bytes {
		remaining := bytes - delivered
		if ratio(cwnd, 1).Cmp(bdp) >= 0 || remaining <= cwnd {
			return new(big.Rat).Add(slowStart, new(big.Rat).Mul(ratio(remaining, network.DownBytesPerSec), ratio(1000000, 1))), slowStart, nil
		}
		slowStart.Add(slowStart, ratio(network.RTTMicros, 1))
		delivered += cwnd
		cwnd *= 2 // Bounded search stops before this can approach int64 overflow.
	}
	return new(big.Rat).Set(slowStart), slowStart, nil
}

type planningModel struct {
	network                          Network
	initial, quantum, reserve, share int64
	static                           bool
	window, slope, fixed             *big.Rat
	status, fingerprint              string
}

func (m planningModel) cost(n int64) (*big.Rat, error) {
	transfer, _, err := transferMicros(n, m.network, m.initial)
	if err != nil {
		return nil, err
	}
	cpu := new(big.Rat).Add(new(big.Rat).Mul(ratio(n, 1), m.slope), m.fixed)
	return transfer.Add(transfer, cpu), nil
}

func (m planningModel) solve() (int64, *big.Rat, error) {
	if m.window.Sign() <= 0 || m.fixed.Cmp(m.window) > 0 {
		return 0, nil, errors.New("insufficient residual time")
	}
	limitCost, err := m.cost(maxSearchBytes)
	if err != nil {
		return 0, nil, err
	}
	if limitCost.Cmp(m.window) <= 0 {
		return 0, nil, errors.New("allocation reaches search limit")
	}
	lo, hi := int64(0), maxSearchBytes
	for lo+1 < hi {
		mid := lo + (hi-lo)/2
		cost, err := m.cost(mid)
		if err != nil {
			return 0, nil, err
		}
		if cost.Cmp(m.window) <= 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	// Recover the exact solution inside the selected linear segment. At an ACK
	// discontinuity, the preceding flight boundary is the largest feasible value.
	_, phase, _ := transferMicros(lo, m.network, m.initial)
	_, nextPhase, _ := transferMicros(lo+1, m.network, m.initial)
	unrounded := ratio(lo, 1)
	if phase.Cmp(nextPhase) == 0 {
		cost, _ := m.cost(lo)
		gap := new(big.Rat).Sub(m.window, cost)
		slope := new(big.Rat).Add(ratio(1000000, m.network.DownBytesPerSec), m.slope)
		unrounded.Add(unrounded, gap.Quo(gap, slope))
	}
	return lo / m.quantum * m.quantum, unrounded, nil
}

func step(name, unit string, value *big.Rat) Step {
	return Step{Name: name, Unit: unit, Numerator: value.Num().String(), Denominator: value.Denom().String()}
}

func (m planningModel) allocation() (Derivation, error) {
	n, unrounded, err := m.solve()
	if err != nil {
		return Derivation{}, err
	}
	pool := n - m.reserve
	if pool < 0 {
		return Derivation{}, errors.New("critical reserve exceeds allocation")
	}
	minimum := pool/1000000*m.share + (pool%1000000*m.share+999999)/1000000
	minimum = (minimum + m.quantum - 1) / m.quantum * m.quantum
	if m.static {
		minimum = pool
	}
	framework := pool - minimum
	if framework < 0 {
		return Derivation{}, errors.New("app minimum exceeds allocation")
	}
	transfer, slow, _ := transferMicros(n, m.network, m.initial)
	cpu := new(big.Rat).Add(new(big.Rat).Mul(ratio(n, 1), m.slope), m.fixed)
	return Derivation{Kind: "transfer-cpu", Status: m.status, InputSHA256: m.fingerprint,
		TotalBytes: n, AppCriticalReserveBytes: m.reserve, MinAppBytes: minimum, FrameworkBytes: framework,
		Steps: []Step{step("window", "us", m.window), step("slow-start", "us", slow), step("transfer", "us", transfer),
			step("cpu", "us", cpu), step("fixed", "us", m.fixed), step("unrounded-total", "B", unrounded),
			step("total", "B", ratio(n, 1)), step("app-reserve", "B", ratio(m.reserve, 1)),
			step("app-minimum", "B", ratio(minimum, 1)), step("framework", "B", ratio(framework, 1))}}, nil
}
