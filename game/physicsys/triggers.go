package physicsys

import (
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/physics"
)

// Game event types Triggers emits when TriggerHandlers.Emit is set.
const (
	EventTriggerEnter = "physics.trigger.enter"
	EventTriggerStay  = "physics.trigger.stay"
	EventTriggerExit  = "physics.trigger.exit"
)

// TriggerHandlers receives trigger events. Each handler is optional. A handler
// runs inside the physics step that produced the event, after that step has
// moved every body; ctx is the context of the fixed step, so ctx.Tick names
// it. Handlers may add or remove bodies and colliders, which take effect from
// the next step.
type TriggerHandlers struct {
	Enter func(ctx *game.Context, event physics.TriggerEvent)
	Stay  func(ctx *game.Context, event physics.TriggerEvent)
	Exit  func(ctx *game.Context, event physics.TriggerEvent)

	// Emit also records each enter and exit as a game.Event (types
	// EventTriggerEnter and EventTriggerExit), so Runtime.Events lists them
	// after the frame. Target is the ID of the trigger's body, when it has
	// one, and Data the physics.TriggerEvent.
	Emit bool
	// EmitStay also emits EventTriggerStay. A stay event happens every step
	// for every overlap, and each emitted event allocates, so leave it off
	// unless something reads them.
	EmitStay bool
}

// TriggerSystem delivers a physics world's trigger events. Build it with
// Triggers.
type TriggerSystem struct {
	name     string
	handlers TriggerHandlers
	world    *physics.World
	ctx      *game.Context
	deliver  func(physics.TriggerEvent)
}

// Triggers returns a PhaseFixedUpdate system that delivers the trigger events
// of the runtime's physics world (game.Context.Physics) to handlers. Add it
// to game.Config.Systems; the runtime must be the one stepping the world.
func Triggers(handlers TriggerHandlers) *TriggerSystem {
	s := &TriggerSystem{name: "physics.triggers", handlers: handlers}
	s.deliver = s.dispatch
	return s
}

// Name implements game.System.
func (s *TriggerSystem) Name() string { return s.name }

// Phase implements game.System.
func (s *TriggerSystem) Phase() game.Phase { return game.PhaseFixedUpdate }

// Update implements game.System. It records the fixed step's context and
// installs the world's trigger handler; the delivery itself happens inside the
// physics step that follows.
func (s *TriggerSystem) Update(ctx *game.Context) error {
	if ctx == nil {
		return nil
	}
	s.ctx = ctx
	if ctx.Physics != s.world {
		if s.world != nil {
			s.world.SetTriggerHandler(nil)
		}
		s.world = ctx.Physics
		if s.world != nil {
			s.world.SetTriggerHandler(s.deliver)
		}
	}
	return nil
}

// Close removes the trigger handler from the world.
func (s *TriggerSystem) Close() {
	if s.world != nil {
		s.world.SetTriggerHandler(nil)
		s.world = nil
	}
}

func (s *TriggerSystem) dispatch(event physics.TriggerEvent) {
	ctx := s.ctx
	var handler func(*game.Context, physics.TriggerEvent)
	emitType := ""
	switch event.Phase {
	case physics.TriggerEnter:
		handler = s.handlers.Enter
		if s.handlers.Emit {
			emitType = EventTriggerEnter
		}
	case physics.TriggerStay:
		handler = s.handlers.Stay
		if s.handlers.Emit && s.handlers.EmitStay {
			emitType = EventTriggerStay
		}
	case physics.TriggerExit:
		handler = s.handlers.Exit
		if s.handlers.Emit {
			emitType = EventTriggerExit
		}
	}
	if handler != nil {
		handler(ctx, event)
	}
	if emitType != "" && ctx != nil {
		target := ""
		if body := event.Trigger.Body; body != nil {
			target = body.ID
		}
		ctx.Emit(game.Event{Type: emitType, Target: target, Data: event})
	}
}
