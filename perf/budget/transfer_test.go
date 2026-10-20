package budget

import (
	"sort"
	"testing"
)

func TestTransferPartialFinalFlight(t *testing.T) {
	a, _, err := transferMicros(20000, Network{DownBytesPerSec: 194000, RTTMicros: 150000}, 14600)
	if err != nil {
		t.Fatal(err)
	}
	b, slow, err := transferMicros(20000, Network{DownBytesPerSec: 200000, RTTMicros: 150000}, 14600)
	if err != nil {
		t.Fatal(err)
	}
	if a.Cmp(ratio(17250000, 97)) != 0 || b.Cmp(ratio(177000, 1)) != 0 || slow.Cmp(ratio(150000, 1)) != 0 || b.Cmp(a) > 0 {
		t.Fatal("partial flight waited for an ACK", a, b, slow)
	}
}

func TestTransferMonotoneBytesAndBandwidth(t *testing.T) {
	// Probe both sides of each flight end and congestion-window/BDP handoff.
	for _, rtt := range []int64{0, 100000, 150000} {
		for cwnd := int64(14600); cwnd < maxSearchBytes; cwnd *= 2 {
			divisor := rtt
			if divisor == 0 {
				divisor = 150000
			}
			bandwidth := cwnd * 1000000 / divisor
			for _, b := range []int64{bandwidth - 1, bandwidth, bandwidth + 1, 1125000} {
				previous := ratio(0, 1)
				points := []int64{0, 1, 14600, 14601, 43800, 43801, 102200, 102201, 1000000, maxSearchBytes}
				end := 2*cwnd - 14600
				if end < maxSearchBytes {
					points = append(points, end-1, end, end+1)
				}
				sort.Slice(points, func(i, j int) bool { return points[i] < points[j] })
				for _, n := range points {
					current, _, err := transferMicros(n, Network{DownBytesPerSec: b, RTTMicros: rtt}, 14600)
					if err != nil || current.Cmp(previous) < 0 {
						t.Fatal("nonmonotone byte cost", n, b, err)
					}
					faster, _, err := transferMicros(n, Network{DownBytesPerSec: b + 1, RTTMicros: rtt}, 14600)
					if err != nil || faster.Cmp(current) > 0 {
						t.Fatal("faster network increased cost", n, b, err)
					}
					previous = current
				}
			}
		}
	}
}

func TestTransferSolverACKBoundary(t *testing.T) {
	m := planningModel{network: Network{DownBytesPerSec: 194000, RTTMicros: 150000}, initial: 14600, quantum: 1,
		share: 500000, static: true, window: ratio(100000, 1), slope: ratio(0, 1), fixed: ratio(0, 1)}
	n, exact, err := m.solve()
	if err != nil || n != 14600 || exact.Cmp(ratio(14600, 1)) != 0 {
		t.Fatal("solver crossed an ACK discontinuity", n, exact, err)
	}
}

func TestDeriveMonotoneModelInputs(t *testing.T) {
	f, p, c := workedFile(t)
	for name := range f.PageTypes {
		m, err := newModel(f, p, c, name, false)
		if err != nil {
			t.Fatal(err)
		}
		base, _, err := m.solve()
		if err != nil {
			t.Fatal(err)
		}
		moreTime := m
		moreTime.window = ratio(100000, 1).Add(m.window, ratio(100000, 1))
		moreCost := m
		moreCost.fixed = ratio(100000, 1).Add(m.fixed, ratio(100000, 1))
		faster := m
		faster.network.DownBytesPerSec += 100000
		a, _, ea := moreTime.solve()
		b, _, eb := moreCost.solve()
		d, _, ed := faster.solve()
		if ea != nil || eb != nil || ed != nil || a < base || b > base || d < base {
			t.Fatal("nonmonotone allocation", name, a, b, d, base, ea, eb, ed)
		}
	}
}

func TestTransferRejectInvalidInputs(t *testing.T) {
	for _, n := range []int64{-1, maxSearchBytes + 1} {
		if _, _, err := transferMicros(n, Network{DownBytesPerSec: 1}, 14600); err == nil {
			t.Fatal("invalid bytes accepted")
		}
	}
	for _, network := range []Network{{DownBytesPerSec: 0}, {DownBytesPerSec: 1, RTTMicros: -1}} {
		if _, _, err := transferMicros(1, network, 14600); err == nil {
			t.Fatal("invalid network accepted")
		}
	}
}
