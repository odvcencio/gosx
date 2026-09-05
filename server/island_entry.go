package server

import (
	"fmt"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
)

// IslandRenderContext is a request-local generated-call context. It is valid
// only during RenderIslandEntry. Node payloads never enter runtime props.
type IslandRenderContext struct {
	renderer *island.Renderer
	err      error
	closed   bool
}

// IslandJSON consumes a compiler-emitted program literal; the renderer still
// verifies its exact JSON or binary asset identity before registration.
func (c *IslandRenderContext) IslandJSON(data string, props any, slots island.ServerSlots) gosx.Node {
	if c == nil || c.closed || c.err != nil {
		return gosx.Node{}
	}
	p, err := program.DecodeJSON([]byte(data))
	if err != nil {
		c.err = fmt.Errorf("island entry: generated program: %w", err)
		return gosx.Node{}
	}
	return c.Island(p, props, slots)
}

// Island registers a generated program, retaining the first registration error.
// Generated local calls share this explicit context, never an ambient runtime.
func (c *IslandRenderContext) Island(prog *program.Program, props any, slots island.ServerSlots) gosx.Node {
	if c == nil || c.closed || c.err != nil {
		return gosx.Node{}
	}
	node, err := c.renderer.RenderRegisteredProgram(prog, props, slots)
	if err != nil {
		c.err = err
	}
	return node
}

// RenderIslandEntry commits renderer-owned registrations only after a generated
// entry succeeds. Authored Go side effects are outside this transaction.
func (r *PageRuntime) RenderIslandEntry(render func(*IslandRenderContext) gosx.Node) (gosx.Node, error) {
	if r == nil || r.renderer == nil || render == nil {
		return gosx.Node{}, fmt.Errorf("island entry: missing PageRuntime or callback")
	}
	if r.renderingIslandEntry {
		return gosx.Node{}, fmt.Errorf("island entry: reentrant public entry; generated calls must share the explicit context")
	}
	r.renderingIslandEntry = true
	defer func() { r.renderingIslandEntry = false }()
	node, err := r.renderer.WithRegistrations(func(stage *island.Renderer) (gosx.Node, error) {
		ctx := &IslandRenderContext{renderer: stage}
		defer func() { ctx.closed = true }()
		node := render(ctx)
		return node, ctx.err
	})
	if err == nil && len(r.renderer.Manifest().Islands) != 0 {
		r.active = true
	}
	return node, err
}
