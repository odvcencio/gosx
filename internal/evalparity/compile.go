package evalparity

import (
	"fmt"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// compileCase runs gosx.Compile on c's generated source — the same
// semantic gate transpile.Transpile itself calls before emitting Go (see
// transpile.go), and the compiled *ir.Program both the route and VM
// backends render from. A failure here means every backend is
// unsupported: there is no IR for route to walk or for ir.LowerIsland to
// lower, and transpile.Transpile would fail at the identical gate.
func compileCase(c Case) (*ir.Program, int, error) {
	prog, err := gosx.Compile(gsxSource(c))
	if err != nil {
		return nil, -1, err
	}
	name := componentName(c)
	for i, comp := range prog.Components {
		if comp.Name == name {
			return prog, i, nil
		}
	}
	return nil, -1, fmt.Errorf("compiled program has no component named %q", name)
}
