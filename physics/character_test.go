package physics

import (
	"encoding/json"
	"math"
	"testing"
)

const characterDT = 1.0 / 60

// characterFloorWorld is a 40 m square floor whose top is at y = 0.
func characterFloorWorld() *World {
	w := NewWorld(WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: characterDT})
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 40, Height: 1, Depth: 40, Offset: Vec3{Y: -0.5}})
	return w
}

// settle drops a character onto the ground.
func settle(t *testing.T, w *World, c *Character) {
	t.Helper()
	for i := 0; i < 120 && !c.Grounded(); i++ {
		c.Step(characterDT, CharacterInput{})
		w.StepFixed()
	}
	if !c.Grounded() {
		t.Fatalf("character never landed; foot at %+v", c.FootPosition())
	}
}

// walk steps the character n times with the same input and returns how many
// of those steps ended airborne.
func walk(w *World, c *Character, n int, input CharacterInput) int {
	airborne := 0
	for i := 0; i < n; i++ {
		c.Step(characterDT, input)
		w.StepFixed()
		if !c.Grounded() {
			airborne++
		}
	}
	return airborne
}

// addStairs builds count stairs rising along +X from x = start, each rise
// tall and run deep, and a landing on top.
func addStairs(w *World, start, rise, run float64, count int) float64 {
	for i := 0; i < count; i++ {
		h := rise * float64(i+1)
		w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: run, Height: h, Depth: 3,
			Offset: Vec3{X: start + run*float64(i) + run/2, Y: h / 2}})
	}
	top := rise * float64(count)
	end := start + run*float64(count)
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 6, Height: top, Depth: 3, Offset: Vec3{X: end + 3, Y: top / 2}})
	return top
}

// addRamp adds a box ramp that rises along +X at the given angle, its lower
// edge at x = start on the floor.
func addRamp(w *World, start, angle, length float64) *Collider {
	thickness := 0.5
	rotation := QuatFromAxisAngle(Vec3{Z: 1}, angle)
	// Place the box so its top face starts at (start, 0).
	topCenterLocal := Vec3{X: length / 2, Y: thickness / 2}
	corner := Vec3{X: start}
	center := corner.Add(rotation.Rotate(topCenterLocal)).Sub(rotation.Rotate(Vec3{Y: thickness}))
	return w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: length, Height: thickness, Depth: 4, Offset: center, Rotation: rotation})
}

func TestCharacterFallsLandsAndRests(t *testing.T) {
	w := characterFloorWorld()
	c := w.AddCharacter(CharacterConfig{ID: "hero", Position: Vec3{Y: 3}})
	landings := 0
	for i := 0; i < 120; i++ {
		if move := c.Step(characterDT, CharacterInput{}); move.Landed {
			landings++
		}
		w.StepFixed()
	}
	if landings != 1 || !c.Grounded() {
		t.Fatalf("landings = %d, grounded = %v", landings, c.Grounded())
	}
	if foot := c.FootPosition().Y; math.Abs(foot-0.02) > 1e-9 {
		t.Fatalf("foot at %v, want the 0.02 m skin above the floor", foot)
	}
	if c.Velocity() != (Vec3{}) || !c.GroundNormal().Near(Vec3{Y: 1}, 1e-9) {
		t.Fatalf("resting velocity %+v ground normal %+v", c.Velocity(), c.GroundNormal())
	}
	// Resting is exact: nothing creeps.
	before := c.Position()
	walk(w, c, 300, CharacterInput{})
	if c.Position() != before {
		t.Fatalf("resting character moved from %+v to %+v", before, c.Position())
	}
}

func TestCharacterWalksAtRequestedSpeed(t *testing.T) {
	w := characterFloorWorld()
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
	settle(t, w, c)
	start := c.Position()
	if airborne := walk(w, c, 60, CharacterInput{Move: Vec3{X: 3, Y: 50, Z: 4}}); airborne != 0 {
		t.Fatalf("walking on flat ground left the ground %d times", airborne)
	}
	moved := c.Position().Sub(start)
	if !moved.Near(Vec3{X: 3, Z: 4}, 1e-9) {
		t.Fatalf("one second at (3, 0, 4) m/s moved %+v; Move's Up part must be ignored", moved)
	}
}

func TestCharacterSlidesAlongWallAndStopsInCorner(t *testing.T) {
	w := characterFloorWorld()
	// A wall at x = 2 and another at z = 2 make a corner.
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 3, Depth: 20, Offset: Vec3{X: 2.5, Y: 1.5}})
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 20, Height: 3, Depth: 1, Offset: Vec3{Z: 6.5, Y: 1.5}})
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
	settle(t, w, c)

	// Diagonally into the wall: the X motion stops, the Z motion keeps going.
	var move CharacterMove
	for i := 0; i < 60; i++ {
		move = c.Step(characterDT, CharacterInput{Move: Vec3{X: 3, Z: 2}})
		w.StepFixed()
	}
	pos := c.Position()
	if move.Collisions&CollidedSides == 0 {
		t.Fatal("pushing into a wall must report a side collision")
	}
	if pos.X > 2-0.35 || pos.X < 2-0.35-0.03 {
		t.Fatalf("x = %v, want resting a skin away from the wall at 1.65", pos.X)
	}
	if pos.Z < 1.9 {
		t.Fatalf("z = %v; sliding along the wall should keep the Z motion", pos.Z)
	}

	// Into the corner: it stops and stays still.
	walk(w, c, 120, CharacterInput{Move: Vec3{X: 3, Z: 3}})
	corner := c.Position()
	walk(w, c, 60, CharacterInput{Move: Vec3{X: 3, Z: 3}})
	if !c.Position().Near(corner, 1e-9) {
		t.Fatalf("character jitters in the corner: %+v then %+v", corner, c.Position())
	}
	if corner.X > 1.65+1e-9 || corner.Z > 6-0.35+1e-9 {
		t.Fatalf("character entered the corner walls: %+v", corner)
	}
}

func TestCharacterClimbsStairsAndWalksDown(t *testing.T) {
	w := characterFloorWorld()
	top := addStairs(w, 2, 0.2, 0.4, 5)
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
	settle(t, w, c)
	stepped := false
	for i := 0; i < 150; i++ {
		if move := c.Step(characterDT, CharacterInput{Move: Vec3{X: 3}}); move.Stepped {
			stepped = true
		}
		w.StepFixed()
		if !c.Grounded() {
			t.Fatalf("step %d: climbing stairs left the ground at %+v", i, c.FootPosition())
		}
	}
	if !stepped || math.Abs(c.FootPosition().Y-(top+0.02)) > 1e-6 {
		t.Fatalf("stepped=%v foot=%v, want the landing at %v", stepped, c.FootPosition().Y, top+0.02)
	}
	// Back down: ground snapping keeps the character on every stair.
	for i := 0; i < 150; i++ {
		c.Step(characterDT, CharacterInput{Move: Vec3{X: -3}})
		w.StepFixed()
		if !c.Grounded() {
			t.Fatalf("step %d: walking down stairs left the ground at %+v", i, c.FootPosition())
		}
	}
	if math.Abs(c.FootPosition().Y-0.02) > 1e-6 {
		t.Fatalf("foot at %v after walking down, want the floor", c.FootPosition().Y)
	}
}

func TestCharacterStopsAtLedgeTallerThanStep(t *testing.T) {
	w := characterFloorWorld()
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 0.5, Depth: 4, Offset: Vec3{X: 3, Y: 0.25}})
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
	settle(t, w, c)
	walk(w, c, 120, CharacterInput{Move: Vec3{X: 3}})
	if foot := c.FootPosition(); foot.Y > 0.03 || c.Position().X > 1-0.35+1e-9 {
		t.Fatalf("a 0.5 m ledge is taller than the 0.3 m step; character reached %+v", c.Position())
	}
}

func TestCharacterSlopes(t *testing.T) {
	t.Run("walkable", func(t *testing.T) {
		w := characterFloorWorld()
		addRamp(w, 1, 30*math.Pi/180, 8)
		c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
		settle(t, w, c)
		if airborne := walk(w, c, 90, CharacterInput{Move: Vec3{X: 3}}); airborne != 0 {
			t.Fatalf("walking up a 30 degree ramp left the ground %d times", airborne)
		}
		foot := c.FootPosition()
		// The ramp surface under the foot is at (x - 1) tan 30 degrees.
		if foot.Y < 1 || c.GroundNormal().Y < math.Cos(31*math.Pi/180) || c.GroundNormal().Y > math.Cos(29*math.Pi/180) {
			t.Fatalf("on the ramp: foot %+v ground normal %+v", foot, c.GroundNormal())
		}
		// Walking uphill keeps the requested speed along the surface.
		before := c.Position()
		walk(w, c, 30, CharacterInput{Move: Vec3{X: 3}})
		if speed := c.Position().Sub(before).Len() * 2; math.Abs(speed-3) > 0.05 {
			t.Fatalf("uphill speed %v m/s, want 3", speed)
		}
		// Down again without leaving the ground.
		if airborne := walk(w, c, 120, CharacterInput{Move: Vec3{X: -3}}); airborne != 0 {
			t.Fatalf("walking down the ramp left the ground %d times", airborne)
		}
	})
	t.Run("too steep", func(t *testing.T) {
		w := characterFloorWorld()
		addRamp(w, 1, 60*math.Pi/180, 6)
		c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
		settle(t, w, c)
		walk(w, c, 120, CharacterInput{Move: Vec3{X: 3}})
		if foot := c.FootPosition(); foot.Y > 0.35 {
			t.Fatalf("character climbed a 60 degree slope to %+v", foot)
		}
		// Dropped onto the steep face, it slides off rather than standing.
		c.Teleport(Vec3{X: 2.2, Y: 3.5})
		for i := 0; i < 240; i++ {
			c.Step(characterDT, CharacterInput{})
			w.StepFixed()
			if c.Grounded() && c.GroundCollider() != nil && c.GroundNormal().Y < 0.7 {
				t.Fatalf("character stood on the steep face with normal %+v", c.GroundNormal())
			}
		}
		if !c.Grounded() || c.FootPosition().Y > 0.03 || c.Position().X > 1 {
			t.Fatalf("character should slide down to the floor, ended at %+v grounded=%v", c.Position(), c.Grounded())
		}
	})
}

func TestCharacterWalksOffLedgeAndCoyoteJumps(t *testing.T) {
	build := func(coyote float64) (*World, *Character) {
		w := NewWorld(WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: characterDT})
		// A platform 2 m up, ending at x = 2, over a floor.
		w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 1, Depth: 4, Offset: Vec3{Y: 1.5}})
		w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 40, Height: 1, Depth: 40, Offset: Vec3{Y: -0.5}})
		c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 3.5}, JumpSpeed: 5, CoyoteTime: coyote})
		return w, c
	}
	w, c := build(0)
	settle(t, w, c)
	leftAt := -1
	for i := 0; i < 120; i++ {
		c.Step(characterDT, CharacterInput{Move: Vec3{X: 3}})
		w.StepFixed()
		if leftAt < 0 && !c.Grounded() {
			leftAt = i
		}
	}
	// The capsule leaves once its centre is past the edge by about its radius.
	if leftAt < 0 || c.Position().X < 2.2 {
		t.Fatalf("character never walked off the ledge (left at step %d, x %v)", leftAt, c.Position().X)
	}
	if foot := c.FootPosition().Y; math.Abs(foot-0.02) > 1e-6 || !c.Grounded() {
		t.Fatalf("after the drop the foot is at %v grounded=%v, want the floor", foot, c.Grounded())
	}

	// Coyote time: a jump a few steps after walking off still starts.
	for _, tc := range []struct {
		coyote float64
		late   int
		jumps  bool
	}{{0.15, 4, true}, {0.15, 15, false}, {0, 1, false}} {
		w, c := build(tc.coyote)
		settle(t, w, c)
		for c.Grounded() {
			c.Step(characterDT, CharacterInput{Move: Vec3{X: 3}})
			w.StepFixed()
		}
		walk(w, c, tc.late-1, CharacterInput{Move: Vec3{X: 3}})
		move := c.Step(characterDT, CharacterInput{Move: Vec3{X: 3}, Jump: true})
		w.StepFixed()
		if move.Jumped != tc.jumps {
			t.Fatalf("coyote %v, jump %d steps late: jumped = %v, want %v", tc.coyote, tc.late, move.Jumped, tc.jumps)
		}
	}
}

func TestCharacterJumpHeightBufferAndCeiling(t *testing.T) {
	w := characterFloorWorld()
	speed := JumpSpeedForHeight(1.2, 9.81)
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}, JumpSpeed: speed, JumpBufferTime: 0.1})
	settle(t, w, c)
	ground := c.FootPosition().Y
	move := c.Step(characterDT, CharacterInput{Jump: true})
	w.StepFixed()
	if !move.Jumped || c.Grounded() {
		t.Fatalf("jump did not start: %+v", move)
	}
	apex := ground
	landedAt := 0
	for i := 1; i < 120; i++ {
		input := CharacterInput{}
		// Press jump again five steps before landing: the buffer holds it.
		if c.Velocity().Y < 0 && c.FootPosition().Y-ground < 0.25 && landedAt == 0 {
			input.Jump = true
			landedAt = -1
		}
		move = c.Step(characterDT, input)
		w.StepFixed()
		apex = math.Max(apex, c.FootPosition().Y)
		if move.Landed {
			if !move.Jumped {
				// The buffered jump fires on the landing step or the next.
				move = c.Step(characterDT, CharacterInput{})
				w.StepFixed()
			}
			if !move.Jumped {
				t.Fatal("a jump pressed just before landing must fire on landing")
			}
			break
		}
	}
	if rise := apex - ground; math.Abs(rise-1.2) > 0.06 {
		t.Fatalf("jump rose %v m, want about 1.2", rise)
	}

	// A ceiling 0.5 m over the head stops the jump without entering it.
	w2 := characterFloorWorld()
	w2.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 0.5, Depth: 4, Offset: Vec3{Y: 2.55}})
	c2 := w2.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}, JumpSpeed: speed})
	settle(t, w2, c2)
	c2.Step(characterDT, CharacterInput{Jump: true})
	w2.StepFixed()
	bumped := false
	for i := 0; i < 60; i++ {
		move := c2.Step(characterDT, CharacterInput{})
		w2.StepFixed()
		if move.Collisions&CollidedAbove != 0 {
			bumped = true
			if c2.Velocity().Y > 0 {
				t.Fatal("hitting the ceiling must stop the rise")
			}
		}
		if top := c2.Position().Y + 0.9; top > 2.3+1e-9 {
			t.Fatalf("head at %v entered the ceiling at 2.3", top)
		}
	}
	if !bumped || !c2.Grounded() {
		t.Fatalf("bumped=%v grounded=%v", bumped, c2.Grounded())
	}
}

func TestCharacterRidesMovingPlatforms(t *testing.T) {
	w := characterFloorWorld()
	platform := w.AddBody(BodyConfig{ID: "platform", IsKinematic: true, Position: Vec3{Y: 1}})
	platform.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 3, Height: 0.2, Depth: 3})
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 2.5}, JumpSpeed: 4})
	settle(t, w, c)
	if c.GroundCollider().Body != platform {
		t.Fatal("character should stand on the platform")
	}
	offset := c.Position().Sub(platform.Position)

	// Slide, lift, lower and turn the platform; the character keeps its place.
	for i := 1; i <= 240; i++ {
		tt := float64(i) * characterDT
		platform.Position = Vec3{X: 2 * math.Sin(tt), Y: 1 + 0.8*math.Sin(1.3*tt), Z: 0.5 * tt}
		platform.Rotation = QuatFromAxisAngle(Vec3{Y: 1}, 0.4*tt)
		move := c.Step(characterDT, CharacterInput{})
		w.StepFixed()
		if !c.Grounded() {
			t.Fatalf("step %d: the character fell off the moving platform", i)
		}
		if i > 1 {
			want := QuatFromAxisAngle(Vec3{Y: 1}, 0.4*characterDT)
			got := move.CarriedRotation
			if math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.W-want.W) > 1e-9 {
				t.Fatalf("carried rotation %+v, want %+v", got, want)
			}
		}
	}
	local := platform.Rotation.Conjugate().Rotate(c.Position().Sub(platform.Position))
	if !local.Near(offset, 1e-6) {
		t.Fatalf("character drifted on the platform: local offset %+v, started %+v", local, offset)
	}

	// Jumping off a moving platform keeps its velocity in the air.
	platform.Rotation = IdentityQuat()
	for i := 0; i < 5; i++ {
		platform.Position = platform.Position.Add(Vec3{X: 0.05})
		c.Step(characterDT, CharacterInput{})
		w.StepFixed()
	}
	platform.Position = platform.Position.Add(Vec3{X: 0.05})
	move := c.Step(characterDT, CharacterInput{Jump: true})
	w.StepFixed()
	if !move.Jumped || math.Abs(c.Velocity().X-3) > 1e-6 {
		t.Fatalf("jumped=%v velocity %+v, want the platform's 3 m/s along X", move.Jumped, c.Velocity())
	}
}

func TestCharacterPushesDynamicBodies(t *testing.T) {
	w := characterFloorWorld()
	crate := w.AddBody(BodyConfig{ID: "crate", Mass: 20, Friction: 0.5, Position: Vec3{X: 2, Y: 0.5}})
	crate.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
	settle(t, w, c)
	pushes := 0
	for i := 0; i < 120; i++ {
		move := c.Step(characterDT, CharacterInput{Move: Vec3{X: 2}})
		pushes += move.Pushed
		w.StepFixed()
	}
	if pushes == 0 || crate.Position.X < 3 {
		t.Fatalf("pushes=%d crate at %+v; walking into a crate should shove it", pushes, crate.Position)
	}
	if gap := crate.Position.X - 0.5 - c.Position().X; gap < 0.35-1e-6 {
		t.Fatalf("character entered the crate: centre gap %v", gap)
	}
	if math.Abs(crate.Position.Y-0.5) > 0.02 {
		t.Fatalf("pushing must not drive the crate into the floor: y = %v", crate.Position.Y)
	}

	// With pushing off, the crate is a wall.
	w2 := characterFloorWorld()
	crate2 := w2.AddBody(BodyConfig{Mass: 20, Position: Vec3{X: 2, Y: 0.5}})
	crate2.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
	c2 := w2.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}, DisablePush: true})
	settle(t, w2, c2)
	walk(w2, c2, 120, CharacterInput{Move: Vec3{X: 2}})
	if crate2.Position.X > 2.01 {
		t.Fatalf("DisablePush still moved the crate to %+v", crate2.Position)
	}
}

func TestCharacterLayersAndOtherCharacters(t *testing.T) {
	w := characterFloorWorld()
	const ghostWall = 1 << 5
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 3, Depth: 6, Offset: Vec3{X: 2, Y: 1.5}, Layer: ghostWall})
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}, Mask: LayerAll &^ ghostWall})
	other := w.AddCharacter(CharacterConfig{Position: Vec3{X: 5, Y: 1}})
	settle(t, w, c)
	settle(t, w, other)
	walk(w, c, 120, CharacterInput{Move: Vec3{X: 3}})
	// It walked through the masked wall and stopped at the other character.
	if x := c.Position().X; x < 5-0.7-0.03 || x > 5-0.7 {
		t.Fatalf("x = %v, want stopped a skin away from the other character at %v", x, 5-0.7)
	}
}

func TestCharacterRecoversFromGeometryThatMovedIntoIt(t *testing.T) {
	w := characterFloorWorld()
	door := w.AddBody(BodyConfig{IsKinematic: true, Position: Vec3{X: 3, Y: 1}})
	door.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.4, Height: 2, Depth: 2})
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
	settle(t, w, c)
	// The door slams through the character's position.
	door.Position = Vec3{X: 0.3, Y: 1}
	c.Step(characterDT, CharacterInput{})
	w.StepFixed()
	if gap := door.Position.X - 0.2 - c.Position().X; gap < 0.35-1e-6 {
		t.Fatalf("character still inside the door: gap %v at %+v", gap, c.Position())
	}
}

func TestCharacterSnapshotRestoreIsExact(t *testing.T) {
	build := func() (*World, *Character, *RigidBody) {
		w := characterFloorWorld()
		addStairs(w, 3, 0.2, 0.4, 4)
		platform := w.AddBody(BodyConfig{ID: "lift", IsKinematic: true, Position: Vec3{X: -3, Y: 0.5}})
		platform.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 0.2, Depth: 2})
		w.AddCollider(ColliderConfig{Shape: ShapeBox, IsTrigger: true, Width: 1, Height: 2, Depth: 3, Offset: Vec3{X: 1, Y: 1}})
		c := w.AddCharacter(CharacterConfig{ID: "hero", Position: Vec3{X: -3, Y: 2}, JumpSpeed: 5, CoyoteTime: 0.1, JumpBufferTime: 0.1})
		return w, c, platform
	}
	script := func(step int) CharacterInput {
		input := CharacterInput{Move: Vec3{X: 2.5 * math.Cos(float64(step)*0.01), Z: 0.4 * math.Sin(float64(step)*0.05)}}
		input.Jump = step%97 == 0
		return input
	}
	drive := func(w *World, c *Character, platform *RigidBody, step int) {
		platform.Position = Vec3{X: -3, Y: 0.5 + 0.4*math.Sin(float64(step)*0.03)}
		c.Step(characterDT, script(step))
		w.StepFixed()
	}

	w, c, platform := build()
	var snapshot []byte
	for step := 0; step < 600; step++ {
		if step == 300 {
			snapshot = w.Snapshot()
		}
		drive(w, c, platform, step)
	}
	want, _ := json.Marshal(w.StateSnapshot())

	w2, c2, platform2 := build()
	for step := 0; step < 300; step++ {
		drive(w2, c2, platform2, step+1000)
	}
	w2.Restore(snapshot)
	for step := 300; step < 600; step++ {
		drive(w2, c2, platform2, step)
	}
	got, _ := json.Marshal(w2.StateSnapshot())
	if string(got) != string(want) {
		t.Fatalf("restored run diverged:\n got %s\nwant %s", got, want)
	}
	if len(snapshot) == 0 || c.State().Index != c2.State().Index {
		t.Fatal("snapshot or character index missing")
	}
}

func TestCharacterStepDoesNotAllocateInSteadyState(t *testing.T) {
	w := characterFloorWorld()
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 3, Depth: 40, Offset: Vec3{X: 4, Y: 1.5}})
	addStairs(w, -6, 0.2, 0.4, 4)
	platform := w.AddBody(BodyConfig{IsKinematic: true, Position: Vec3{Z: 6, Y: 0.5}})
	platform.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 3, Height: 0.2, Depth: 3})
	walker := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
	rider := w.AddCharacter(CharacterConfig{Position: Vec3{Z: 6, Y: 2}})
	settle(t, w, walker)
	settle(t, w, rider)
	walk(w, walker, 120, CharacterInput{Move: Vec3{X: 3}})

	cases := map[string]func(){
		"walking into a wall": func() { walker.Step(characterDT, CharacterInput{Move: Vec3{X: 3, Z: 0.5}}) },
		"riding a platform": func() {
			platform.Position = platform.Position.Add(Vec3{X: 0.01})
			rider.Step(characterDT, CharacterInput{})
		},
	}
	for name, run := range cases {
		run()
		if allocs := testing.AllocsPerRun(200, run); allocs != 0 {
			t.Errorf("%s: Step allocates %.1f times", name, allocs)
		}
	}
}

func TestCharacterLifecycleAndState(t *testing.T) {
	w := characterFloorWorld()
	c := w.AddCharacter(CharacterConfig{ID: "a", Position: Vec3{Y: 1}, TotalHeight: 0.4, Radius: 0.3})
	if c.TotalHeight() != 0.6 || c.Radius() != 0.3 || c.Collider().Shape != ShapeCapsule || !c.Body().IsKinematic {
		t.Fatalf("a capsule shorter than its diameter becomes a sphere: height %v", c.TotalHeight())
	}
	b := w.AddCharacter(CharacterConfig{ID: "b", Position: Vec3{X: 3, Y: 1}})
	if w.CharacterCount() != 2 || w.CharacterAt(1) != b || len(w.Characters()) != 2 || b.Index() != 2 {
		t.Fatal("character list")
	}
	settle(t, w, b)
	state := b.State()
	b.Teleport(Vec3{X: 9, Y: 9})
	if b.Grounded() {
		t.Fatal("Teleport must clear the ground")
	}
	b.SetState(state)
	if !b.Grounded() || b.Position() != state.Position || b.GroundCollider() == nil {
		t.Fatal("SetState must restore position and ground")
	}
	b.SetVelocity(Vec3{Y: 4})
	b.Step(characterDT, CharacterInput{})
	if b.Grounded() {
		t.Fatal("an upward SetVelocity must lift a grounded character")
	}
	if !w.RemoveCharacter(c) || w.CharacterCount() != 1 || w.CharacterAt(0) != b {
		t.Fatal("RemoveCharacter")
	}
	if w.RemoveCharacter(c) {
		t.Fatal("removing twice must fail")
	}
}

func TestCharacterStepHeightIsTheLimit(t *testing.T) {
	for _, tc := range []struct {
		height float64
		climbs bool
	}{{0.28, true}, {0.32, false}, {0.34, false}} {
		w := characterFloorWorld()
		w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 20, Height: tc.height, Depth: 4, Offset: Vec3{X: 11, Y: tc.height / 2}})
		c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}, StepHeight: 0.3, Radius: 0.35})
		settle(t, w, c)
		walk(w, c, 120, CharacterInput{Move: Vec3{X: 3}})
		climbed := c.FootPosition().Y > tc.height
		if climbed != tc.climbs {
			t.Fatalf("ledge %.2f m with a 0.3 m step: climbed=%v, want %v (foot %+v)", tc.height, climbed, tc.climbs, c.FootPosition())
		}
	}
}

func TestCharacterCrossesMeshTerrainWithoutHitching(t *testing.T) {
	w := NewWorld(WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: characterDT})
	height := func(x, z float64) float64 { return 0.3*math.Sin(0.4*x) + 0.2*math.Cos(0.3*z) }
	w.AddCollider(terrainMeshConfig(40, 40, height))
	c := w.AddCharacter(CharacterConfig{Position: Vec3{X: -15, Y: 3}})
	settle(t, w, c)
	for i := 0; i < 360; i++ {
		c.Step(characterDT, CharacterInput{Move: Vec3{X: 4, Z: 1}})
		w.StepFixed()
		if !c.Grounded() {
			t.Fatalf("step %d: left the mesh terrain at %+v", i, c.FootPosition())
		}
		foot := c.FootPosition()
		// The capsule rests on the triangles, a skin above them, never inside.
		if surface := height(foot.X, foot.Z); foot.Y < surface-0.05 || foot.Y > surface+0.2 {
			t.Fatalf("step %d: foot %v far from the terrain surface %v", i, foot.Y, surface)
		}
	}
}
