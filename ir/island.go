package ir

import (
	"fmt"

	"github.com/odvcencio/gosx/island/program"
)

// LowerIsland converts an IR component to an IslandProgram.
// The component must have IsIsland == true.
func LowerIsland(prog *Program, compIdx int) (*program.Program, error) {
	if compIdx >= len(prog.Components) {
		return nil, fmt.Errorf("component index %d out of range", compIdx)
	}
	comp := prog.Components[compIdx]
	if !comp.IsIsland {
		return nil, fmt.Errorf("component %q is not an island", comp.Name)
	}

	// Build expression scope from the component's node tree AND the body analyzer.
	// If the component has a Scope (from body analysis), merge it into the
	// expression scope so identifiers resolve correctly.
	scope := buildIslandScope(prog, comp)
	if comp.Scope != nil {
		for _, sig := range comp.Scope.Signals {
			scope.Signals[sig.Name] = true
		}
		for _, c := range comp.Scope.Computeds {
			scope.Signals[c.Name] = true // computeds read like signals
		}
		for _, h := range comp.Scope.Handlers {
			scope.Handlers[h.Name] = true
		}
	}

	l := &islandLowerer{
		src:     prog,
		dst:     &program.Program{Name: comp.Name},
		nodeMap: make(map[NodeID]program.NodeID),
		scope:   scope,
	}

	// Lower the node tree
	rootID, err := l.lowerNode(comp.Root)
	if err != nil {
		return nil, fmt.Errorf("lower %s: %w", comp.Name, err)
	}
	l.dst.Root = rootID

	// Generate SignalDef, ComputedDef, Handler entries from the body analyzer.
	if comp.Scope != nil {
		for _, sig := range comp.Scope.Signals {
			// Parse the init expression into opcodes
			initExprs, initID, err := ParseExpr(sig.InitExpr, scope)
			if err != nil {
				// Fallback: literal init
				initID = l.addExprDirect(program.Expr{
					Op:    program.OpLitString,
					Value: sig.InitExpr,
					Type:  program.TypeAny,
				})
			} else {
				initID = l.appendExprs(initExprs, initID)
			}

			exprType := typeHintToExprType(sig.TypeHint)
			l.dst.Signals = append(l.dst.Signals, program.SignalDef{
				Name: sig.Name,
				Type: exprType,
				Init: initID,
			})
		}

		for _, comp := range comp.Scope.Computeds {
			bodyExprs, bodyID, err := ParseExpr(comp.BodyExpr, scope)
			if err != nil {
				bodyID = l.addExprDirect(program.Expr{
					Op:    program.OpPropGet,
					Value: comp.BodyExpr,
					Type:  program.TypeAny,
				})
			} else {
				bodyID = l.appendExprs(bodyExprs, bodyID)
			}

			l.dst.Computeds = append(l.dst.Computeds, program.ComputedDef{
				Name: comp.Name,
				Type: program.TypeAny,
				Expr: bodyID,
			})
		}

		for _, handler := range comp.Scope.Handlers {
			h := program.Handler{Name: handler.Name}
			for _, stmtSource := range handler.Statements {
				stmtExprs, stmtID, err := ParseExpr(stmtSource, scope)
				if err != nil {
					continue // skip unparseable statements
				}
				stmtID = l.appendExprs(stmtExprs, stmtID)
				h.Body = append(h.Body, stmtID)
			}
			l.dst.Handlers = append(l.dst.Handlers, h)
		}
	}

	// Compute static mask
	l.dst.StaticMask = make([]bool, len(l.dst.Nodes))
	for i, srcID := range l.srcIDs {
		if int(srcID) < len(prog.Nodes) {
			l.dst.StaticMask[i] = prog.Nodes[srcID].IsStatic
		}
	}

	return l.dst, nil
}

// buildIslandScope extracts signal, prop, and handler names from the component's
// node tree to build the expression scope needed for parsing island expressions.
func buildIslandScope(prog *Program, comp Component) *ExprScope {
	scope := &ExprScope{
		Signals:  make(map[string]bool),
		Props:    make(map[string]bool),
		Handlers: make(map[string]bool),
	}

	// Scan the component's nodes for event handler references
	var walkNodes func(id NodeID)
	walkNodes = func(id NodeID) {
		if int(id) >= len(prog.Nodes) {
			return
		}
		node := prog.Nodes[id]
		for _, attr := range node.Attrs {
			if attr.IsEvent {
				scope.Handlers[attr.Expr] = true
			}
		}
		for _, child := range node.Children {
			walkNodes(child)
		}
	}
	walkNodes(comp.Root)

	// Expression text that appears as identifiers could be signals or props.
	// Without full type analysis, we treat all expression identifiers as props
	// by default — the expression parser will resolve them against scope.

	return scope
}

type islandLowerer struct {
	src     *Program
	dst     *program.Program
	nodeMap map[NodeID]program.NodeID
	srcIDs  []NodeID // tracks source node ID for each dst node
	scope   *ExprScope
}

func (l *islandLowerer) lowerNode(srcID NodeID) (program.NodeID, error) {
	if mapped, ok := l.nodeMap[srcID]; ok {
		return mapped, nil
	}

	if int(srcID) >= len(l.src.Nodes) {
		return 0, fmt.Errorf("node %d not found", srcID)
	}
	srcNode := l.src.NodeAt(srcID)

	// Check NodeID overflow (uint32 -> uint16)
	if len(l.dst.Nodes) >= 65535 {
		return 0, fmt.Errorf("island exceeds 65,535 node limit")
	}

	dstID := program.NodeID(len(l.dst.Nodes))
	l.nodeMap[srcID] = dstID

	// Pre-allocate the slot
	l.dst.Nodes = append(l.dst.Nodes, program.Node{})
	l.srcIDs = append(l.srcIDs, srcID)

	var node program.Node

	switch srcNode.Kind {
	case NodeElement:
		node.Kind = program.NodeElement
		node.Tag = srcNode.Tag
		// Lower attributes
		for _, attr := range srcNode.Attrs {
			dstAttr, err := l.lowerAttr(attr)
			if err != nil {
				return 0, err
			}
			node.Attrs = append(node.Attrs, dstAttr)
		}
	case NodeComponent:
		// Components in islands are rendered as elements
		node.Kind = program.NodeElement
		node.Tag = "div"
	case NodeText:
		node.Kind = program.NodeText
		node.Text = srcNode.Text
	case NodeExpr:
		node.Kind = program.NodeExpr
		// For now, store expression text as a simple signal/prop reference
		// Full expression parsing comes in Task 10
		exprID := l.addExpr(srcNode.Text)
		node.Expr = exprID
	case NodeFragment:
		node.Kind = program.NodeFragment
	case NodeRawHTML:
		node.Kind = program.NodeText
		node.Text = srcNode.Text
	}

	// Lower children
	for _, childSrcID := range srcNode.Children {
		childDstID, err := l.lowerNode(childSrcID)
		if err != nil {
			return 0, err
		}
		node.Children = append(node.Children, childDstID)
	}

	l.dst.Nodes[dstID] = node
	return dstID, nil
}

func (l *islandLowerer) lowerAttr(attr Attr) (program.Attr, error) {
	switch attr.Kind {
	case AttrStatic:
		return program.Attr{
			Kind:  program.AttrStatic,
			Name:  attr.Name,
			Value: attr.Value,
		}, nil
	case AttrBool:
		return program.Attr{
			Kind: program.AttrBool,
			Name: attr.Name,
		}, nil
	case AttrExpr:
		if attr.IsEvent {
			return program.Attr{
				Kind:  program.AttrEvent,
				Name:  attr.Name,
				Event: attr.Expr, // handler name from expression
			}, nil
		}
		// Expression attribute -- add to expr table
		exprID := l.addExpr(attr.Expr)
		return program.Attr{
			Kind: program.AttrExpr,
			Name: attr.Name,
			Expr: exprID,
		}, nil
	case AttrSpread:
		return program.Attr{}, fmt.Errorf("spread attributes are not allowed in island components")
	default:
		return program.Attr{}, fmt.Errorf("unknown attr kind: %d", attr.Kind)
	}
}

// addExpr parses a Go expression source string into typed opcodes and appends
// them to the island program's expression table. Returns the root ExprID.
func (l *islandLowerer) addExpr(source string) program.ExprID {
	baseID := program.ExprID(len(l.dst.Exprs))

	exprs, rootID, err := ParseExpr(source, l.scope)
	if err != nil {
		// If parsing fails, fall back to a simple prop/signal reference
		id := program.ExprID(len(l.dst.Exprs))
		l.dst.Exprs = append(l.dst.Exprs, program.Expr{
			Op:    program.OpPropGet,
			Value: source,
			Type:  program.TypeAny,
		})
		return id
	}

	// Append all parsed expressions, adjusting IDs by the base offset
	for _, e := range exprs {
		adjusted := e
		// Offset operand references by baseID
		if len(adjusted.Operands) > 0 {
			ops := make([]program.ExprID, len(adjusted.Operands))
			for i, op := range adjusted.Operands {
				ops[i] = op + baseID
			}
			adjusted.Operands = ops
		}
		l.dst.Exprs = append(l.dst.Exprs, adjusted)
	}

	return rootID + baseID
}

// addExprDirect appends a single expression to the program and returns its ID.
func (l *islandLowerer) addExprDirect(e program.Expr) program.ExprID {
	id := program.ExprID(len(l.dst.Exprs))
	l.dst.Exprs = append(l.dst.Exprs, e)
	return id
}

// appendExprs appends parsed expressions to the program, offsetting operand
// references, and returns the adjusted root ID.
func (l *islandLowerer) appendExprs(exprs []program.Expr, rootID program.ExprID) program.ExprID {
	baseID := program.ExprID(len(l.dst.Exprs))

	for _, e := range exprs {
		adjusted := e
		if len(adjusted.Operands) > 0 {
			ops := make([]program.ExprID, len(adjusted.Operands))
			for i, op := range adjusted.Operands {
				ops[i] = op + baseID
			}
			adjusted.Operands = ops
		}
		l.dst.Exprs = append(l.dst.Exprs, adjusted)
	}

	return rootID + baseID
}

// typeHintToExprType converts a type hint string to an ExprType.
func typeHintToExprType(hint string) program.ExprType {
	switch hint {
	case "int":
		return program.TypeInt
	case "float":
		return program.TypeFloat
	case "string":
		return program.TypeString
	case "bool":
		return program.TypeBool
	default:
		return program.TypeAny
	}
}
