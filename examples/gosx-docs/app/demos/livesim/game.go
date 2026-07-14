package livesim

import (
	"encoding/json"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"

	"m31labs.dev/gosx/sim"
)

const (
	worldWidth    = 800
	worldHeight   = 500
	maxCircles    = 120
	gravity       = 550.0 // px/s²
	restitution   = 0.65
	airFriction   = 0.998
	floorFriction = 0.92
	minRadius     = 10.0
	maxRadius     = 20.0
	dt            = 1.0 / 20.0 // fixed at 20 Hz

	// contactMinSpeed is the minimum pre-impact speed (px/s) required for a
	// wall/floor/pairwise collision to be reported as a "contact" event.
	// Resting circles re-trigger the floor branch every tick at ~0 velocity;
	// gating on speed keeps contact events meaningful "hits" instead of noise.
	contactMinSpeed = 40.0

	// Burst spawns a cluster of circles from a single click, spread around
	// the click point. Count is server-chosen (not client-supplied) so the
	// button's effect is authoritative and bounded.
	burstMinCircles = 15
	burstMaxCircles = 20
	burstSpreadX    = 90.0 // px, horizontal jitter half-width
	burstSpreadY    = 36.0 // px, vertical jitter half-width
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

// contactEvent is a transient per-tick record of a collision (wall, floor,
// or pairwise) strong enough to be worth a visual "hit" flash. It is
// derived from the same impulse math that already resolves the collision,
// so it never invents state the physics didn't produce.
type contactEvent struct {
	X   float64
	Y   float64
	Mag float64 // impact speed / impulse magnitude, px/s
}

// spawnEvent attributes a circle spawn (or burst) to the player whose input
// caused it, for client-side "who did that" flashes tied to presence cursors.
type spawnEvent struct {
	PlayerID string
	X        float64
	Y        float64
	Burst    bool
}

// game implements sim.Simulation for the 2D gravity sandbox.
type game struct {
	mu       sync.Mutex
	circles  []*circle
	frame    uint64
	nextID   uint32
	rng      *rand.Rand
	contacts []contactEvent
	spawns   []spawnEvent

	// viewers is set from outside the tick loop (a lightweight poller reading
	// hub.Hub.ClientCount()) so State() can report live presence without
	// game needing a hub reference. Accessed without g.mu since it's a
	// single machine word.
	viewers atomic.Int32
}

func newGame() *game {
	return &game{
		rng: rand.New(rand.NewSource(42)),
	}
}

// SetViewers records the current connected-client count for the next
// broadcast. Safe to call concurrently with Tick/State from any goroutine.
func (g *game) SetViewers(n int) {
	g.viewers.Store(int32(n))
}

// Tick advances the simulation by one step (1/20 s).
func (g *game) Tick(inputs map[string]sim.Input) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.frame++
	// Contacts and spawns are transient per-tick events, not accumulated
	// world state — reset before this tick derives fresh ones.
	g.contacts = g.contacts[:0]
	g.spawns = g.spawns[:0]

	// 1. Apply inputs — spawn circles (or a burst cluster of them).
	for playerID, inp := range inputs {
		var click struct {
			X     float64 `json:"x"`
			Y     float64 `json:"y"`
			Burst bool    `json:"burst"`
		}
		if err := json.Unmarshal(inp.Data, &click); err != nil {
			continue
		}
		if click.Burst {
			n := burstMinCircles + g.rng.Intn(burstMaxCircles-burstMinCircles+1)
			for i := 0; i < n; i++ {
				jx := click.X + (g.rng.Float64()*2-1)*burstSpreadX
				jy := click.Y + (g.rng.Float64()*2-1)*burstSpreadY
				g.spawnOne(jx, jy)
			}
			g.spawns = append(g.spawns, spawnEvent{PlayerID: playerID, X: click.X, Y: click.Y, Burst: true})
			continue
		}
		g.spawnOne(click.X, click.Y)
		g.spawns = append(g.spawns, spawnEvent{PlayerID: playerID, X: click.X, Y: click.Y})
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
				speed := -c.VX
				c.VX = -c.VX * restitution
				g.recordContact(c.X, c.Y, speed)
			}
		}
		// Right wall.
		if c.X+c.R > float64(worldWidth) {
			c.X = float64(worldWidth) - c.R
			if c.VX > 0 {
				speed := c.VX
				c.VX = -c.VX * restitution
				g.recordContact(c.X, c.Y, speed)
			}
		}
		// Ceiling.
		if c.Y-c.R < 0 {
			c.Y = c.R
			if c.VY < 0 {
				speed := -c.VY
				c.VY = -c.VY * restitution
				g.recordContact(c.X, c.Y, speed)
			}
		}
		// Floor.
		if c.Y+c.R >= float64(worldHeight) {
			c.Y = float64(worldHeight) - c.R
			if c.VY > 0 {
				speed := c.VY
				c.VY = -c.VY * restitution
				g.recordContact(c.X, c.Y, speed)
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
					g.recordContact((a.X+b.X)/2, (a.Y+b.Y)/2, -dot)
				}
			}
		}
	}
}

// spawnOne creates a single circle near (x, y), clamped inside the world,
// appends it, and enforces the maxCircles cap by dropping the oldest circle
// if needed. Shared by single-click spawn and Burst clusters.
func (g *game) spawnOne(x, y float64) *circle {
	r := minRadius + g.rng.Float64()*(maxRadius-minRadius)
	cx := math.Max(r, math.Min(float64(worldWidth)-r, x))
	cy := math.Max(r, math.Min(float64(worldHeight)-r, y))
	g.nextID++
	c := &circle{
		ID: g.nextID,
		X:  cx,
		Y:  cy,
		R:  r,
	}
	g.circles = append(g.circles, c)
	if len(g.circles) > maxCircles {
		g.circles = g.circles[len(g.circles)-maxCircles:]
	}
	return c
}

// recordContact appends a contact event if the impact speed clears
// contactMinSpeed. Below that threshold, a contact is resting/settling
// noise rather than a visually meaningful hit.
func (g *game) recordContact(x, y, mag float64) {
	if mag < contactMinSpeed {
		return
	}
	g.contacts = append(g.contacts, contactEvent{X: x, Y: y, Mag: mag})
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

	type wireContact struct {
		X   float64 `json:"x"`
		Y   float64 `json:"y"`
		Mag float64 `json:"mag"`
	}

	var wcontacts []wireContact
	if len(g.contacts) > 0 {
		wcontacts = make([]wireContact, len(g.contacts))
		for i, ct := range g.contacts {
			wcontacts[i] = wireContact{X: round1(ct.X), Y: round1(ct.Y), Mag: round1(ct.Mag)}
		}
	}

	type wireSpawn struct {
		ID    string  `json:"id"`
		X     float64 `json:"x"`
		Y     float64 `json:"y"`
		Burst bool    `json:"burst,omitempty"`
	}

	var wspawns []wireSpawn
	if len(g.spawns) > 0 {
		wspawns = make([]wireSpawn, len(g.spawns))
		for i, sp := range g.spawns {
			wspawns[i] = wireSpawn{ID: sp.PlayerID, X: round1(sp.X), Y: round1(sp.Y), Burst: sp.Burst}
		}
	}

	type world struct {
		W        int           `json:"w"`
		H        int           `json:"h"`
		Frame    uint64        `json:"frame"`
		Circles  []wireCircle  `json:"circles"`
		Contacts []wireContact `json:"contacts,omitempty"`
		Spawns   []wireSpawn   `json:"spawns,omitempty"`
		Viewers  int           `json:"viewers"`
	}

	b, _ := json.Marshal(world{
		W:        worldWidth,
		H:        worldHeight,
		Frame:    g.frame,
		Circles:  wcs,
		Contacts: wcontacts,
		Spawns:   wspawns,
		Viewers:  int(g.viewers.Load()),
	})
	return b
}

// round1 rounds a float64 to 1 decimal place.
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
