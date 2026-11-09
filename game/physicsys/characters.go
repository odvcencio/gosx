package physicsys

import (
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/physics"
)

// CharacterInputFunc returns what a character should do this fixed step. It
// runs once per character per fixed step, in character index order. Read the
// player's intent from ctx (ctx.Input, ctx.NetworkInputs or game state); do
// not read wall-clock time or render-frame values, or the result will differ
// between a server and a predicting client.
type CharacterInputFunc func(ctx *game.Context, c *physics.Character) physics.CharacterInput

// CharacterSystem steps every character of the runtime's physics world once
// per fixed step. Build it with Characters.
type CharacterSystem struct {
	name  string
	input CharacterInputFunc
	after func(ctx *game.Context, c *physics.Character, move physics.CharacterMove)

	world *physics.World
	moves []characterMove
}

type characterMove struct {
	character *physics.Character
	move      physics.CharacterMove
}

// CharacterOption configures a CharacterSystem.
type CharacterOption func(*CharacterSystem)

// AfterMove calls fn with each character's CharacterMove right after its step,
// which is where game code reacts to a landing, a jump or a climbed step
// (sounds, animation triggers, camera shake).
func AfterMove(fn func(ctx *game.Context, c *physics.Character, move physics.CharacterMove)) CharacterOption {
	return func(s *CharacterSystem) { s.after = fn }
}

// Named replaces the system name, which defaults to "physics.characters".
func Named(name string) CharacterOption {
	return func(s *CharacterSystem) {
		if name != "" {
			s.name = name
		}
	}
}

// Characters returns a PhaseFixedUpdate system that steps every character of
// the runtime's physics world (game.Context.Physics) with the input fn
// returns, using the world's fixed timestep. A nil fn steps every character
// with no input, which still applies gravity and platform carry.
//
// List it after the systems that move kinematic platforms and before the
// systems that read character positions. The runtime steps the physics world
// after every fixed-update system, so dynamic bodies respond to the
// characters' pushes in the same fixed step.
func Characters(fn CharacterInputFunc, options ...CharacterOption) *CharacterSystem {
	s := &CharacterSystem{name: "physics.characters", input: fn}
	for _, option := range options {
		option(s)
	}
	return s
}

// Name implements game.System.
func (s *CharacterSystem) Name() string { return s.name }

// Phase implements game.System.
func (s *CharacterSystem) Phase() game.Phase { return game.PhaseFixedUpdate }

// Update implements game.System. It steps each character and records its
// move. A steady-state update allocates nothing.
func (s *CharacterSystem) Update(ctx *game.Context) error {
	if ctx == nil || ctx.Physics == nil {
		return nil
	}
	world := ctx.Physics
	s.world = world
	dt := world.FixedTimestep()
	count := world.CharacterCount()
	if cap(s.moves) < count {
		s.moves = make([]characterMove, 0, count)
	}
	clear(s.moves)
	s.moves = s.moves[:0]
	for i := 0; i < count; i++ {
		c := world.CharacterAt(i)
		var input physics.CharacterInput
		if s.input != nil {
			input = s.input(ctx, c)
		}
		move := c.Step(dt, input)
		s.moves = append(s.moves, characterMove{character: c, move: move})
		if s.after != nil {
			s.after(ctx, c, move)
		}
	}
	return nil
}

// LastMove returns what c did in the most recent fixed step the system ran,
// and false when the system did not step it.
func (s *CharacterSystem) LastMove(c *physics.Character) (physics.CharacterMove, bool) {
	for i := range s.moves {
		if s.moves[i].character == c {
			return s.moves[i].move, true
		}
	}
	return physics.CharacterMove{}, false
}
