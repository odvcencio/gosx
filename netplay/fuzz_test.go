package netplay

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fuzzConditions draws the network from the section 7 envelope: one-way delay
// 0-125 ms per direction, jitter up to 20 ms, loss 0-20%, reordering 0-5%.
// The first seeds sit at the extremes.
func fuzzConditions(seed uint64) (NetConfig, [2]int, time.Duration) {
	r := rand.New(rand.NewPCG(seed, 99))
	c := NetConfig{Seed: seed}
	switch seed % 100 {
	case 0: // worst case on every axis
		c.Delay = [2]time.Duration{125 * time.Millisecond, 125 * time.Millisecond}
		c.Jitter, c.Loss, c.Reorder = 20*time.Millisecond, 0.20, 0.05
	case 1: // asymmetric
		c.Delay = [2]time.Duration{125 * time.Millisecond, 0}
		c.Jitter, c.Loss, c.Reorder = 20*time.Millisecond, 0.20, 0.05
	default:
		c.Delay = [2]time.Duration{
			time.Duration(r.Int64N(int64(125*time.Millisecond) + 1)),
			time.Duration(r.Int64N(int64(125*time.Millisecond) + 1)),
		}
		c.Jitter = time.Duration(r.Int64N(int64(20*time.Millisecond) + 1))
		c.Loss = r.Float64() * 0.20
		c.Reorder = r.Float64() * 0.05
	}
	delay := [2]int{r.IntN(4), r.IntN(4)}
	if r.IntN(4) == 0 {
		delay = [2]int{AutoDelay, AutoDelay}
	}
	offset := time.Duration(r.Int64N(int64(frameDur)))
	return c, delay, offset
}

type fuzzResult struct {
	err        error
	frames     [2]int64
	common     int
	rollbacks  int
	stallTicks int
	maxDepth   int
}

const fuzzTargetFrames = 600

func runFuzzSession(seed uint64) fuzzResult {
	nc, delay, offset := fuzzConditions(seed)
	net := NewLossyNet(nc)
	script := randomInputs(seed^0x5eed, fuzzTargetFrames+64)
	var sums [2]map[int64]uint64
	var ss [2]*Session[pad]
	for i := 0; i < 2; i++ {
		i := i
		sums[i] = map[int64]uint64{}
		s, err := NewSession(Config[pad]{
			Game: newFightGame(), Codec: padCodec{}, Transport: net.Endpoint(i), LocalPlayer: i,
			InputDelay: delay[i],
			OnChecksum: func(f int64, c uint64) { sums[i][f] = c },
		})
		if err != nil {
			return fuzzResult{err: err}
		}
		ss[i] = s
	}
	next := [2]time.Duration{0, offset}
	var now time.Duration
	limit := 120 * time.Second
	for now < limit {
		i := 0
		if next[1] < next[0] {
			i = 1
		}
		now = next[i]
		next[i] += frameDur
		net.SetNow(now)
		s := ss[i]
		var in pad
		if f := s.Frame(); f < int64(len(script)) {
			in = script[f][i]
		}
		s.Advance(now, in)
		if s.Err() != nil {
			return fuzzResult{err: fmt.Errorf("peer %d: %w (cond %+v delay %v)", i, s.Err(), nc, delay)}
		}
		if ss[0].ConfirmedFrame() >= fuzzTargetFrames && ss[1].ConfirmedFrame() >= fuzzTargetFrames &&
			ss[0].Frame() >= fuzzTargetFrames && ss[1].Frame() >= fuzzTargetFrames {
			break
		}
	}
	res := fuzzResult{frames: [2]int64{ss[0].Frame(), ss[1].Frame()}}
	if res.frames[0] < fuzzTargetFrames || res.frames[1] < fuzzTargetFrames {
		res.err = fmt.Errorf("no progress: frames %v after %v (cond %+v delay %v)", res.frames, now, nc, delay)
		return res
	}
	var last int64
	for f, a := range sums[0] {
		if b, ok := sums[1][f]; ok {
			res.common++
			if a != b {
				res.err = fmt.Errorf("checksum differs at frame %d (cond %+v delay %v)", f, nc, delay)
				return res
			}
			if f > last {
				last = f
			}
		}
	}
	if last < fuzzTargetFrames-60 {
		res.err = fmt.Errorf("final common checksum at frame %d, want >= %d", last, fuzzTargetFrames-60)
	}
	for i := 0; i < 2; i++ {
		st := ss[i].Stats()
		if st.Mismatches != 0 {
			res.err = fmt.Errorf("peer %d reported %d desyncs", i, st.Mismatches)
		}
		res.rollbacks += st.Rollbacks
		res.stallTicks += st.StallTicks
		if st.MaxRollbackDepth > res.maxDepth {
			res.maxDepth = st.MaxRollbackDepth
		}
		if st.MaxRollbackDepth > 8+3 {
			res.err = fmt.Errorf("rollback depth %d exceeds window", st.MaxRollbackDepth)
		}
	}
	return res
}

func fuzzCount(t *testing.T) int {
	if v := os.Getenv("NETPLAY_FUZZ_SESSIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if testing.Short() {
		return 200
	}
	return 10000
}

// TestFuzzRollbackSessions runs seeded two-peer sessions over a lossy network
// and requires identical checksums and zero desyncs. NETPLAY_FUZZ_SESSIONS
// sets the count; the section 7 target is 10000.
func TestFuzzRollbackSessions(t *testing.T) {
	n := fuzzCount(t)
	workers := 4
	var next atomic.Int64
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []string
	var rollbacks, stalls, done int64
	deepest := 0
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				seed := uint64(next.Add(1) - 1)
				if int(seed) >= n {
					return
				}
				r := runFuzzSession(seed)
				mu.Lock()
				done++
				rollbacks += int64(r.rollbacks)
				stalls += int64(r.stallTicks)
				if r.maxDepth > deepest {
					deepest = r.maxDepth
				}
				if r.err != nil && len(failures) < 10 {
					failures = append(failures, fmt.Sprintf("seed %d: %v", seed, r.err))
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	t.Logf("fuzz: %d sessions in %v, %d rollbacks, %d stall ticks, deepest rollback %d, failures %d",
		done, time.Since(start).Round(time.Millisecond), rollbacks, stalls, deepest, len(failures))
	for _, f := range failures {
		t.Error(f)
	}
}
