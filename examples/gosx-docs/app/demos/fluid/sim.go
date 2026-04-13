package fluid

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/odvcencio/gosx/field"
	"github.com/odvcencio/gosx/hub"
)

const (
	gridN      = 16
	tickRateHz = 20
	bitWidth   = 6
	topic      = "velocity"
	boundMin   = -1.0
	boundMax   = 1.0
)

// Sim runs a time-varying velocity field and publishes it to the hub.
type Sim struct {
	hub       *hub.Hub
	startTime time.Time
	running   atomic.Bool
	once      sync.Once
}

// NewSim constructs a Sim backed by the given hub.
func NewSim(h *hub.Hub) *Sim {
	return &Sim{
		hub:       h,
		startTime: time.Now(),
	}
}

// Start spins the background tick goroutine. Safe to call multiple times;
// subsequent calls are no-ops. The goroutine is intentionally leaked on
// process exit (matches the livesim pattern).
func (s *Sim) Start() {
	s.once.Do(func() {
		s.running.Store(true)
		go s.tickLoop()
	})
}

// computeFrame builds a fresh Field for time t (seconds since start).
// The analytical function produces a time-varying pseudo-curl-noise flow
// with swirling eddies — not a Navier-Stokes solution, just pretty.
func (s *Sim) computeFrame(t float32) *field.Field {
	bounds := field.AABB{
		Min: [3]float32{boundMin, boundMin, boundMin},
		Max: [3]float32{boundMax, boundMax, boundMax},
	}
	return field.FromFunc([3]int{gridN, gridN, gridN}, 3, bounds,
		func(x, y, z float32) []float32 {
			tf := float64(t)
			xf := float64(x)
			yf := float64(y)
			zf := float64(z)
			vx := math.Sin(yf*2+tf) * math.Cos(zf*3)
			vy := math.Sin(zf*2+tf) * math.Cos(xf*3)
			vz := math.Sin(xf*2+tf) * math.Cos(yf*3)
			return []float32{float32(vx), float32(vy), float32(vz)}
		},
	)
}

// tickLoop fires at tickRateHz and publishes quantized field frames.
func (s *Sim) tickLoop() {
	interval := time.Second / tickRateHz
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if !s.running.Load() {
			return
		}
		t := float32(time.Since(s.startTime).Seconds())
		f := s.computeFrame(t)
		field.PublishField(s.hub, topic, f, field.QuantizeOptions{BitWidth: bitWidth})
	}
}
