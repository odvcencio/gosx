package livesim

import (
	"encoding/json"
	"math"
	"math/rand"
	"sync"

	"github.com/odvcencio/gosx/sim"
)

const (
	worldWidth    = 800
	worldHeight   = 500
	maxCircles    = 50
	gravity       = 550.0 // px/s²
	restitution   = 0.65
	airFriction   = 0.998
	floorFriction = 0.92
	minRadius     = 10.0
	maxRadius     = 20.0
	dt            = 1.0 / 20.0 // fixed at 20 Hz
)

// circle is one particle in the simulation.
type circle struct {
	ID uint32  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	VX float64 `json:"vx"`
	VY float64 `json:"vy"`
	R  float64 `json:"r"`
}

// game implements sim.Simulation for the 2D gravity sandbox.
type game struct {
	mu      sync.Mutex
	circles []*circle
	frame   uint64
	nextID  uint32
	rng     *rand.Rand
}

func newGame() *game {
	return &game{
		rng: rand.New(rand.NewSource(42)),
	}
}

// Tick advances the simulation by one step (1/20 s).
func (g *game) Tick(inputs map[string]sim.Input) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.frame++

	// 1. Apply inputs — spawn circles.
	for _, inp := range inputs {
		var click struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		}
		if err := json.Unmarshal(inp.Data, &click); err != nil {
			continue
		}
		r := minRadius + g.rng.Float64()*(maxRadius-minRadius)
		// Clamp spawn position within world.
		cx := math.Max(r, math.Min(float64(worldWidth)-r, click.X))
		cy := math.Max(r, math.Min(float64(worldHeight)-r, click.Y))
		g.nextID++
		c := &circle{
			ID: g.nextID,
			X:  cx,
			Y:  cy,
			VX: 0,
			VY: 0,
			R:  r,
		}
		g.circles = append(g.circles, c)
		// Cap: drop oldest if over limit.
		if len(g.circles) > maxCircles {
			g.circles = g.circles[len(g.circles)-maxCircles:]
		}
	}

	// 2. Integrate each circle.
	for _, c := range g.circles {
		// Gravity.
		c.VY += gravity * dt
		// Air friction.
		c.VX *= airFriction
		c.VY *= airFriction
		// Move.
		c.X += c.VX * dt
		c.Y += c.VY * dt
	}

	// 3. Resolve wall + floor collisions.
	for _, c := range g.circles {
		// Left wall.
		if c.X-c.R < 0 {
			c.X = c.R
			if c.VX < 0 {
				c.VX = -c.VX * restitution
			}
		}
		// Right wall.
		if c.X+c.R > float64(worldWidth) {
			c.X = float64(worldWidth) - c.R
			if c.VX > 0 {
				c.VX = -c.VX * restitution
			}
		}
		// Ceiling.
		if c.Y-c.R < 0 {
			c.Y = c.R
			if c.VY < 0 {
				c.VY = -c.VY * restitution
			}
		}
		// Floor.
		if c.Y+c.R >= float64(worldHeight) {
			c.Y = float64(worldHeight) - c.R
			if c.VY > 0 {
				c.VY = -c.VY * restitution
			}
			c.VX *= floorFriction
		}
	}

	// 4. Pairwise circle–circle collision resolution.
	n := len(g.circles)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			a, b := g.circles[i], g.circles[j]
			dx := b.X - a.X
			dy := b.Y - a.Y
			dist := math.Sqrt(dx*dx + dy*dy)
			minDist := a.R + b.R
			if dist < minDist && dist > 1e-6 {
				// Separation: push apart.
				overlap := minDist - dist
				nx := dx / dist
				ny := dy / dist
				a.X -= nx * overlap * 0.5
				a.Y -= ny * overlap * 0.5
				b.X += nx * overlap * 0.5
				b.Y += ny * overlap * 0.5
				// Velocity reflection along collision normal.
				dvx := b.VX - a.VX
				dvy := b.VY - a.VY
				dot := dvx*nx + dvy*ny
				if dot < 0 {
					impulse := dot * restitution
					a.VX += impulse * nx
					a.VY += impulse * ny
					b.VX -= impulse * nx
					b.VY -= impulse * ny
				}
			}
		}
	}
}

// Snapshot returns the serialized current state for rollback.
func (g *game) Snapshot() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	type snap struct {
		Frame   uint64    `json:"frame"`
		NextID  uint32    `json:"nextID"`
		Circles []*circle `json:"circles"`
	}
	b, _ := json.Marshal(snap{
		Frame:   g.frame,
		NextID:  g.nextID,
		Circles: g.circles,
	})
	return b
}

// Restore resets the simulation to a prior snapshot.
func (g *game) Restore(raw []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	type snap struct {
		Frame   uint64    `json:"frame"`
		NextID  uint32    `json:"nextID"`
		Circles []*circle `json:"circles"`
	}
	var s snap
	if err := json.Unmarshal(raw, &s); err != nil {
		return
	}
	g.frame = s.Frame
	g.nextID = s.NextID
	g.circles = s.Circles
}

// State returns the current world as JSON for broadcast.
// Values are rounded to 1 decimal place to reduce wire size.
func (g *game) State() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()

	type wireCircle struct {
		ID uint32  `json:"id"`
		X  float64 `json:"x"`
		Y  float64 `json:"y"`
		VX float64 `json:"vx"`
		VY float64 `json:"vy"`
		R  float64 `json:"r"`
	}

	wcs := make([]wireCircle, len(g.circles))
	for i, c := range g.circles {
		wcs[i] = wireCircle{
			ID: c.ID,
			X:  round1(c.X),
			Y:  round1(c.Y),
			VX: round1(c.VX),
			VY: round1(c.VY),
			R:  round1(c.R),
		}
	}

	type world struct {
		W       int          `json:"w"`
		H       int          `json:"h"`
		Frame   uint64       `json:"frame"`
		Circles []wireCircle `json:"circles"`
	}

	b, _ := json.Marshal(world{
		W:       worldWidth,
		H:       worldHeight,
		Frame:   g.frame,
		Circles: wcs,
	})
	return b
}

// round1 rounds a float64 to 1 decimal place.
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
