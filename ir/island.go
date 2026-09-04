package ir

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx/island/program"
)

// LowerIsland converts an IR component to an IslandProgram.
// The component must have IsIsland == true.
func LowerIsland(prog *Program, compIdx int) (*program.Program, error) {
	comp, err := islandComponent(prog, compIdx)
	if err != nil {
		return nil, err
	}
	if comp.AcceptsChildren || len(comp.AcceptsSlots) > 0 {
		return nil, fmt.Errorf("island component %s cannot declare caller children or named slots: root island call-site content is rendered outside the per-component .gxi program; compose children and slots through a same-file pure-view component inside the island instead", comp.Name)
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
	if err := l.validateProgramIntegrity(); err != nil {
		return nil, err
	}

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
		Browser:       scope.Browser,
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
	src                *Program
	dst                *program.Program
	srcIDs             []NodeID // tracks source node ID for each dst node
	scope              *ExprScope
	inlineHandlerIndex int
	componentIndex     map[string]int
	calleeErrors       map[int]error
	compositionIndex   int
	composed           bool
}

const maxIslandCompositionDepth = 32
const maxIslandProgramEntries = 1<<16 - 1

// islandExpansionError marks a lowering failure whose exact answer depends on
// the physically expanded island program (composition depth, emitted-node
// count, or expression-table capacity). Validation delegates those answers to
// LowerIsland so compile-time diagnostics and direct lowering cannot drift.
type islandExpansionError struct {
	message string
}

func (e *islandExpansionError) Error() string { return e.message }

func newIslandExpansionError(format string, args ...any) error {
	return &islandExpansionError{message: fmt.Sprintf(format, args...)}
}

type islandInlineExpr struct {
	source  string
	context *islandInlineContext
	scope   *ExprScope
}

type islandProjection struct {
	nodes   []NodeID
	context *islandInlineContext
	scope   *ExprScope
	// ancestry belongs to the projection's caller. A finite nested use such as
	// <Wrapper><Wrapper /></Wrapper> is not a recursive component definition,
	// even though the inner call is lowered while visiting Wrapper's hole.
	ancestry []string
}

// islandInlineContext is compile-time-only. It specializes a same-file
// pure-view component call into its owning island program; no component frame
// or virtual-DOM abstraction survives into the browser artifact.
type islandInlineContext struct {
	component    string
	props        map[string]islandInlineExpr
	children     islandProjection
	slots        map[string]islandProjection
	identAliases map[string]string
	activeNames  map[string]bool
}

func newIslandInlineContext() *islandInlineContext {
	return &islandInlineContext{
		props:        make(map[string]islandInlineExpr),
		slots:        make(map[string]islandProjection),
		identAliases: make(map[string]string),
		activeNames:  make(map[string]bool),
	}
}

func newIslandLowerer(src *Program, name string, scope *ExprScope) *islandLowerer {
	componentIndex := make(map[string]int, len(src.Components))
	for i := range src.Components {
		componentIndex[src.Components[i].Name] = i
	}
	return &islandLowerer{
		src:            src,
		dst:            &program.Program{Name: name},
		scope:          scope,
		componentIndex: componentIndex,
		calleeErrors:   make(map[int]error),
	}
}

func (l *islandLowerer) lowerComponent(comp Component) error {
	rootID, err := l.lowerNode(comp.Root, newIslandInlineContext(), []string{comp.Name}, 1)
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
	if err := l.emitSignalDefs(scope.Signals); err != nil {
		return err
	}
	if err := l.emitComputedDefs(scope.Computeds); err != nil {
		return err
	}
	return l.emitHandlerDefs(scope.Handlers)
}

func (l *islandLowerer) emitSignalDefs(signals []SignalInfo) error {
	for _, sig := range signals {
		initID, err := l.parseExprOrFallback(sig.InitExpr, l.scope, program.Expr{
			Op:    program.OpLitString,
			Value: sig.InitExpr,
			Type:  program.TypeAny,
		})
		if err != nil {
			return fmt.Errorf("emit signal %s initializer: %w", sig.Name, err)
		}
		l.dst.Signals = append(l.dst.Signals, program.SignalDef{
			Name: sig.Name,
			Type: typeHintToExprType(sig.TypeHint),
			Init: initID,
		})
	}
	return nil
}

func (l *islandLowerer) emitComputedDefs(computeds []ComputedInfo) error {
	// Render expressions and handlers may refer to any declaration because
	// they execute after component initialization, so l.scope intentionally
	// contains every computed name. A computed initializer is different: Go
	// lexical rules only make earlier declarations visible. Build a private,
	// sequential scope and publish each name only after its body parses.
	computedScope := cloneExprScope(l.scope)
	for _, computed := range computeds {
		delete(computedScope.Signals, computed.Name)
		delete(computedScope.SignalAliases, computed.Name)
	}

	for _, computed := range computeds {
		bodySource := strings.TrimSpace(computed.BodyExpr)
		if bodySource == "" {
			return fmt.Errorf("parse computed %s: body must contain exactly one return expression", computed.Name)
		}
		exprs, rootID, err := ParseExpr(bodySource, computedScope)
		if err != nil {
			return fmt.Errorf("parse computed %s expression %q: %w", computed.Name, bodySource, err)
		}
		bodyID, err := l.appendExprs(exprs, rootID)
		if err != nil {
			return fmt.Errorf("emit computed %s expression: %w", computed.Name, err)
		}
		l.dst.Computeds = append(l.dst.Computeds, program.ComputedDef{
			Name: computed.Name,
			Type: program.TypeAny,
			Expr: bodyID,
		})
		computedScope.Signals[computed.Name] = true
	}
	return nil
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
			bodyID, err := l.appendExprs(stmtExprs, stmtID)
			if err != nil {
				return fmt.Errorf("emit handler %s statement: %w", handler.Name, err)
			}
			h.Body = append(h.Body, bodyID)
		}
		l.dst.Handlers = append(l.dst.Handlers, h)
	}
	return nil
}

func handlerExprScope(scope *ExprScope) *ExprScope {
	handlerScope := cloneExprScope(scope)
	handlerScope.Browser = true
	for _, field := range islandEventFields {
		if handlerScope.SignalAliases[field] != "" || handlerScope.Signals[field] ||
			handlerScope.Props[field] || handlerScope.Handlers[field] {
			continue
		}
		handlerScope.EventFields[field] = true
	}
	return handlerScope
}

// islandEventFields mirrors the compact payload produced by the delegated
// browser runtime. Both data (the handler element's dataset object) and
// eventData (data-gosx-event-value / drag transfer text) remain structured VM
// values rather than being flattened to diagnostic strings.
var islandEventFields = []string{
	"type", "value", "checked", "selectedIndex", "key", "code",
	"ctrlKey", "metaKey", "altKey", "shiftKey", "repeat", "timeStamp", "editable",
	"targetID", "currentTargetID", "pointerID", "pointerType", "isPrimary",
	"clientX", "clientY", "button", "buttons", "pressure", "width", "height",
	"data", "eventData",
}

func (l *islandLowerer) parseExprOrFallback(source string, scope *ExprScope, fallback program.Expr) (program.ExprID, error) {
	exprs, rootID, err := ParseExpr(source, scope)
	if err != nil {
		return l.addExprDirect(fallback)
	}
	return l.appendExprs(exprs, rootID)
}

func (l *islandLowerer) populateStaticMask() {
	if l.composed {
		l.populateComposedStaticMask()
		return
	}
	l.dst.StaticMask = make([]bool, len(l.dst.Nodes))
	for i, srcID := range l.srcIDs {
		if int(srcID) < len(l.src.Nodes) {
			l.dst.StaticMask[i] = l.src.Nodes[srcID].IsStatic
		}
	}
}

func (l *islandLowerer) populateComposedStaticMask() {
	l.dst.StaticMask = make([]bool, len(l.dst.Nodes))
	known := make([]bool, len(l.dst.Nodes))
	var isStatic func(program.NodeID) bool
	isStatic = func(id program.NodeID) bool {
		if int(id) >= len(l.dst.Nodes) {
			return false
		}
		if known[id] {
			return l.dst.StaticMask[id]
		}
		known[id] = true
		node := l.dst.Nodes[id]
		static := false
		switch node.Kind {
		case program.NodeText:
			static = true
		case program.NodeElement, program.NodeFragment:
			static = true
			for _, attr := range node.Attrs {
				if attr.Kind == program.AttrExpr || attr.Kind == program.AttrEvent {
					static = false
					break
				}
			}
			if static {
				for _, child := range node.Children {
					if !isStatic(child) {
						static = false
						break
					}
				}
			}
		}
		l.dst.StaticMask[id] = static
		return static
	}
	for id := range l.dst.Nodes {
		isStatic(program.NodeID(id))
	}
}

// validateProgramIntegrity is the final fail-closed wire-contract gate. The
// binary format encodes collection counts and node/expression references as
// uint16; reaching this point must prove that no earlier append or offset can
// be truncated by serialization.
func (l *islandLowerer) validateProgramIntegrity() error {
	p := l.dst
	checkCount := func(label string, count int) error {
		if count > maxIslandProgramEntries {
			return newIslandExpansionError("island %s has %d %s; the program limit is 65,535", p.Name, count, label)
		}
		return nil
	}
	counts := []struct {
		label string
		count int
	}{
		{"props", len(p.Props)},
		{"nodes", len(p.Nodes)},
		{"expressions", len(p.Exprs)},
		{"signals", len(p.Signals)},
		{"computeds", len(p.Computeds)},
		{"handlers", len(p.Handlers)},
		{"functions", len(p.Funcs)},
		{"engine nodes", len(p.EngineNodes)},
		{"static flags", len(p.StaticMask)},
	}
	for _, entry := range counts {
		if err := checkCount(entry.label, entry.count); err != nil {
			return err
		}
	}
	if len(p.Nodes) == 0 || int(p.Root) >= len(p.Nodes) {
		return newIslandExpansionError("island %s has invalid root node %d for %d nodes", p.Name, p.Root, len(p.Nodes))
	}
	if len(p.StaticMask) != len(p.Nodes) {
		return newIslandExpansionError("island %s has %d static flags for %d nodes", p.Name, len(p.StaticMask), len(p.Nodes))
	}
	validExpr := func(id program.ExprID) bool { return int(id) < len(p.Exprs) }
	for i, expr := range p.Exprs {
		if len(expr.Operands) > maxIslandProgramEntries {
			return newIslandExpansionError("island %s has %d operands on expression %d; the program limit is 65,535", p.Name, len(expr.Operands), i)
		}
		for _, operand := range expr.Operands {
			if !validExpr(operand) {
				return newIslandExpansionError("island %s expression %d references missing operand %d", p.Name, i, operand)
			}
		}
	}
	for i, node := range p.Nodes {
		if len(node.Attrs) > maxIslandProgramEntries {
			return newIslandExpansionError("island %s has %d attributes on node %d; the program limit is 65,535", p.Name, len(node.Attrs), i)
		}
		if len(node.Children) > maxIslandProgramEntries {
			return newIslandExpansionError("island %s has %d children on node %d; the program limit is 65,535", p.Name, len(node.Children), i)
		}
		for _, child := range node.Children {
			if int(child) >= len(p.Nodes) {
				return newIslandExpansionError("island %s node %d references missing child %d", p.Name, i, child)
			}
		}
		if (node.Kind == program.NodeExpr || node.Kind == program.NodeForEach || node.Kind == program.NodeConditional) && !validExpr(node.Expr) {
			return newIslandExpansionError("island %s node %d references missing expression %d", p.Name, i, node.Expr)
		}
		for _, attr := range node.Attrs {
			if attr.Kind == program.AttrExpr && !validExpr(attr.Expr) {
				return newIslandExpansionError("island %s node %d attribute %q references missing expression %d", p.Name, i, attr.Name, attr.Expr)
			}
		}
	}
	for _, signal := range p.Signals {
		if !validExpr(signal.Init) {
			return newIslandExpansionError("island %s signal %s references missing initializer %d", p.Name, signal.Name, signal.Init)
		}
	}
	for _, computed := range p.Computeds {
		if !validExpr(computed.Expr) {
			return newIslandExpansionError("island %s computed %s references missing expression %d", p.Name, computed.Name, computed.Expr)
		}
	}
	for _, handler := range p.Handlers {
		if len(handler.Body) > maxIslandProgramEntries {
			return newIslandExpansionError("island %s has %d expressions in handler %s; the program limit is 65,535", p.Name, len(handler.Body), handler.Name)
		}
		for _, id := range handler.Body {
			if !validExpr(id) {
				return newIslandExpansionError("island %s handler %s references missing expression %d", p.Name, handler.Name, id)
			}
		}
	}
	for _, fn := range p.Funcs {
		if len(fn.Params) > maxIslandProgramEntries {
			return newIslandExpansionError("island %s has %d parameters in function %s; the program limit is 65,535", p.Name, len(fn.Params), fn.Name)
		}
		if len(fn.Body) > maxIslandProgramEntries {
			return newIslandExpansionError("island %s has %d expressions in function %s; the program limit is 65,535", p.Name, len(fn.Body), fn.Name)
		}
		for _, id := range fn.Body {
			if !validExpr(id) {
				return newIslandExpansionError("island %s function %s references missing expression %d", p.Name, fn.Name, id)
			}
		}
	}
	for i, node := range p.EngineNodes {
		if len(node.Props) > maxIslandProgramEntries {
			return newIslandExpansionError("island %s has %d properties on engine node %d; the program limit is 65,535", p.Name, len(node.Props), i)
		}
		if len(node.Children) > maxIslandProgramEntries {
			return newIslandExpansionError("island %s has %d children on engine node %d; the program limit is 65,535", p.Name, len(node.Children), i)
		}
		names := make([]string, 0, len(node.Props))
		for name := range node.Props {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			id := node.Props[name]
			if !validExpr(id) {
				return newIslandExpansionError("island %s engine node %d property %s references missing expression %d", p.Name, i, name, id)
			}
		}
	}
	return nil
}

func (l *islandLowerer) lowerNode(srcID NodeID, context *islandInlineContext, ancestry []string, physicalDepth int) (program.NodeID, error) {
	if int(srcID) >= len(l.src.Nodes) {
		return 0, fmt.Errorf("node %d not found", srcID)
	}
	srcNode := l.src.NodeAt(srcID)

	if srcNode.Kind == NodeComponent {
		// A same-file declaration is authoritative even when its name shadows
		// an island builtin or element alias. The strict front end applies the
		// same precedence before this VM lowering stage.
		if targetIdx, ok := l.componentIndex[srcNode.Tag]; ok {
			return l.lowerComposedCall(srcNode, targetIdx, context, ancestry, physicalDepth)
		}
		if strings.Contains(srcNode.Tag, ".") {
			return 0, fmt.Errorf("imported component <%s> cannot be composed inside island %s in v1; use a same-file strict pure-view component", srcNode.Tag, l.dst.Name)
		}
	}

	// Check NodeID overflow (uint32 -> uint16)
	if len(l.dst.Nodes) >= maxIslandProgramEntries {
		return 0, newIslandExpansionError("island %s exceeds the 65,535 expanded-node limit while composing client-side components", l.dst.Name)
	}

	dstID := program.NodeID(len(l.dst.Nodes))

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
			dstAttr, err := l.lowerAttr(attr, context)
			if err != nil {
				return 0, err
			}
			node.Attrs = append(node.Attrs, dstAttr)
		}
	case NodeComponent:
		if isEachComponent(srcNode.Tag) {
			var err error
			node, err = l.lowerEachNode(srcNode, context)
			if err != nil {
				return 0, err
			}
			break
		}
		if isConditionalComponent(srcNode.Tag) {
			var err error
			node, err = l.lowerConditionalNode(srcNode, context)
			if err != nil {
				return 0, err
			}
			break
		}
		if srcNode.Tag == "Image" {
			return 0, unsupportedIslandComponentImageError()
		}
		if tag, ok := islandElementAlias(srcNode.Tag); ok {
			node.Kind = program.NodeElement
			node.Tag = tag
			for _, attr := range srcNode.Attrs {
				dstAttr, err := l.lowerAttr(attr, context)
				if err != nil {
					return 0, err
				}
				node.Attrs = append(node.Attrs, dstAttr)
			}
			break
		}
		return 0, fmt.Errorf("component <%s> is not supported inside island components yet", srcNode.Tag)
	case NodeText:
		node.Kind = program.NodeText
		node.Text = srcNode.Text
	case NodeExpr:
		node.Kind = program.NodeExpr
		exprID, err := l.addExprWithContext(srcNode.Text, context, l.scope)
		if err != nil {
			return 0, err
		}
		node.Expr = exprID
	case NodeFragment:
		node.Kind = program.NodeFragment
	case NodeRawHTML:
		node.Kind = program.NodeText
		node.Text = srcNode.Text
	}

	// Lower children
	childScope := l.scope
	childContext := context
	if node.Kind == program.NodeForEach {
		childScope = l.scopeForEach(node)
		childContext, node = l.scopeInlineForEach(context, node)
	}
	children, err := l.lowerChildren(srcNode.Children, childScope, childContext, ancestry, physicalDepth)
	if err != nil {
		return 0, err
	}
	node.Children = append(node.Children, children...)

	l.dst.Nodes[dstID] = node
	return dstID, nil
}

func (l *islandLowerer) lowerNodeWithScope(srcID NodeID, scope *ExprScope, context *islandInlineContext, ancestry []string, physicalDepth int) (program.NodeID, error) {
	prev := l.scope
	l.scope = scope
	defer func() {
		l.scope = prev
	}()
	return l.lowerNode(srcID, context, ancestry, physicalDepth)
}

func (l *islandLowerer) lowerChildren(srcIDs []NodeID, scope *ExprScope, context *islandInlineContext, ancestry []string, physicalDepth int) ([]program.NodeID, error) {
	var out []program.NodeID
	for _, srcID := range srcIDs {
		if int(srcID) >= len(l.src.Nodes) {
			return nil, fmt.Errorf("node %d not found", srcID)
		}
		srcNode := l.src.NodeAt(srcID)
		if srcNode.Kind == NodeExpr && context != nil {
			if name, projected := islandProjectionExpression(srcNode.Text); projected && name == "" {
				projected, err := l.lowerProjection(context.children, ancestry, physicalDepth)
				if err != nil {
					return nil, err
				}
				out = append(out, projected...)
				continue
			} else if projected {
				projected, err := l.lowerProjection(context.slots[name], ancestry, physicalDepth)
				if err != nil {
					return nil, err
				}
				out = append(out, projected...)
				continue
			}
		}
		dstID, err := l.lowerNodeWithScope(srcID, scope, context, ancestry, physicalDepth)
		if err != nil {
			return nil, err
		}
		out = append(out, dstID)
	}
	return out, nil
}

// islandProjectionExpression recognizes only the whole bare identifiers used
// for children and named-slot holes. ParseExpr makes redundant parentheses
// transparent while keeping this build-neutral; ir/island.go is compiled by
// TinyGo, whereas the host strict-component validator depends on go/parser.
// The empty returned name denotes the anonymous children projection.
func islandProjectionExpression(source string) (name string, ok bool) {
	exprs, root, err := ParseExpr(source, nil)
	if err != nil || len(exprs) != 1 || int(root) >= len(exprs) {
		return "", false
	}
	expr := exprs[root]
	if expr.Op != program.OpPropGet {
		return "", false
	}
	if expr.Value == "children" {
		return "", true
	}
	if !strings.HasPrefix(expr.Value, "slot") || len(expr.Value) == len("slot") {
		return "", false
	}
	rest := expr.Value[len("slot"):]
	if rest[0] < 'A' || rest[0] > 'Z' {
		return "", false
	}
	return rest, true
}

func (l *islandLowerer) lowerProjection(projection islandProjection, ancestry []string, physicalDepth int) ([]program.NodeID, error) {
	if len(projection.nodes) == 0 {
		return nil, nil
	}
	if projection.ancestry != nil {
		ancestry = projection.ancestry
	}
	return l.lowerChildren(projection.nodes, projection.scope, projection.context, ancestry, physicalDepth)
}

func (l *islandLowerer) lowerComposedCall(call *Node, targetIdx int, callerContext *islandInlineContext, ancestry []string, physicalDepth int) (program.NodeID, error) {
	target := l.src.Components[targetIdx]
	for _, name := range ancestry {
		if name == target.Name {
			path := append(append([]string(nil), ancestry...), target.Name)
			return 0, fmt.Errorf("island component composition cycle: %s", strings.Join(path, " -> "))
		}
	}
	if physicalDepth >= maxIslandCompositionDepth {
		return 0, newIslandExpansionError("island component composition exceeds the %d-component depth limit at <%s>", maxIslandCompositionDepth, target.Name)
	}
	calleeErr, checked := l.calleeErrors[targetIdx]
	if !checked {
		calleeErr = l.composableCalleeError(&target)
		l.calleeErrors[targetIdx] = calleeErr
	}
	if calleeErr != nil {
		return 0, calleeErr
	}
	l.composed = true

	callScope := cloneExprScope(l.scope)
	context := newIslandInlineContext()
	context.component = target.Name
	if callerContext != nil {
		for name, active := range callerContext.activeNames {
			context.activeNames[name] = active
		}
	}
	for _, attr := range call.Attrs {
		var source string
		switch attr.Kind {
		case AttrStatic:
			source = strconv.Quote(attr.Value)
		case AttrBool:
			source = "true"
		case AttrExpr:
			if attr.IsEvent || (l.scope != nil && l.scope.Handlers[strings.TrimSpace(attr.Expr)]) {
				return 0, fmt.Errorf("component <%s> passes handler-valued prop %q inside island %s; v1 pure-view composition accepts typed scalar props only", target.Name, attr.Name, l.dst.Name)
			}
			source = attr.Expr
		case AttrSpread:
			return 0, fmt.Errorf("component <%s> uses a spread inside island %s; v1 pure-view composition requires explicit typed scalar props", target.Name, l.dst.Name)
		default:
			return 0, fmt.Errorf("component <%s> has unsupported prop %q inside island %s", target.Name, attr.Name, l.dst.Name)
		}
		context.props[attr.Name] = islandInlineExpr{source: source, context: callerContext, scope: callScope}
	}
	projectionAncestry := append([]string(nil), ancestry...)
	context.children = islandProjection{nodes: call.Children, context: callerContext, scope: callScope, ancestry: projectionAncestry}
	// A single map key has no ordering ambiguity. Keep the overwhelmingly
	// common one-slot path allocation-free while sorting multi-slot calls.
	if len(call.Slots) == 1 {
		for name, nodeID := range call.Slots {
			context.slots[name] = islandProjection{nodes: []NodeID{nodeID}, context: callerContext, scope: callScope, ancestry: projectionAncestry}
		}
	} else {
		for _, name := range sortedNodeSlotNames(call.Slots) {
			nodeID := call.Slots[name]
			context.slots[name] = islandProjection{nodes: []NodeID{nodeID}, context: callerContext, scope: callScope, ancestry: projectionAncestry}
		}
	}

	l.compositionIndex++
	return l.lowerNodeWithScope(target.Root, l.scope, context, append(ancestry, target.Name), physicalDepth+1)
}

func (l *islandLowerer) composableCalleeError(target *Component) error {
	if target.IsIsland {
		return fmt.Errorf("nested island <%s> is not allowed inside island %s; compose a non-island strict pure-view component so the subtree has one hydration root and one VM", target.Name, l.dst.Name)
	}
	if target.IsEngine || target.ServerOnly {
		return fmt.Errorf("component <%s> is not a client pure-view component and cannot be composed inside island %s", target.Name, l.dst.Name)
	}
	if target.Syntax != ComponentSyntaxStrict {
		return fmt.Errorf("component <%s> uses legacy component syntax; v1 island composition accepts same-file strict pure-view components only", target.Name)
	}
	if target.Scope != nil && (len(target.Scope.Signals) > 0 || len(target.Scope.Computeds) > 0 || len(target.Scope.Handlers) > 0 || len(target.Scope.Locals) > 0) {
		return fmt.Errorf("component <%s> owns signals, computed values, handlers, or effects; v1 island composition requires a pure-view callee and keeps all state in the parent island", target.Name)
	}
	fields := make([]string, 0, len(target.PropsFields))
	for field := range target.PropsFields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		typ := target.PropsFields[field]
		if !strictRendererScalarType(typ) {
			return fmt.Errorf("component <%s> reads non-scalar prop %s (%s); v1 island composition accepts typed string, bool, integer, and floating-point props only", target.Name, field, typ)
		}
	}
	if len(target.PropsPaths) > 0 || len(target.PropsSlices) > 0 {
		return fmt.Errorf("component <%s> reads nested or slice props; v1 island composition accepts direct typed scalar props only", target.Name)
	}
	if err := l.composableViewTreeError(target.Name, target.Root, make(map[NodeID]bool)); err != nil {
		return err
	}
	return nil
}

func (l *islandLowerer) composableViewTreeError(componentName string, id NodeID, seen map[NodeID]bool) error {
	if int(id) >= len(l.src.Nodes) || seen[id] {
		return nil
	}
	seen[id] = true
	node := l.src.NodeAt(id)
	for _, attr := range node.Attrs {
		if attr.Kind == AttrSpread {
			return fmt.Errorf("pure-view component <%s> contains a spread attribute; v1 island composition requires explicit attributes", componentName)
		}
		if attr.IsEvent || (attr.Kind == AttrStatic && strings.HasPrefix(strings.ToLower(strings.TrimSpace(attr.Name)), "data-on-")) {
			return fmt.Errorf("pure-view component <%s> contains handler attribute %q; keep behavior in the parent island and pass only scalar view data", componentName, attr.Name)
		}
	}
	for _, child := range node.Children {
		if err := l.composableViewTreeError(componentName, child, seen); err != nil {
			return err
		}
	}
	for _, name := range sortedNodeSlotNames(node.Slots) {
		child := node.Slots[name]
		if err := l.composableViewTreeError(componentName, child, seen); err != nil {
			return err
		}
	}
	return nil
}

func sortedNodeSlotNames(slots map[string]NodeID) []string {
	if len(slots) == 0 {
		return nil
	}
	names := make([]string, 0, len(slots))
	for name := range slots {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (l *islandLowerer) scopeInlineForEach(context *islandInlineContext, node program.Node) (*islandInlineContext, program.Node) {
	if context == nil {
		context = newIslandInlineContext()
	}
	next := &islandInlineContext{
		component:    context.component,
		props:        context.props,
		children:     context.children,
		slots:        context.slots,
		identAliases: make(map[string]string, len(context.identAliases)+3),
		activeNames:  make(map[string]bool, len(context.activeNames)+3),
	}
	for name, alias := range context.identAliases {
		next.identAliases[name] = alias
	}
	for name, active := range context.activeNames {
		next.activeNames[name] = active
	}

	itemName := forEachStaticAttr(node.Attrs, "as")
	if itemName == "" {
		itemName = "item"
	}
	actualItem := l.uniqueInlineBinding(itemName, next.activeNames)
	next.identAliases[itemName] = actualItem
	next.identAliases[itemName+"Key"] = actualItem + "Key"
	next.activeNames[actualItem] = true
	next.activeNames[actualItem+"Key"] = true
	setForEachStaticAttr(node.Attrs, "as", actualItem)

	if indexName := forEachStaticAttr(node.Attrs, "index"); indexName != "" {
		actualIndex := l.uniqueInlineBinding(indexName, next.activeNames)
		next.identAliases[indexName] = actualIndex
		next.activeNames[actualIndex] = true
		setForEachStaticAttr(node.Attrs, "index", actualIndex)
	}
	return next, node
}

func (l *islandLowerer) uniqueInlineBinding(name string, active map[string]bool) string {
	if !active[name] {
		return name
	}
	for {
		l.compositionIndex++
		candidate := "__gosx_inline_" + strconv.Itoa(l.compositionIndex) + "_" + name
		if !active[candidate] {
			return candidate
		}
	}
}

func setForEachStaticAttr(attrs []program.Attr, name, value string) {
	for i := range attrs {
		if attrs[i].Kind == program.AttrStatic && attrs[i].Name == name {
			attrs[i].Value = value
			return
		}
	}
}

func (l *islandLowerer) lowerEachNode(srcNode *Node, context *islandInlineContext) (program.Node, error) {
	collectionExpr := eachAttrSource(srcNode.Attrs, "of", "each", "items")
	if collectionExpr == "" {
		return program.Node{}, fmt.Errorf("%s requires an of/each/items attribute", srcNode.Tag)
	}

	exprID, err := l.addExprWithContext(collectionExpr, context, l.scope)
	if err != nil {
		return program.Node{}, err
	}
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
		fallbackID, err := l.addExprWithContext(fallbackSource, context, l.scope)
		if err != nil {
			return program.Node{}, err
		}
		node.Attrs = append(node.Attrs, program.Attr{
			Kind: program.AttrExpr,
			Name: "fallback",
			Expr: fallbackID,
		})
	}

	return node, nil
}

func (l *islandLowerer) lowerConditionalNode(srcNode *Node, context *islandInlineContext) (program.Node, error) {
	conditionExpr := islandAttrSource(srcNode.Attrs, "when", "if", "cond", "test")
	if conditionExpr == "" {
		return program.Node{}, fmt.Errorf("%s requires a when/if/cond/test attribute", srcNode.Tag)
	}

	conditionID, err := l.addExprWithContext(conditionExpr, context, l.scope)
	if err != nil {
		return program.Node{}, err
	}
	node := program.Node{
		Kind: program.NodeConditional,
		Expr: conditionID,
	}
	if fallbackSource := islandAttrSource(srcNode.Attrs, "fallback", "else"); fallbackSource != "" {
		fallbackID, err := l.addExprWithContext(fallbackSource, context, l.scope)
		if err != nil {
			return program.Node{}, err
		}
		node.Attrs = append(node.Attrs, program.Attr{
			Kind: program.AttrExpr,
			Name: "fallback",
			Expr: fallbackID,
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

func (l *islandLowerer) lowerAttr(attr Attr, context *islandInlineContext) (program.Attr, error) {
	switch attr.Kind {
	case AttrStatic:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(attr.Name)), "data-on-") {
			eventType, ok := legacyInlineEventType(attr.Name)
			if !ok {
				return program.Attr{}, fmt.Errorf("island event attribute %q is not supported", attr.Name)
			}
			return l.lowerInlineEvent(eventType, attr.Value)
		}
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
		// Expression attribute -- add to expr table, substituting any
		// compile-time pure-view prop bindings before the opcode graph lands.
		exprID, err := l.addExprWithContext(attr.Expr, context, l.scope)
		if err != nil {
			return program.Attr{}, err
		}
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

// legacyInlineEventType recognizes the original island event spelling:
//
//	<button data-on-click="count.Set(count.Get() + 1)">+1</button>
//
// TSX-style onClick={increment} remains the preferred typed form. Keeping this
// spelling in the island lowerer preserves existing components and playground
// snippets without treating data-on-* as executable on server components.
func legacyInlineEventType(name string) (string, bool) {
	const prefix = "data-on-"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	eventType := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(name, prefix)))
	if eventType == "" {
		return "", false
	}
	return eventType, legacyInlineEventSupported(eventType)
}

func legacyInlineEventSupported(eventType string) bool {
	switch eventType {
	case "click", "input", "change", "submit", "keydown", "keyup", "focus", "blur",
		"dragstart", "dragend", "dragover", "dragleave", "drop",
		"pointerdown", "pointermove", "pointerup", "pointercancel",
		"document-keydown", "document-keyup", "window-resize":
		return true
	default:
		return false
	}
}

func (l *islandLowerer) lowerInlineEvent(eventType, source string) (program.Attr, error) {
	expression := strings.TrimSpace(source)
	// JSX string literals retain Go-style escapes in the IR so ordinary static
	// attributes round-trip exactly. Inline event source is code, however, and
	// must turn \"light\" back into "light" before expression parsing.
	if unquoted, err := strconv.Unquote(`"` + expression + `"`); err == nil {
		expression = unquoted
	}
	if expression == "" {
		return program.Attr{}, fmt.Errorf("data-on-%s requires a handler expression", eventType)
	}

	handlerName := l.nextInlineHandlerName()
	exprs, rootID, err := ParseExpr(expression, handlerExprScope(l.scope))
	if err != nil {
		return program.Attr{}, fmt.Errorf("parse data-on-%s expression %q: %w", eventType, expression, err)
	}
	bodyID, err := l.appendExprs(exprs, rootID)
	if err != nil {
		return program.Attr{}, fmt.Errorf("emit data-on-%s expression: %w", eventType, err)
	}
	l.dst.Handlers = append(l.dst.Handlers, program.Handler{
		Name: handlerName,
		Body: []program.ExprID{bodyID},
	})

	return program.Attr{
		Kind:  program.AttrEvent,
		Name:  eventType,
		Event: handlerName,
	}, nil
}

func (l *islandLowerer) nextInlineHandlerName() string {
	for {
		name := "__gosx_inline_event_" + strconv.Itoa(l.inlineHandlerIndex)
		l.inlineHandlerIndex++
		if l.scope == nil || !l.scope.Handlers[name] {
			return name
		}
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

// islandElementAlias no longer maps "Image" to "img" (gosx#201): see
// unsupportedIslandComponentImageError below, and
// unsupportedIslandComponentDiagnostic in ir/validate.go, for why <Image>
// is rejected inside an island instead of silently downgraded to a plain
// <img>.
func islandElementAlias(tag string) (string, bool) {
	switch tag {
	case "Link":
		return "a", true
	default:
		return "", false
	}
}

// unsupportedIslandComponentImageError carries the same message text
// unsupportedIslandComponentDiagnostic (ir/validate.go) uses for the same
// rejection, formatted as a plain error instead of a Diagnostic: ir.Validate
// gates every gosx.Compile call (see compile.go), so a program reaching
// lowerNode below has almost always already failed there first. This
// message exists for the paths that call LowerIsland directly against a
// program Validate never ran over -- for example
// route/fileprogram.go's dev-mode islandProgram lookup -- so <Image> still
// fails closed even then, not just at compile time.
func unsupportedIslandComponentImageError() error {
	return fmt.Errorf("<Image> is not supported inside island components: an island cannot rebuild <Image>'s server-rendered <picture> markup on the client; use a plain <img> element inside the island instead, and set width and height explicitly to avoid layout shift")
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

// addExprWithContext parses one expression and rewrites direct props.Field
// reads through the scalar expressions supplied at the component call site.
// The rewrite happens on the opcode graph, not by source-text replacement, so
// identifiers with matching substrings and string literals remain untouched.
func (l *islandLowerer) addExprWithContext(source string, context *islandInlineContext, scope *ExprScope) (program.ExprID, error) {
	exprs, rootID, err := ParseExpr(source, scope)
	if err != nil {
		return 0, fmt.Errorf("parse island expression %q: %w", source, err)
	}
	return l.appendInlineExpr(exprs, rootID, context, make(map[program.ExprID]program.ExprID))
}

func (l *islandLowerer) appendInlineExpr(exprs []program.Expr, id program.ExprID, context *islandInlineContext, memo map[program.ExprID]program.ExprID) (program.ExprID, error) {
	if mapped, ok := memo[id]; ok {
		return mapped, nil
	}
	if int(id) >= len(exprs) {
		return 0, fmt.Errorf("island expression references invalid opcode %d", id)
	}
	if field, ok := directPropsField(exprs, id); ok && context != nil && context.component != "" {
		binding, supplied := context.props[field]
		if !supplied {
			return 0, fmt.Errorf("composed component <%s> requires scalar prop %s, but the call does not supply it", context.component, field)
		}
		mapped, err := l.addExprWithContext(binding.source, binding.context, binding.scope)
		if err != nil {
			return 0, fmt.Errorf("compose <%s> prop %s: %w", context.component, field, err)
		}
		memo[id] = mapped
		return mapped, nil
	}

	expr := exprs[id]
	if expr.Op == program.OpPropGet && context != nil {
		if alias := context.identAliases[expr.Value]; alias != "" {
			expr.Value = alias
		}
	}
	if len(expr.Operands) > 0 {
		operands := make([]program.ExprID, len(expr.Operands))
		for i, operand := range expr.Operands {
			mapped, err := l.appendInlineExpr(exprs, operand, context, memo)
			if err != nil {
				return 0, err
			}
			operands[i] = mapped
		}
		expr.Operands = operands
	}
	mapped, err := l.addExprDirect(expr)
	if err != nil {
		return 0, err
	}
	memo[id] = mapped
	return mapped, nil
}

func directPropsField(exprs []program.Expr, id program.ExprID) (string, bool) {
	if int(id) >= len(exprs) {
		return "", false
	}
	expr := exprs[id]
	if expr.Op != program.OpIndex || len(expr.Operands) != 2 {
		return "", false
	}
	receiverID, fieldID := expr.Operands[0], expr.Operands[1]
	if int(receiverID) >= len(exprs) || int(fieldID) >= len(exprs) {
		return "", false
	}
	receiver, field := exprs[receiverID], exprs[fieldID]
	if receiver.Op != program.OpPropGet || receiver.Value != "props" || field.Op != program.OpLitString || field.Value == "" {
		return "", false
	}
	return field.Value, true
}

// addExprDirect is the only expression-table allocator. Every caller receives
// an error before len(Exprs) can overflow the uint16 count/ID wire contract.
func (l *islandLowerer) addExprDirect(e program.Expr) (program.ExprID, error) {
	if len(l.dst.Exprs) >= maxIslandProgramEntries {
		return 0, newIslandExpansionError("island %s exceeds the 65,535 expression limit", l.dst.Name)
	}
	id := program.ExprID(len(l.dst.Exprs))
	l.dst.Exprs = append(l.dst.Exprs, e)
	return id, nil
}

// appendExprs appends parsed expressions to the program, offsetting operand
// references, and returns the adjusted root ID.
func (l *islandLowerer) appendExprs(exprs []program.Expr, rootID program.ExprID) (program.ExprID, error) {
	if len(exprs) == 0 || int(rootID) >= len(exprs) {
		return 0, fmt.Errorf("island expression root %d is outside %d parsed opcodes", rootID, len(exprs))
	}
	base := len(l.dst.Exprs)
	if len(exprs) > maxIslandProgramEntries-base {
		return 0, newIslandExpansionError("island %s exceeds the 65,535 expression limit", l.dst.Name)
	}
	for _, e := range exprs {
		adjusted := e
		if len(adjusted.Operands) > 0 {
			ops := make([]program.ExprID, len(adjusted.Operands))
			for i, op := range adjusted.Operands {
				if int(op) >= len(exprs) {
					return 0, fmt.Errorf("island expression operand %d is outside %d parsed opcodes", op, len(exprs))
				}
				offset := base + int(op)
				if offset >= maxIslandProgramEntries {
					return 0, newIslandExpansionError("island %s exceeds the 65,535 expression limit", l.dst.Name)
				}
				ops[i] = program.ExprID(offset)
			}
			adjusted.Operands = ops
		}
		if _, err := l.addExprDirect(adjusted); err != nil {
			return 0, err
		}
	}
	rootOffset := base + int(rootID)
	if rootOffset >= maxIslandProgramEntries {
		return 0, newIslandExpansionError("island %s exceeds the 65,535 expression limit", l.dst.Name)
	}
	return program.ExprID(rootOffset), nil
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
