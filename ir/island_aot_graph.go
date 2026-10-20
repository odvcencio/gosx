//go:build !tinygo && !js

package ir

import (
	"fmt"

	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

// Measure longest paths on the emitted graph before checking any types.
// Memoized heights describe the whole suffix, independently of visit order.
func aotGraphLimits(p *program.Program) error {
	l := aot.ProfileLimits()
	if len(p.Nodes) > int(l.Nodes) || len(p.Exprs) > int(l.Expressions) || len(p.Signals) > int(l.Signals) || len(p.Computeds) > int(l.Computeds) || len(p.Handlers) > int(l.Handlers) {
		return fmt.Errorf("graph_limit: scalar profile counts")
	}
	height := func(count int, edges func(int) []int, limit uint32) error {
		heights := make([]uint32, count)
		active := make([]bool, count)
		var walk func(int) (uint32, error)
		walk = func(id int) (uint32, error) {
			if id < 0 || id >= count || active[id] {
				return 0, fmt.Errorf("graph_limit: invalid or cyclic graph")
			}
			if heights[id] != 0 {
				return heights[id], nil
			}
			active[id] = true
			defer func() { active[id] = false }()
			h := uint32(1)
			for _, child := range edges(id) {
				ch, err := walk(child)
				if err != nil {
					return 0, err
				}
				if ch+1 > h {
					h = ch + 1
				}
			}
			if h > limit {
				return 0, fmt.Errorf("graph_limit: depth %d exceeds %d", h, limit)
			}
			heights[id] = h
			return h, nil
		}
		for id := 0; id < count; id++ {
			if _, err := walk(id); err != nil {
				return err
			}
		}
		return nil
	}
	if err := height(len(p.Nodes), func(id int) []int {
		var children []int
		for _, child := range p.Nodes[id].Children {
			children = append(children, int(child))
		}
		return children
	}, l.NodeDepth); err != nil {
		return err
	}
	if err := height(len(p.Exprs), func(id int) []int {
		var children []int
		for _, child := range p.Exprs[id].Operands {
			children = append(children, int(child))
		}
		return children
	}, l.ExpressionDepth); err != nil {
		return err
	}
	computeds := map[string]int{}
	for id, c := range p.Computeds {
		computeds[c.Name] = id
	}
	return height(len(p.Computeds), func(id int) []int {
		var dependencies []int
		seen := map[program.ExprID]bool{}
		var walk func(program.ExprID)
		walk = func(expr program.ExprID) {
			if seen[expr] {
				return
			}
			seen[expr] = true
			e := p.Exprs[expr]
			if e.Op == program.OpSignalGet {
				if dep, ok := computeds[e.Value]; ok {
					dependencies = append(dependencies, dep)
				}
			}
			for _, operand := range e.Operands {
				walk(operand)
			}
		}
		walk(p.Computeds[id].Expr)
		return dependencies
	}, l.ComputedDepth)
}
