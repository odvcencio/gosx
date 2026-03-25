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

	l := &islandLowerer{
		src:     prog,
		dst:     &program.Program{Name: comp.Name},
		nodeMap: make(map[NodeID]program.NodeID),
	}

	// Lower the node tree
	rootID, err := l.lowerNode(comp.Root)
	if err != nil {
		return nil, fmt.Errorf("lower %s: %w", comp.Name, err)
	}
	l.dst.Root = rootID

	// Compute static mask
	l.dst.StaticMask = make([]bool, len(l.dst.Nodes))
	for i, srcID := range l.srcIDs {
		if int(srcID) < len(prog.Nodes) {
			l.dst.StaticMask[i] = prog.Nodes[srcID].IsStatic
		}
	}

	return l.dst, nil
}

type islandLowerer struct {
	src     *Program
	dst     *program.Program
	nodeMap map[NodeID]program.NodeID
	srcIDs  []NodeID // tracks source node ID for each dst node
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

// addExpr adds a placeholder expression. Full parsing comes in Task 10.
// For now, treat expressions as simple signal/prop references.
func (l *islandLowerer) addExpr(source string) program.ExprID {
	id := program.ExprID(len(l.dst.Exprs))
	// Simple heuristic: if it looks like a signal or prop, create appropriate opcode
	// Full parsing in Task 10 (ir/exprparse.go)
	l.dst.Exprs = append(l.dst.Exprs, program.Expr{
		Op:    program.OpPropGet,
		Value: source,
		Type:  program.TypeAny,
	})
	return id
}
