package livesim

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/sim"
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
// Spawns comfortably more than maxCircles so the cap logic is actually
// exercised (rather than trivially satisfied by an under-cap spawn count).
func TestSimCircleCap(t *testing.T) {
	g := newGame()
	for i := 0; i < maxCircles+30; i++ {
		data, _ := json.Marshal(map[string]int{"x": 400, "y": 250})
		g.Tick(map[string]sim.Input{
			"p": {Data: data},
		})
	}
	state := parseState(t, g)
	if len(state.Circles) > maxCircles {
		t.Errorf("circle count %d exceeds cap %d", len(state.Circles), maxCircles)
	}
	if len(state.Circles) != maxCircles {
		t.Errorf("expected exactly %d circles once over-spawned, got %d", maxCircles, len(state.Circles))
	}
}

// TestSimBurstSpawnsCluster verifies that a single Burst input spawns a
// bounded cluster of circles (not just one), all near the click point, in
// one tick — proving the Burst button really does something server-side
// rather than merely relabeling a single spawn.
func TestSimBurstSpawnsCluster(t *testing.T) {
	g := newGame()
	data, _ := json.Marshal(map[string]any{"x": 400, "y": 100, "burst": true})
	g.Tick(map[string]sim.Input{
		"player-1": {Data: data},
	})
	state := parseState(t, g)
	if len(state.Circles) < burstMinCircles || len(state.Circles) > burstMaxCircles {
		t.Fatalf("burst spawned %d circles; want between %d and %d", len(state.Circles), burstMinCircles, burstMaxCircles)
	}
	for _, c := range state.Circles {
		if c.X < 400-burstSpreadX-maxRadius-1 || c.X > 400+burstSpreadX+maxRadius+1 {
			t.Errorf("burst circle X = %v out of expected spread around 400", c.X)
		}
		if c.Y < 100-burstSpreadY-maxRadius-1 || c.Y > 100+burstSpreadY+maxRadius+1 {
			t.Errorf("burst circle Y = %v out of expected spread around 100", c.Y)
		}
	}
}

// TestSimBurstRespectsCap verifies that a burst still enforces maxCircles
// when the world is already near the cap.
func TestSimBurstRespectsCap(t *testing.T) {
	g := newGame()
	for i := 0; i < maxCircles-5; i++ {
		data, _ := json.Marshal(map[string]int{"x": 400, "y": 250})
		g.Tick(map[string]sim.Input{"p": {Data: data}})
	}
	data, _ := json.Marshal(map[string]any{"x": 400, "y": 100, "burst": true})
	g.Tick(map[string]sim.Input{"burster": {Data: data}})
	state := parseState(t, g)
	if len(state.Circles) > maxCircles {
		t.Errorf("circle count %d exceeds cap %d after burst", len(state.Circles), maxCircles)
	}
}

// TestSimSpawnAttributionIncludesPlayerID verifies that State() reports
// which player caused a spawn, so the client can tie a spawn flash to the
// right presence ghost cursor.
func TestSimSpawnAttributionIncludesPlayerID(t *testing.T) {
	g := newGame()
	g.Tick(clickInput(200, 60))

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(g.State(), &obj); err != nil {
		t.Fatalf("State() invalid JSON: %v", err)
	}
	raw, ok := obj["spawns"]
	if !ok {
		t.Fatal("State() missing \"spawns\" key after a spawning tick")
	}
	var spawns []struct {
		ID    string  `json:"id"`
		X     float64 `json:"x"`
		Y     float64 `json:"y"`
		Burst bool    `json:"burst"`
	}
	if err := json.Unmarshal(raw, &spawns); err != nil {
		t.Fatalf("spawns not valid JSON: %v", err)
	}
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn event, got %d", len(spawns))
	}
	if spawns[0].ID != "player-1" {
		t.Errorf("spawn attributed to %q; want \"player-1\"", spawns[0].ID)
	}
	if spawns[0].Burst {
		t.Errorf("expected non-burst spawn event")
	}

	// Spawns are transient — the next tick with no input must not carry
	// forward stale spawn events.
	g.Tick(noInput())
	var obj2 map[string]json.RawMessage
	if err := json.Unmarshal(g.State(), &obj2); err != nil {
		t.Fatalf("State() invalid JSON: %v", err)
	}
	if raw2, ok := obj2["spawns"]; ok && string(raw2) != "null" {
		var spawns2 []json.RawMessage
		if err := json.Unmarshal(raw2, &spawns2); err == nil && len(spawns2) != 0 {
			t.Errorf("expected no spawn events on a quiet tick, got %d", len(spawns2))
		}
	}
}

// TestSimContactEventOnHardFloorImpact verifies that a circle dropped from
// height (a "hard" impact) produces a contact event with a plausible
// position and a positive impact magnitude, and that resting circles stop
// producing contacts once they settle below contactMinSpeed.
func TestSimContactEventOnHardFloorImpact(t *testing.T) {
	g := newGame()
	g.Tick(clickInput(400, 20))

	sawContact := false
	for i := 0; i < 200; i++ {
		g.Tick(noInput())
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(g.State(), &obj); err != nil {
			t.Fatalf("State() invalid JSON: %v", err)
		}
		raw, ok := obj["contacts"]
		if !ok {
			continue
		}
		var contacts []struct {
			X   float64 `json:"x"`
			Y   float64 `json:"y"`
			Mag float64 `json:"mag"`
		}
		if err := json.Unmarshal(raw, &contacts); err != nil {
			t.Fatalf("contacts not valid JSON: %v", err)
		}
		for _, c := range contacts {
			if c.Mag < contactMinSpeed {
				t.Errorf("contact magnitude %v below contactMinSpeed %v", c.Mag, contactMinSpeed)
			}
			if c.Y < float64(worldHeight)-30 {
				t.Errorf("floor contact Y = %v, expected near the floor (worldHeight=%d)", c.Y, worldHeight)
			}
			sawContact = true
		}
	}
	if !sawContact {
		t.Fatal("expected at least one contact event from a hard floor impact within 200 ticks")
	}
}

// TestSimViewerCountReportsSetViewers verifies that SetViewers is reflected
// in the next State() broadcast, independent of the tick loop.
func TestSimViewerCountReportsSetViewers(t *testing.T) {
	g := newGame()
	g.SetViewers(3)
	state := parseState(t, g)
	if state.Viewers != 3 {
		t.Errorf("state.Viewers = %d; want 3", state.Viewers)
	}
	g.SetViewers(1)
	state = parseState(t, g)
	if state.Viewers != 1 {
		t.Errorf("state.Viewers = %d; want 1", state.Viewers)
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
	required := []string{"w", "h", "frame", "circles", "viewers"}
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
	Viewers int       `json:"viewers"`
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
