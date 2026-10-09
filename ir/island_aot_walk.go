//go:build !tinygo

package ir

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

type aotSourceProp struct {
	caller Component
	source string
	target aot.ScalarKind
}

// Validate all reachable source payloads before tree lowering can discard a
// prop, slot, handler reference or projection. Slot contents retain the caller
// scope; a component body and its declarations use the callee's scope.
func aotWalkSource(src *Program, root Component) ([]aotSourceProp, error) {
	var props []aotSourceProp
	active := make(map[NodeID]bool)
	var visit func(NodeID, Component, int) error
	visit = func(id NodeID, owner Component, depth int) error {
		if int(id) >= len(src.Nodes) || depth > 64 || active[id] {
			return fmt.Errorf("invalid source node graph")
		}
		active[id] = true
		defer delete(active, id)
		n := src.Nodes[id]
		if n.Kind == NodeExpr {
			if err := aotSourceTokens(src, owner, n.Text); err != nil {
				return err
			}
		}
		for _, attr := range n.Attrs {
			if attr.Expr != "" {
				if err := aotSourceTokens(src, owner, attr.Expr); err != nil {
					return err
				}
			}
			if attr.Kind == AttrExpr && attr.IsEvent && !aotNamedHandler(owner, attr.Expr) {
				return fmt.Errorf("event attribute has no proved handler binding")
			}
			if _, inline := legacyInlineEventType(attr.Name); attr.Kind == AttrStatic && inline {
				source := strings.TrimSpace(attr.Value)
				if decoded, err := strconv.Unquote(`"` + source + `"`); err == nil {
					source = decoded
				}
				if err := aotSourceTokens(src, owner, source); err != nil {
					return err
				}
			}
		}
		for _, child := range n.Children {
			if err := visit(child, owner, depth+1); err != nil {
				return err
			}
		}
		for _, name := range sortedNodeSlotNames(n.Slots) {
			if err := visit(n.Slots[name], owner, depth+1); err != nil {
				return err
			}
		}
		if n.Kind == NodeComponent && !n.IsSyntheticConditional() {
			for _, callee := range src.Components {
				if callee.Name != n.Tag {
					continue
				}
				names := make([]string, 0, len(callee.PropsFields))
				for name := range callee.PropsFields {
					names = append(names, name)
				}
				slices.Sort(names)
				for _, name := range names {
					if aotSourceKind(src, callee.PropsFields[name]) == "" {
						return fmt.Errorf("composed prop has an unproved scalar type")
					}
				}
				for _, attr := range n.Attrs {
					source := attr.Expr
					switch attr.Kind {
					case AttrStatic:
						source = strconv.Quote(attr.Value)
					case AttrBool:
						source = "true"
					}
					if err := aotSourceTokens(src, owner, source); err != nil {
						return err
					}
					props = append(props, aotSourceProp{owner, source, aotSourceKind(src, callee.PropsFields[attr.Name])})
				}
				if err := aotWalkScope(src, callee); err != nil {
					return err
				}
				if err := visit(callee.Root, callee, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := aotWalkScope(src, root); err != nil {
		return nil, err
	}
	if err := visit(root.Root, root, 1); err != nil {
		return nil, err
	}
	return props, nil
}

func aotWalkScope(src *Program, comp Component) error {
	if comp.Scope == nil {
		return nil
	}
	for _, sig := range comp.Scope.Signals {
		if err := aotSourceTokens(src, comp, sig.InitExpr); err != nil {
			return err
		}
	}
	for _, computed := range comp.Scope.Computeds {
		if err := aotSourceTokens(src, comp, computed.BodyExpr); err != nil {
			return err
		}
	}
	for _, handler := range comp.Scope.Handlers {
		for _, statement := range handler.Statements {
			if err := aotSourceTokens(src, comp, statement); err != nil {
				return err
			}
		}
	}
	return nil
}

func aotNamedHandler(comp Component, source string) bool {
	if comp.Scope != nil {
		for _, handler := range comp.Scope.Handlers {
			if strings.TrimSpace(source) == handler.Name {
				return true
			}
		}
	}
	return false
}

// Prop substitution does not retain a destination-type annotation. Check
// assignments before substitution, using the same scalar inference as the
// final graph. A conversion that would change the emitted kind stays on VM.
func aotCheckSourceProps(src *Program, props []aotSourceProp, states map[string]aot.ScalarKind) error {
	for _, prop := range props {
		if prop.target == "" {
			return fmt.Errorf("composed prop has an unproved scalar type")
		}
		exprs, root, err := ParseExpr(prop.source, mergedIslandScope(src, prop.caller))
		if err != nil {
			return fmt.Errorf("composed prop has no proved expression lowering: %w", err)
		}
		p := &program.Program{Exprs: exprs}
		kinds, constants, _, err := aotInferExpressions(src, prop.caller, p, &aot.ScalarContract{}, states)
		if err != nil {
			return err
		}
		if !aotValueFits(kinds[root], constants[root], prop.target) || kinds[root] != prop.target {
			return fmt.Errorf("composed prop does not preserve its declared scalar kind")
		}
	}
	return nil
}
