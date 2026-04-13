package livesim

import (
	"encoding/json"
	"testing"

	"github.com/odvcencio/gosx/sim"
)

// helper: make an input map with a single player click at (x, y)
func clickInput(x, y int) map[string]sim.Input {
	data, _ := json.Marshal(map[string]int{"x": x, "y": y})
	return map[string]sim.Input{
		"player-1": {Data: data},
	}
}

// TestSimSpawnCircleOnInput verifies that feeding a click input spawns a circle.
func TestSimSpawnCircleOnInput(t *testing.T) {
	g := newGame()
	g.Tick(clickInput(100, 50))
	state := parseState(t, g)
	if len(state.Circles) != 1 {
		t.Fatalf("expected 1 circle, got %d", len(state.Circles))
	}
	c := state.Circles[0]
	// Circle spawns at the clicked position; allow small tolerance.
	if c.X < 80 || c.X > 120 {
		t.Errorf("circle.X = %v; want ~100", c.X)
	}
	if c.Y < 30 || c.Y > 70 {
		t.Errorf("circle.Y = %v; want ~50", c.Y)
	}
}

// TestSimGravityPullsDown verifies that circles fall under gravity.
func TestSimGravityPullsDown(t *testing.T) {
	g := newGame()
	// Spawn a circle in the middle (far from floor so it can fall freely).
	g.Tick(clickInput(400, 50))
	initialState := parseState(t, g)
	if len(initialState.Circles) == 0 {
		t.Fatal("no circles after spawn")
	}
	startY := initialState.Circles[0].Y

	// Advance 10 ticks — gravity should pull it down.
	for i := 0; i < 10; i++ {
		g.Tick(noInput())
	}
	laterState := parseState(t, g)
	if len(laterState.Circles) == 0 {
		t.Fatal("circle disappeared")
	}
	endY := laterState.Circles[0].Y
	if endY <= startY {
		t.Errorf("expected Y to increase (gravity), got startY=%v endY=%v", startY, endY)
	}
}

// TestSimFloorCollision verifies that a circle resting near the floor stays
// bounded within the world (doesn't fall through).
func TestSimFloorCollision(t *testing.T) {
	g := newGame()
	// Spawn just above the floor.
	g.Tick(clickInput(400, worldHeight-15))
	// Run many ticks to ensure it hits the floor.
	for i := 0; i < 60; i++ {
		g.Tick(noInput())
	}
	state := parseState(t, g)
	if len(state.Circles) == 0 {
		t.Fatal("circle disappeared")
	}
	c := state.Circles[0]
	// Circle center + radius must be <= worldHeight (floor).
	if c.Y+c.R > float64(worldHeight)+1 {
		t.Errorf("circle fell through floor: y=%v r=%v world=%v", c.Y, c.R, worldHeight)
	}
}

// TestSimCircleCap verifies that spawning beyond maxCircles drops the oldest.
func TestSimCircleCap(t *testing.T) {
	g := newGame()
	for i := 0; i < 60; i++ {
		data, _ := json.Marshal(map[string]int{"x": 400, "y": 250})
		g.Tick(map[string]sim.Input{
			"p": {Data: data},
		})
	}
	state := parseState(t, g)
	if len(state.Circles) > maxCircles {
		t.Errorf("circle count %d exceeds cap %d", len(state.Circles), maxCircles)
	}
}

// TestSimSnapshotRestore verifies that snapshot/restore round-trips correctly.
func TestSimSnapshotRestore(t *testing.T) {
	g := newGame()
	g.Tick(clickInput(200, 200))
	before := parseState(t, g)

	snap := g.Snapshot()

	// Mutate: advance a few ticks.
	for i := 0; i < 5; i++ {
		g.Tick(noInput())
	}
	after := parseState(t, g)
	if len(after.Circles) == 0 {
		t.Fatal("no circles after mutation ticks")
	}

	// Restore: state should match pre-mutation.
	g.Restore(snap)
	restored := parseState(t, g)
	if len(restored.Circles) != len(before.Circles) {
		t.Errorf("restored circle count %d != before %d", len(restored.Circles), len(before.Circles))
	}
	if len(restored.Circles) > 0 {
		orig := before.Circles[0]
		rest := restored.Circles[0]
		if rest.X != orig.X || rest.Y != orig.Y {
			t.Errorf("restored circle at (%v,%v), want (%v,%v)", rest.X, rest.Y, orig.X, orig.Y)
		}
	}
}

// TestSimStateJSONShape verifies the JSON state has the required top-level keys.
func TestSimStateJSONShape(t *testing.T) {
	g := newGame()
	raw := g.State()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("State() returned invalid JSON: %v", err)
	}
	required := []string{"w", "h", "frame", "circles"}
	for _, k := range required {
		if _, ok := obj[k]; !ok {
			t.Errorf("State() missing key %q", k)
		}
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func noInput() map[string]sim.Input {
	return map[string]sim.Input{}
}

type stateSnapshot struct {
	W       int       `json:"w"`
	H       int       `json:"h"`
	Frame   uint64    `json:"frame"`
	Circles []circleJ `json:"circles"`
}

type circleJ struct {
	ID uint32  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	VX float64 `json:"vx"`
	VY float64 `json:"vy"`
	R  float64 `json:"r"`
}

func parseState(t *testing.T, g *game) stateSnapshot {
	t.Helper()
	var s stateSnapshot
	if err := json.Unmarshal(g.State(), &s); err != nil {
		t.Fatalf("parseState: %v", err)
	}
	return s
}
