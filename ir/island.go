package ir

import (
	"fmt"
	"strconv"

	"github.com/odvcencio/gosx/island/program"
)

// LowerIsland converts an IR component to an IslandProgram.
// The component must have IsIsland == true.
func LowerIsland(prog *Program, compIdx int) (*program.Program, error) {
	comp, err := islandComponent(prog, compIdx)
	if err != nil {
		return nil, err
	}

	scope := mergedIslandScope(prog, comp)
	l := newIslandLowerer(prog, comp.Name, scope)

	if err := l.lowerComponent(comp); err != nil {
		return nil, err
	}
	if err := l.emitComponentScope(comp.Scope); err != nil {
		return nil, err
	}
	l.populateStaticMask()

	return l.dst, nil
}

func islandComponent(prog *Program, compIdx int) (Component, error) {
	if compIdx >= len(prog.Components) {
		return Component{}, fmt.Errorf("component index %d out of range", compIdx)
	}
	comp := prog.Components[compIdx]
	if !comp.IsIsland {
		return Component{}, fmt.Errorf("component %q is not an island", comp.Name)
	}
	return comp, nil
}

func mergedIslandScope(prog *Program, comp Component) *ExprScope {
	scope := buildIslandScope(prog, comp)
	applyComponentScope(scope, comp.Scope)
	return scope
}

func applyComponentScope(scope *ExprScope, compScope *ComponentScope) {
	if scope == nil || compScope == nil {
		return
	}
	for _, sig := range compScope.Signals {
		scope.Signals[sig.Name] = true
		if sig.Local != "" {
			scope.SignalAliases[sig.Local] = sig.Name
		}
	}
	for _, computed := range compScope.Computeds {
		scope.Signals[computed.Name] = true
	}
	for _, handler := range compScope.Handlers {
		scope.Handlers[handler.Name] = true
	}
}

// buildIslandScope extracts signal, prop, and handler names from the component's
// node tree to build the expression scope needed for parsing island expressions.
func buildIslandScope(prog *Program, comp Component) *ExprScope {
	scope := &ExprScope{
		Signals:       make(map[string]bool),
		SignalAliases: make(map[string]string),
		Props:         make(map[string]bool),
		Handlers:      make(map[string]bool),
		EventFields:   make(map[string]bool),
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

func cloneExprScope(scope *ExprScope) *ExprScope {
	if scope == nil {
		return &ExprScope{
			Signals:       make(map[string]bool),
			SignalAliases: make(map[string]string),
			Props:         make(map[string]bool),
			Handlers:      make(map[string]bool),
			EventFields:   make(map[string]bool),
		}
	}

	next := &ExprScope{
		Signals:       make(map[string]bool, len(scope.Signals)),
		SignalAliases: make(map[string]string, len(scope.SignalAliases)),
		Props:         make(map[string]bool, len(scope.Props)),
		Handlers:      make(map[string]bool, len(scope.Handlers)),
		EventFields:   make(map[string]bool, len(scope.EventFields)),
	}
	for key, value := range scope.Signals {
		next.Signals[key] = value
	}
	for key, value := range scope.SignalAliases {
		next.SignalAliases[key] = value
	}
	for key, value := range scope.Props {
		next.Props[key] = value
	}
	for key, value := range scope.Handlers {
		next.Handlers[key] = value
	}
	for key, value := range scope.EventFields {
		next.EventFields[key] = value
	}
	return next
}

type islandLowerer struct {
	src     *Program
	dst     *program.Program
	nodeMap map[NodeID]program.NodeID
	srcIDs  []NodeID // tracks source node ID for each dst node
	scope   *ExprScope
}

func newIslandLowerer(src *Program, name string, scope *ExprScope) *islandLowerer {
	return &islandLowerer{
		src:     src,
		dst:     &program.Program{Name: name},
		nodeMap: make(map[NodeID]program.NodeID),
		scope:   scope,
	}
}

func (l *islandLowerer) lowerComponent(comp Component) error {
	rootID, err := l.lowerNode(comp.Root)
	if err != nil {
		return fmt.Errorf("lower %s: %w", comp.Name, err)
	}
	l.dst.Root = rootID
	return nil
}

func (l *islandLowerer) emitComponentScope(scope *ComponentScope) error {
	if scope == nil {
		return nil
	}
	l.emitSignalDefs(scope.Signals)
	l.emitComputedDefs(scope.Computeds)
	return l.emitHandlerDefs(scope.Handlers)
}

func (l *islandLowerer) emitSignalDefs(signals []SignalInfo) {
	for _, sig := range signals {
		initID := l.parseExprOrFallback(sig.InitExpr, l.scope, program.Expr{
			Op:    program.OpLitString,
			Value: sig.InitExpr,
			Type:  program.TypeAny,
		})
		l.dst.Signals = append(l.dst.Signals, program.SignalDef{
			Name: sig.Name,
			Type: typeHintToExprType(sig.TypeHint),
			Init: initID,
		})
	}
}

func (l *islandLowerer) emitComputedDefs(computeds []ComputedInfo) {
	for _, computed := range computeds {
		bodyID := l.parseExprOrFallback(computed.BodyExpr, l.scope, program.Expr{
			Op:    program.OpPropGet,
			Value: computed.BodyExpr,
			Type:  program.TypeAny,
		})
		l.dst.Computeds = append(l.dst.Computeds, program.ComputedDef{
			Name: computed.Name,
			Type: program.TypeAny,
			Expr: bodyID,
		})
	}
}

func (l *islandLowerer) emitHandlerDefs(handlers []HandlerInfo) error {
	handlerScope := handlerExprScope(l.scope)
	for _, handler := range handlers {
		h := program.Handler{Name: handler.Name}
		for _, stmtSource := range handler.Statements {
			stmtExprs, stmtID, err := ParseExpr(stmtSource, handlerScope)
			if err != nil {
				return fmt.Errorf("parse handler %s statement %q: %w", handler.Name, stmtSource, err)
			}
			h.Body = append(h.Body, l.appendExprs(stmtExprs, stmtID))
		}
		l.dst.Handlers = append(l.dst.Handlers, h)
	}
	return nil
}

func handlerExprScope(scope *ExprScope) *ExprScope {
	handlerScope := cloneExprScope(scope)
	handlerScope.EventFields["value"] = true
	handlerScope.EventFields["checked"] = true
	handlerScope.EventFields["key"] = true
	handlerScope.EventFields["selectedIndex"] = true
	return handlerScope
}

func (l *islandLowerer) parseExprOrFallback(source string, scope *ExprScope, fallback program.Expr) program.ExprID {
	exprs, rootID, err := ParseExpr(source, scope)
	if err != nil {
		return l.addExprDirect(fallback)
	}
	return l.appendExprs(exprs, rootID)
}

func (l *islandLowerer) populateStaticMask() {
	l.dst.StaticMask = make([]bool, len(l.dst.Nodes))
	for i, srcID := range l.srcIDs {
		if int(srcID) < len(l.src.Nodes) {
			l.dst.StaticMask[i] = l.src.Nodes[srcID].IsStatic
		}
	}
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
		if isEachComponent(srcNode.Tag) {
			var err error
			node, err = l.lowerEachNode(srcNode)
			if err != nil {
				return 0, err
			}
			break
		}
		if isConditionalComponent(srcNode.Tag) {
			var err error
			node, err = l.lowerConditionalNode(srcNode)
			if err != nil {
				return 0, err
			}
			break
		}
		return 0, fmt.Errorf("component <%s> is not supported inside island components yet", srcNode.Tag)
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
	childScope := l.scope
	if node.Kind == program.NodeForEach {
		childScope = l.scopeForEach(node)
	}
	for _, childSrcID := range srcNode.Children {
		childDstID, err := l.lowerNodeWithScope(childSrcID, childScope)
		if err != nil {
			return 0, err
		}
		node.Children = append(node.Children, childDstID)
	}

	l.dst.Nodes[dstID] = node
	return dstID, nil
}

func (l *islandLowerer) lowerNodeWithScope(srcID NodeID, scope *ExprScope) (program.NodeID, error) {
	prev := l.scope
	l.scope = scope
	defer func() {
		l.scope = prev
	}()
	return l.lowerNode(srcID)
}

func (l *islandLowerer) lowerEachNode(srcNode *Node) (program.Node, error) {
	collectionExpr := eachAttrSource(srcNode.Attrs, "of", "each", "items")
	if collectionExpr == "" {
		return program.Node{}, fmt.Errorf("%s requires an of/each/items attribute", srcNode.Tag)
	}

	exprID := l.addExpr(collectionExpr)
	node := program.Node{
		Kind: program.NodeForEach,
		Expr: exprID,
	}

	itemName := eachStaticAttrValue(srcNode.Attrs, "as", "item")
	if itemName == "" {
		itemName = "item"
	}
	node.Attrs = append(node.Attrs, program.Attr{
		Kind:  program.AttrStatic,
		Name:  "as",
		Value: itemName,
	})

	if indexName := eachStaticAttrValue(srcNode.Attrs, "index"); indexName != "" {
		node.Attrs = append(node.Attrs, program.Attr{
			Kind:  program.AttrStatic,
			Name:  "index",
			Value: indexName,
		})
	}

	if fallbackSource := eachAttrSource(srcNode.Attrs, "fallback", "empty"); fallbackSource != "" {
		node.Attrs = append(node.Attrs, program.Attr{
			Kind: program.AttrExpr,
			Name: "fallback",
			Expr: l.addExpr(fallbackSource),
		})
	}

	return node, nil
}

func (l *islandLowerer) lowerConditionalNode(srcNode *Node) (program.Node, error) {
	conditionExpr := islandAttrSource(srcNode.Attrs, "when", "if", "cond", "test")
	if conditionExpr == "" {
		return program.Node{}, fmt.Errorf("%s requires a when/if/cond/test attribute", srcNode.Tag)
	}

	node := program.Node{
		Kind: program.NodeConditional,
		Expr: l.addExpr(conditionExpr),
	}
	if fallbackSource := islandAttrSource(srcNode.Attrs, "fallback", "else"); fallbackSource != "" {
		node.Attrs = append(node.Attrs, program.Attr{
			Kind: program.AttrExpr,
			Name: "fallback",
			Expr: l.addExpr(fallbackSource),
		})
	}
	return node, nil
}

func (l *islandLowerer) scopeForEach(node program.Node) *ExprScope {
	scope := cloneExprScope(l.scope)
	itemName := forEachStaticAttr(node.Attrs, "as")
	if itemName == "" {
		itemName = "item"
	}
	scope.Props[itemName] = true
	scope.Props["_item"] = true
	scope.Props[itemName+"Key"] = true
	scope.Props["_key"] = true

	indexName := forEachStaticAttr(node.Attrs, "index")
	if indexName != "" {
		scope.Props[indexName] = true
	}
	scope.Props["_index"] = true
	return scope
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

func isEachComponent(tag string) bool {
	switch tag {
	case "Each", "For":
		return true
	default:
		return false
	}
}

func isConditionalComponent(tag string) bool {
	switch tag {
	case "If", "Show", "When":
		return true
	default:
		return false
	}
}

func eachAttrSource(attrs []Attr, names ...string) string {
	return islandAttrSource(attrs, names...)
}

func islandAttrSource(attrs []Attr, names ...string) string {
	for _, name := range names {
		for _, attr := range attrs {
			if attr.Name != name {
				continue
			}
			switch attr.Kind {
			case AttrExpr, AttrSpread:
				return attr.Expr
			case AttrStatic:
				if attr.Value != "" {
					return strconv.Quote(attr.Value)
				}
			case AttrBool:
				return "true"
			}
		}
	}
	return ""
}

func eachStaticAttrValue(attrs []Attr, names ...string) string {
	for _, name := range names {
		for _, attr := range attrs {
			if attr.Name == name && attr.Kind == AttrStatic {
				return attr.Value
			}
		}
	}
	return ""
}

func forEachStaticAttr(attrs []program.Attr, name string) string {
	for _, attr := range attrs {
		if attr.Kind == program.AttrStatic && attr.Name == name {
			return attr.Value
		}
	}
	return ""
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
