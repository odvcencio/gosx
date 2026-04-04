package engine

import (
	"strconv"
	"strings"

	islandprogram "github.com/odvcencio/gosx/island/program"
)

// Handle is an index-based reference to a node in a builder.
type Handle int

// InvalidHandle is returned when a builder operation cannot create a node.
const InvalidHandle Handle = -1

// MeshOptions configures mesh-node creation.
type MeshOptions struct {
	Static   bool
	Children []Handle
}

// InputSignals exposes the standard viewport-relative input signals.
type InputSignals struct {
	CenterX    islandprogram.ExprID
	CenterY    islandprogram.ExprID
	PointerX   islandprogram.ExprID
	PointerY   islandprogram.ExprID
	ArrowLeft  islandprogram.ExprID
	ArrowRight islandprogram.ExprID
	ArrowUp    islandprogram.ExprID
}

// SceneEventSignals exposes framework-owned Scene3D interaction state and the
// latest semantic event revision routed through the shared runtime.
type SceneEventSignals struct {
	Revision      islandprogram.ExprID
	Type          islandprogram.ExprID
	TargetIndex   islandprogram.ExprID
	TargetID      islandprogram.ExprID
	TargetKind    islandprogram.ExprID
	Hovered       islandprogram.ExprID
	HoverIndex    islandprogram.ExprID
	HoverID       islandprogram.ExprID
	HoverKind     islandprogram.ExprID
	Down          islandprogram.ExprID
	DownIndex     islandprogram.ExprID
	DownID        islandprogram.ExprID
	DownKind      islandprogram.ExprID
	Selected      islandprogram.ExprID
	SelectedIndex islandprogram.ExprID
	SelectedID    islandprogram.ExprID
	SelectedKind  islandprogram.ExprID
	ClickCount    islandprogram.ExprID
	PointerX      islandprogram.ExprID
	PointerY      islandprogram.ExprID
}

// SceneObjectSignals exposes interaction state for one Scene3D object ID.
type SceneObjectSignals struct {
	Hovered    islandprogram.ExprID
	Down       islandprogram.ExprID
	Selected   islandprogram.ExprID
	ClickCount islandprogram.ExprID
}

// Builder assembles engine programs with stable node handles and reusable
// expression helpers.
type Builder struct {
	name    string
	exprs   []islandprogram.Expr
	signals []islandprogram.SignalDef
	nodes   []Node
}

// NewBuilder constructs a new engine-program builder.
func NewBuilder(name string) *Builder {
	return &Builder{name: name}
}

// Build emits a self-contained engine program.
func (b *Builder) Build() *Program {
	if b == nil {
		return &Program{}
	}
	return &Program{
		Name:    b.name,
		Exprs:   cloneExprs(b.exprs),
		Signals: cloneSignals(b.signals),
		Nodes:   cloneNodes(b.nodes),
	}
}

// Expr appends a raw expression to the program.
func (b *Builder) Expr(expr islandprogram.Expr) islandprogram.ExprID {
	id := islandprogram.ExprID(len(b.exprs))
	b.exprs = append(b.exprs, cloneExpr(expr))
	return id
}

// Value appends a typed expression with the given opcode and operands.
func (b *Builder) Value(op islandprogram.OpCode, value string, typ islandprogram.ExprType, operands ...islandprogram.ExprID) islandprogram.ExprID {
	return b.Expr(islandprogram.Expr{
		Op:       op,
		Operands: append([]islandprogram.ExprID(nil), operands...),
		Value:    value,
		Type:     typ,
	})
}

// Prop references a runtime prop.
func (b *Builder) Prop(name string, typ islandprogram.ExprType) islandprogram.ExprID {
	return b.Value(islandprogram.OpPropGet, name, typ)
}

// Signal references a runtime signal.
func (b *Builder) Signal(name string, typ islandprogram.ExprType) islandprogram.ExprID {
	return b.Value(islandprogram.OpSignalGet, name, typ)
}

// DeclareSignal registers a signal definition and returns a reference to it.
func (b *Builder) DeclareSignal(name string, typ islandprogram.ExprType, init islandprogram.ExprID) islandprogram.ExprID {
	b.signals = append(b.signals, islandprogram.SignalDef{
		Name: name,
		Type: typ,
		Init: init,
	})
	return b.Signal(name, typ)
}

// DeclareViewportInputSignals adds the shared pointer and keyboard signals used
// by interactive scene programs.
func (b *Builder) DeclareViewportInputSignals(width, height islandprogram.ExprID) InputSignals {
	half := b.Float(0.5)
	centerX := b.Mul(width, half)
	centerY := b.Mul(height, half)
	return InputSignals{
		CenterX:    centerX,
		CenterY:    centerY,
		PointerX:   b.DeclareSignal("$input.pointer.x", islandprogram.TypeFloat, centerX),
		PointerY:   b.DeclareSignal("$input.pointer.y", islandprogram.TypeFloat, centerY),
		ArrowLeft:  b.DeclareSignal("$input.key.arrowleft", islandprogram.TypeBool, b.Bool(false)),
		ArrowRight: b.DeclareSignal("$input.key.arrowright", islandprogram.TypeBool, b.Bool(false)),
		ArrowUp:    b.DeclareSignal("$input.key.arrowup", islandprogram.TypeBool, b.Bool(false)),
	}
}

// DeclareSceneEventSignals adds framework-owned Scene3D interaction signals.
func (b *Builder) DeclareSceneEventSignals(namespace string) SceneEventSignals {
	namespace = sceneSignalNamespace(namespace)
	return SceneEventSignals{
		Revision:      b.DeclareSignal(namespace+".revision", islandprogram.TypeFloat, b.Float(0)),
		Type:          b.DeclareSignal(namespace+".type", islandprogram.TypeString, b.String("")),
		TargetIndex:   b.DeclareSignal(namespace+".targetIndex", islandprogram.TypeFloat, b.Float(-1)),
		TargetID:      b.DeclareSignal(namespace+".targetID", islandprogram.TypeString, b.String("")),
		TargetKind:    b.DeclareSignal(namespace+".targetKind", islandprogram.TypeString, b.String("")),
		Hovered:       b.DeclareSignal(namespace+".hovered", islandprogram.TypeBool, b.Bool(false)),
		HoverIndex:    b.DeclareSignal(namespace+".hoverIndex", islandprogram.TypeFloat, b.Float(-1)),
		HoverID:       b.DeclareSignal(namespace+".hoverID", islandprogram.TypeString, b.String("")),
		HoverKind:     b.DeclareSignal(namespace+".hoverKind", islandprogram.TypeString, b.String("")),
		Down:          b.DeclareSignal(namespace+".down", islandprogram.TypeBool, b.Bool(false)),
		DownIndex:     b.DeclareSignal(namespace+".downIndex", islandprogram.TypeFloat, b.Float(-1)),
		DownID:        b.DeclareSignal(namespace+".downID", islandprogram.TypeString, b.String("")),
		DownKind:      b.DeclareSignal(namespace+".downKind", islandprogram.TypeString, b.String("")),
		Selected:      b.DeclareSignal(namespace+".selected", islandprogram.TypeBool, b.Bool(false)),
		SelectedIndex: b.DeclareSignal(namespace+".selectedIndex", islandprogram.TypeFloat, b.Float(-1)),
		SelectedID:    b.DeclareSignal(namespace+".selectedID", islandprogram.TypeString, b.String("")),
		SelectedKind:  b.DeclareSignal(namespace+".selectedKind", islandprogram.TypeString, b.String("")),
		ClickCount:    b.DeclareSignal(namespace+".clickCount", islandprogram.TypeFloat, b.Float(0)),
		PointerX:      b.DeclareSignal(namespace+".pointerX", islandprogram.TypeFloat, b.Float(0)),
		PointerY:      b.DeclareSignal(namespace+".pointerY", islandprogram.TypeFloat, b.Float(0)),
	}
}

// DeclareSceneObjectSignals adds per-object Scene3D interaction signals for a
// stable object ID lowered from the runtime scene.
func (b *Builder) DeclareSceneObjectSignals(namespace, objectID string) SceneObjectSignals {
	namespace = sceneSignalNamespace(namespace) + ".object." + sceneSignalSegment(objectID, "object")
	return SceneObjectSignals{
		Hovered:    b.DeclareSignal(namespace+".hovered", islandprogram.TypeBool, b.Bool(false)),
		Down:       b.DeclareSignal(namespace+".down", islandprogram.TypeBool, b.Bool(false)),
		Selected:   b.DeclareSignal(namespace+".selected", islandprogram.TypeBool, b.Bool(false)),
		ClickCount: b.DeclareSignal(namespace+".clickCount", islandprogram.TypeFloat, b.Float(0)),
	}
}

// String appends a string literal.
func (b *Builder) String(value string) islandprogram.ExprID {
	return b.Value(islandprogram.OpLitString, value, islandprogram.TypeString)
}

// Bool appends a bool literal.
func (b *Builder) Bool(value bool) islandprogram.ExprID {
	if value {
		return b.Value(islandprogram.OpLitBool, "true", islandprogram.TypeBool)
	}
	return b.Value(islandprogram.OpLitBool, "false", islandprogram.TypeBool)
}

// Float appends a float literal.
func (b *Builder) Float(value float64) islandprogram.ExprID {
	return b.Value(islandprogram.OpLitFloat, strconv.FormatFloat(value, 'f', -1, 64), islandprogram.TypeFloat)
}

// Add appends a float addition.
func (b *Builder) Add(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.Value(islandprogram.OpAdd, "", islandprogram.TypeFloat, left, right)
}

// Sub appends a float subtraction.
func (b *Builder) Sub(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.Value(islandprogram.OpSub, "", islandprogram.TypeFloat, left, right)
}

// Mul appends a float multiplication.
func (b *Builder) Mul(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.Value(islandprogram.OpMul, "", islandprogram.TypeFloat, left, right)
}

// Div appends a float division.
func (b *Builder) Div(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.Value(islandprogram.OpDiv, "", islandprogram.TypeFloat, left, right)
}

// Eq appends a typed equality comparison.
func (b *Builder) Eq(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.Value(islandprogram.OpEq, "", islandprogram.TypeBool, left, right)
}

// Neq appends a typed inequality comparison.
func (b *Builder) Neq(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.Value(islandprogram.OpNeq, "", islandprogram.TypeBool, left, right)
}

// Cond appends a conditional expression.
func (b *Builder) Cond(test, whenTrue, whenFalse islandprogram.ExprID, typ islandprogram.ExprType) islandprogram.ExprID {
	return b.Value(islandprogram.OpCond, "", typ, test, whenTrue, whenFalse)
}

// AddNode appends a raw scene node and returns its handle.
func (b *Builder) AddNode(node Node) Handle {
	handle := Handle(len(b.nodes))
	b.nodes = append(b.nodes, cloneNode(node))
	return handle
}

// Camera appends a camera node.
func (b *Builder) Camera(props map[string]islandprogram.ExprID) Handle {
	return b.AddNode(Node{
		Kind:  "camera",
		Props: cloneNodeProps(props),
	})
}

// Mesh appends a mesh node.
func (b *Builder) Mesh(geometry, material string, props map[string]islandprogram.ExprID, opts MeshOptions) Handle {
	return b.AddNode(Node{
		Kind:     "mesh",
		Geometry: geometry,
		Material: material,
		Props:    cloneNodeProps(props),
		Children: handlesToChildren(opts.Children),
		Static:   opts.Static,
	})
}

// Label appends a screen-overlay label node anchored in world space.
func (b *Builder) Label(props map[string]islandprogram.ExprID) Handle {
	return b.AddNode(Node{
		Kind:  "label",
		Props: cloneNodeProps(props),
	})
}

// Sprite appends a projected image billboard anchored in world space.
func (b *Builder) Sprite(props map[string]islandprogram.ExprID) Handle {
	return b.AddNode(Node{
		Kind:  "sprite",
		Props: cloneNodeProps(props),
	})
}

// Light appends a light node.
func (b *Builder) Light(props map[string]islandprogram.ExprID) Handle {
	return b.AddNode(Node{
		Kind:  "light",
		Props: cloneNodeProps(props),
	})
}

// Node returns a mutable pointer to an existing node.
func (b *Builder) Node(handle Handle) *Node {
	index := int(handle)
	if index < 0 || index >= len(b.nodes) {
		return nil
	}
	return &b.nodes[index]
}

// Include appends another program, remapping expression and child indices as needed.
func (b *Builder) Include(program *Program) []Handle {
	if b == nil || program == nil {
		return nil
	}

	exprOffset := islandprogram.ExprID(len(b.exprs))
	nodeOffset := len(b.nodes)

	for _, expr := range program.Exprs {
		cloned := cloneExpr(expr)
		for i, operand := range cloned.Operands {
			cloned.Operands[i] = remapExprID(operand, exprOffset)
		}
		b.exprs = append(b.exprs, cloned)
	}

	for _, def := range program.Signals {
		cloned := def
		cloned.Init = remapExprID(cloned.Init, exprOffset)
		b.signals = append(b.signals, cloned)
	}

	handles := make([]Handle, len(program.Nodes))
	for i, node := range program.Nodes {
		cloned := cloneNode(node)
		for key, id := range cloned.Props {
			cloned.Props[key] = remapExprID(id, exprOffset)
		}
		for childIndex, child := range cloned.Children {
			cloned.Children[childIndex] = child + nodeOffset
		}
		handles[i] = b.AddNode(cloned)
	}

	return handles
}

func cloneExprs(exprs []islandprogram.Expr) []islandprogram.Expr {
	out := make([]islandprogram.Expr, len(exprs))
	for i, expr := range exprs {
		out[i] = cloneExpr(expr)
	}
	return out
}

func cloneExpr(expr islandprogram.Expr) islandprogram.Expr {
	expr.Operands = append([]islandprogram.ExprID(nil), expr.Operands...)
	return expr
}

func cloneSignals(signals []islandprogram.SignalDef) []islandprogram.SignalDef {
	return append([]islandprogram.SignalDef(nil), signals...)
}

func sceneSignalNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "$scene.event"
	}
	return namespace
}

func sceneSignalSegment(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	var builder strings.Builder
	lastHyphen := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastHyphen = false
		case !lastHyphen && builder.Len() > 0:
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return fallback
	}
	return out
}

func cloneNodes(nodes []Node) []Node {
	out := make([]Node, len(nodes))
	for i, node := range nodes {
		out[i] = cloneNode(node)
	}
	return out
}

func cloneNode(node Node) Node {
	node.Props = cloneNodeProps(node.Props)
	node.Children = append([]int(nil), node.Children...)
	return node
}

func cloneNodeProps(props map[string]islandprogram.ExprID) map[string]islandprogram.ExprID {
	if len(props) == 0 {
		return nil
	}
	out := make(map[string]islandprogram.ExprID, len(props))
	for key, value := range props {
		out[key] = value
	}
	return out
}

func handlesToChildren(handles []Handle) []int {
	if len(handles) == 0 {
		return nil
	}
	out := make([]int, 0, len(handles))
	for _, handle := range handles {
		if handle < 0 {
			continue
		}
		out = append(out, int(handle))
	}
	return out
}

func remapExprID(id islandprogram.ExprID, offset islandprogram.ExprID) islandprogram.ExprID {
	if id < 0 {
		return id
	}
	return id + offset
}
