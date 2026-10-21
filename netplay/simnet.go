package netplay

import (
	"math/rand/v2"
	"time"
)

// NetConfig describes a simulated link between endpoints 0 and 1.
type NetConfig struct {
	Seed uint64
	// Delay is the fixed one-way delay: Delay[0] for packets sent by
	// endpoint 0, Delay[1] for packets sent by endpoint 1.
	Delay [2]time.Duration
	// Jitter adds a uniform random delay in [0, Jitter] to each packet.
	Jitter time.Duration
	// Loss is the probability, 0 to 1, that a packet is dropped.
	Loss float64
	// Reorder is the probability, 0 to 1, that a packet is held back by an
	// extra random delay of up to ReorderDelay (default 3 frames at 60 Hz).
	Reorder      float64
	ReorderDelay time.Duration
}

type simPacket struct {
	at   time.Duration
	data []byte
	n    int
}

// LossyNet is a seeded in-process network for tests and fuzzing. The caller
// owns the clock: call SetNow before each endpoint acts. The same seed and
// the same call sequence give the same deliveries.
type LossyNet struct {
	cfg  NetConfig
	rng  *rand.Rand
	now  time.Duration
	q    [2][]simPacket // q[i] holds packets sent by endpoint i, ordered by arrival
	free [][]byte
	// Counters, for assertions.
	Sent, Dropped, Delivered int
}

// NewLossyNet returns a network with the given conditions.
func NewLossyNet(cfg NetConfig) *LossyNet {
	if cfg.ReorderDelay == 0 {
		cfg.ReorderDelay = 50 * time.Millisecond
	}
	return &LossyNet{cfg: cfg, rng: rand.New(rand.NewPCG(cfg.Seed, 0x6e657470))}
}

// SetLoss changes the drop probability, for example 1 to cut the link.
func (n *LossyNet) SetLoss(p float64) { n.cfg.Loss = p }

// SetNow sets the network clock. Time must not go backwards.
func (n *LossyNet) SetNow(t time.Duration) { n.now = t }

// Endpoint returns the Transport for endpoint i (0 or 1).
func (n *LossyNet) Endpoint(i int) Transport { return &simEnd{net: n, id: i} }

type simEnd struct {
	net *LossyNet
	id  int
}

func (e *simEnd) Send(p []byte) error {
	n := e.net
	if len(p) > MaxPacketSize {
		return ErrPacketTooLarge
	}
	n.Sent++
	if n.cfg.Loss > 0 && n.rng.Float64() < n.cfg.Loss {
		n.Dropped++
		return nil
	}
	at := n.now + n.cfg.Delay[e.id]
	if n.cfg.Jitter > 0 {
		at += time.Duration(n.rng.Int64N(int64(n.cfg.Jitter) + 1))
	}
	if n.cfg.Reorder > 0 && n.rng.Float64() < n.cfg.Reorder {
		at += time.Duration(n.rng.Int64N(int64(n.cfg.ReorderDelay) + 1))
	}
	var buf []byte
	if k := len(n.free); k > 0 {
		buf, n.free = n.free[k-1], n.free[:k-1]
	} else {
		buf = make([]byte, MaxPacketSize)
	}
	pk := simPacket{at: at, data: buf, n: copy(buf, p)}
	q := n.q[e.id]
	i := len(q)
	q = append(q, pk)
	for i > 0 && q[i-1].at > at { // insertion keeps arrival order, stable
		q[i] = q[i-1]
		i--
	}
	q[i] = pk
	n.q[e.id] = q
	return nil
}

func (e *simEnd) Recv(buf []byte) (int, bool) {
	n := e.net
	q := n.q[1-e.id]
	if len(q) == 0 || q[0].at > n.now {
		return 0, false
	}
	pk := q[0]
	copy(q, q[1:])
	n.q[1-e.id] = q[:len(q)-1]
	m := copy(buf, pk.data[:pk.n])
	n.free = append(n.free, pk.data)
	n.Delivered++
	return m, true
}
