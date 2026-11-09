package netsync

import (
	"time"

	"m31labs.dev/gosx/room/wire"
)

// clockSync estimates round-trip time and the server's tick timeline from
// Pong frames. The timeline estimate uses the sample with the lowest round
// trip among recent ones: the less time a ping spent queued, the less its
// one-way delays can differ.
type clockSync struct {
	samples [8]syncSample
	next    int
	srtt    float64 // smoothed RTT, nanoseconds
	rttvar  float64 // smoothed mean deviation, nanoseconds
	minRTT  time.Duration
	last    time.Duration
	have    bool
}

type syncSample struct {
	rtt    time.Duration
	server float64       // server timeline, in ticks, when the pong arrived
	at     time.Duration // local time the pong arrived
	ok     bool
}

// sampleTTL bounds how old a timeline sample may be. A server that drops a
// backlog moves its timeline, so old samples go stale.
const sampleTTL = 8 * time.Second

func (s *clockSync) reset() { *s = clockSync{} }

func (s *clockSync) add(now time.Duration, p *wire.Pong, period time.Duration) {
	rtt := now - time.Duration(p.Sent)
	if rtt < 0 || rtt > 30*time.Second {
		return
	}
	s.last = rtt
	if !s.have {
		s.srtt, s.rttvar, s.minRTT, s.have = float64(rtt), float64(rtt)/2, rtt, true
	} else {
		// RFC 6298 smoothing.
		dev := float64(rtt) - s.srtt
		if dev < 0 {
			dev = -dev
		}
		s.rttvar = 0.75*s.rttvar + 0.25*dev
		s.srtt = 0.875*s.srtt + 0.125*float64(rtt)
		s.minRTT = min(s.minRTT, rtt)
	}
	if p.Tick == 0 && p.Frac == 0 {
		return // the room was not running: no timeline in this pong
	}
	s.samples[s.next] = syncSample{
		rtt:    rtt,
		server: p.ServerTick() + float64(rtt/2)/float64(period),
		at:     now,
		ok:     true,
	}
	s.next = (s.next + 1) % len(s.samples)
}

// serverTick estimates the server's timeline at now, in ticks.
func (s *clockSync) serverTick(now, period time.Duration) (float64, bool) {
	best := -1
	for i, smp := range s.samples {
		if !smp.ok || now-smp.at > sampleTTL {
			continue
		}
		if best < 0 || smp.rtt < s.samples[best].rtt {
			best = i
		}
	}
	if best < 0 {
		return 0, false
	}
	b := s.samples[best]
	return b.server + float64(now-b.at)/float64(period), true
}

func (s *clockSync) rtt() time.Duration    { return time.Duration(s.srtt) }
func (s *clockSync) jitter() time.Duration { return time.Duration(s.rttvar) }
