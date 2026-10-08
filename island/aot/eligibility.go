package aot

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	htmltree "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"m31labs.dev/gosx/internal/htmlattr"
	"m31labs.dev/gosx/island/program"
)

// Eligibility binds a stable decision and offending table entry to its inputs.
// An index of -1 identifies a whole-table or envelope failure.
type Eligibility struct {
	Eligible                            bool
	Profile                             Profile
	UnitDigest, ProgramSHA, ContractSHA [32]byte
	Reason, Table                       string
	Index                               int
}

type rejection struct {
	reason, table string
	index         int
}

func reject(reason, table string, index int) *rejection { return &rejection{reason, table, index} }

// Classify checks every table before admitting the implemented scalar subset.
// Unsupported valid operations remain on the VM.
func Classify(u Unit, profile Profile) Eligibility {
	r := Eligibility{Profile: profile, UnitDigest: u.Digest, ProgramSHA: u.ProgramSHA, ContractSHA: u.ContractSHA, Index: -1}
	var failure *rejection
	if profile != ScalarDOMV1 {
		failure = reject("profile_unsupported", "", -1)
	} else {
		failure = classify(u)
	}
	if failure != nil {
		r.Reason, r.Table, r.Index = failure.reason, failure.table, failure.index
	} else {
		r.Eligible = true
	}
	return r
}

func classify(u Unit) *rejection {
	p, c := u.Program, u.Contract
	if p == nil {
		return reject("program_missing", "", -1)
	}
	if p.Version != "" || p.Surface != program.SurfaceDOM || len(p.Funcs) != 0 || len(p.EngineNodes) != 0 || p.MaxCallDepth != 0 {
		return reject("envelope_unsupported", "", -1)
	}
	limits := ProfileLimits()
	for _, bound := range []struct {
		table string
		count int
		max   uint32
	}{
		{"nodes", len(p.Nodes), limits.Nodes}, {"expressions", len(p.Exprs), limits.Expressions},
		{"handlers", len(p.Handlers), limits.Handlers}, {"computeds", len(p.Computeds), limits.Computeds},
		{"signals", len(p.Signals), limits.Signals}, {"inputs", len(c.Inputs), limits.Inputs},
	} {
		if uint64(bound.count) > uint64(bound.max) {
			return reject("limit", bound.table, -1)
		}
	}
	if c.Version != 1 || c.Component != u.Component || c.Expressions == nil || c.Inputs == nil || c.Signals == nil || c.Computeds == nil || c.Bindings == nil {
		return reject("contract_missing", "contract", -1)
	}
	if len(c.Expressions) != len(p.Exprs) || len(c.Signals) != len(p.Signals) || len(c.Computeds) != len(p.Computeds) {
		return reject("contract_count", "contract", -1)
	}
	if len(p.Nodes) == 0 || int(p.Root) >= len(p.Nodes) {
		return reject("reference", "root", int(p.Root))
	}
	if p.Nodes[p.Root].Kind != program.NodeElement {
		return reject("root_kind", "nodes", int(p.Root))
	}
	if len(p.StaticMask) != len(p.Nodes) {
		return reject("static_mask", "nodes", -1)
	}
	if err := validateWireRanges(p); err != nil {
		return reject("wire_range", "program", -1)
	}
	props := map[string]bool{}
	for i, prop := range p.Props {
		if !validScalarType(prop.Type) {
			return reject("type_unsupported", "props", i)
		}
		if prop.Name == "" || props[prop.Name] {
			return reject("duplicate_name", "props", i)
		}
		props[prop.Name] = true
	}
	states := map[string]program.ExprID{}
	mutable := map[string]bool{}
	for i, s := range p.Signals {
		if !validScalarType(s.Type) || !scalarKind(c.Signals[i].Kind) {
			return reject("type_unsupported", "signals", i)
		}
		if s.Name == "" {
			return reject("duplicate_name", "signals", i)
		}
		if _, exists := states[s.Name]; exists {
			return reject("duplicate_name", "signals", i)
		}
		if int(s.Init) >= len(p.Exprs) {
			return reject("reference", "signals", i)
		}
		if c.Signals[i].Slot != uint32(i) || c.Signals[i].Name != s.Name {
			return reject("contract_state", "signals", i)
		}
		states[s.Name], mutable[s.Name] = s.Init, true
	}
	for i, s := range p.Computeds {
		if !validScalarType(s.Type) || !scalarKind(c.Computeds[i].Kind) {
			return reject("type_unsupported", "computeds", i)
		}
		if s.Name == "" {
			return reject("duplicate_name", "computeds", i)
		}
		if _, exists := states[s.Name]; exists {
			return reject("duplicate_name", "computeds", i)
		}
		if int(s.Expr) >= len(p.Exprs) {
			return reject("reference", "computeds", i)
		}
		if c.Computeds[i].Slot != uint32(i) || c.Computeds[i].Name != s.Name {
			return reject("contract_state", "computeds", i)
		}
		states[s.Name] = s.Expr
	}
	handlers := map[string]bool{}
	for i, h := range p.Handlers {
		if h.Name == "" || handlers[h.Name] {
			return reject("duplicate_name", "handlers", i)
		}
		handlers[h.Name] = true
		if len(h.Body) > 64 {
			return reject("limit", "handlers", i)
		}
		for _, id := range h.Body {
			if int(id) >= len(p.Exprs) {
				return reject("reference", "handlers", i)
			}
		}
	}
	for i, e := range p.Exprs {
		if e.Op > program.OpClosure {
			return reject("opcode_unknown", "expressions", i)
		}
		if !validScalarType(e.Type) {
			return reject("type_unsupported", "expressions", i)
		}
		if c.Expressions[i].Expr != program.ExprID(i) || !contractKind(c.Expressions[i].Kind) {
			return reject("contract_expression", "expressions", i)
		}
		for _, id := range e.Operands {
			if int(id) >= len(p.Exprs) {
				return reject("reference", "expressions", i)
			}
		}
		if e.Op == program.OpSignalGet || e.Op == program.OpSignalSet {
			if _, exists := states[e.Value]; !exists || e.Op == program.OpSignalSet && !mutable[e.Value] {
				return reject("state_undeclared", "expressions", i)
			}
		}
	}
	if failure := expressionGraph(p, states, mutable); failure != nil {
		return failure
	}
	for i, n := range p.Nodes {
		if n.Kind != program.NodeElement && n.Kind != program.NodeText && n.Kind != program.NodeExpr {
			return reject("node_unsupported", "nodes", i)
		}
		if n.Kind == program.NodeExpr && int(n.Expr) >= len(p.Exprs) {
			return reject("reference", "nodes", i)
		}
		if n.Kind != program.NodeElement && (len(n.Attrs) != 0 || len(n.Children) != 0 || n.Tag != "") {
			return reject("node_shape", "nodes", i)
		}
		if n.Kind == program.NodeElement && !fixedTag(n.Tag) {
			return reject("topology_unsupported", "nodes", i)
		}
		if len(n.Text) > int(limits.StringBytes) {
			return reject("string_limit", "nodes", i)
		}
		attrs := map[string]bool{}
		buttonType := ""
		for _, a := range n.Attrs {
			if a.Kind > program.AttrEvent {
				return reject("attribute_unknown", "nodes", i)
			}
			name := strings.ToLower(a.Name)
			if attrs[name] {
				return reject("duplicate_attribute", "nodes", i)
			}
			attrs[name] = true
			if a.Kind == program.AttrEvent {
				if !handlers[a.Event] {
					return reject("handler_undeclared", "nodes", i)
				}
				if !fixedEvent(a.Name) || n.Tag == "a" {
					return reject("event_unsupported", "nodes", i)
				}
				continue
			}
			if !htmlattr.ValidName(a.Name) || strings.HasPrefix(name, "on") || strings.HasPrefix(name, "data-gosx-") || strings.HasPrefix(name, "data-on-") || name == "data-action" || name == "form" || name == "action" || name == "formaction" || name == "formmethod" || name == "key" {
				return reject("attribute_unsupported", "nodes", i)
			}
			if a.Kind == program.AttrExpr {
				if int(a.Expr) >= len(p.Exprs) {
					return reject("reference", "nodes", i)
				}
				if !fixedAttribute(a.Name, c.Expressions[a.Expr].Kind) {
					return reject("attribute_unsupported", "nodes", i)
				}
			} else if htmlattr.FilterURL(a.Name, a.Value) != a.Value || a.Kind == program.AttrBool && !htmlattr.IsBoolean(a.Name) {
				return reject("attribute_unsupported", "nodes", i)
			}
			if a.Kind == program.AttrStatic && name == "type" {
				buttonType = a.Value
			}
		}
		if n.Tag == "button" && buttonType != "button" {
			return reject("submit_ambiguous", "nodes", i)
		}
	}
	bindings, err := ContractBindings(p)
	if err != nil {
		return reject("node_graph", "nodes", -1)
	}
	if !reflect.DeepEqual(bindings, c.Bindings) {
		return reject("contract_binding", "bindings", -1)
	}
	if failure := staticAnalysis(p); failure != nil {
		return failure
	}
	if !parserTopology(p) {
		return reject("parser_topology", "nodes", -1)
	}
	inputRefs := make([]bool, len(p.Exprs))
	for i, input := range c.Inputs {
		if input.ID != uint32(i) || input.Source != "prop" && input.Source != "event" || input.Root == "" || !utf8.ValidString(input.Root) || !scalarKind(input.Kind) || input.Path == nil || len(input.Exprs) == 0 {
			return reject("contract_input", "inputs", i)
		}
		if i > 0 && compareInputs(c.Inputs[i-1], input) >= 0 {
			return reject("contract_input_order", "inputs", i)
		}
		for _, key := range input.Path {
			if key == "" || !utf8.ValidString(key) {
				return reject("contract_input", "inputs", i)
			}
		}
		for j, id := range input.Exprs {
			if int(id) >= len(p.Exprs) || inputRefs[id] || j > 0 && input.Exprs[j-1] >= id || c.Expressions[id].Kind != input.Kind {
				return reject("contract_input", "inputs", i)
			}
			inputRefs[id] = true
		}
	}
	if failure := literalSubset(u); failure != nil {
		return failure
	}
	canonical, err := NewUnit(u.Component, p, c)
	contractBytes, _ := json.Marshal(c)
	if err != nil || !bytes.Equal(canonical.ProgramBytes, u.ProgramBytes) || canonical.ProgramSHA != u.ProgramSHA || sha256.Sum256(contractBytes) != u.ContractSHA || canonical.Digest != u.Digest {
		return reject("unit_identity", "unit", -1)
	}
	for i, s := range p.Signals {
		kind := c.Expressions[s.Init].Kind
		if !sameScalar(kind, c.Signals[i].Kind) || !typeMatchesKind(s.Type, kind) {
			return reject("state_kind", "signals", i)
		}
	}
	for i, s := range p.Computeds {
		kind := c.Expressions[s.Expr].Kind
		if !sameScalar(kind, c.Computeds[i].Kind) || !typeMatchesKind(s.Type, kind) {
			return reject("state_kind", "computeds", i)
		}
	}
	return constantLimits(p)
}

func sameScalar(a, b ScalarKind) bool {
	return a == b || (a == Int || a == Int32) && (b == Int || b == Int32)
}
func typeMatchesKind(t program.ExprType, k ScalarKind) bool {
	return t == program.TypeAny || t == program.TypeString && k == String || t == program.TypeBool && k == Bool || t == program.TypeInt && (k == Int || k == Int32)
}

func constantLimits(p *program.Program) *rejection {
	seen := map[string]bool{}
	bytes := 0
	add := func(s string) bool {
		if len(s) > 4096 {
			return false
		}
		if !seen[s] {
			seen[s] = true
			bytes += len(s)
		}
		return true
	}
	for i, n := range p.Nodes {
		if !add(n.Tag) || !add(n.Text) {
			return reject("string_limit", "nodes", i)
		}
		for _, a := range n.Attrs {
			if !add(a.Name) || !add(a.Value) {
				return reject("string_limit", "nodes", i)
			}
		}
	}
	for i, e := range p.Exprs {
		if (e.Op == program.OpLitString || e.Op == program.OpFormat) && !add(e.Value) {
			return reject("string_limit", "expressions", i)
		}
	}
	if bytes > 16384 {
		return reject("constants_limit", "expressions", -1)
	}
	return nil
}

func validScalarType(t program.ExprType) bool {
	return t == program.TypeString || t == program.TypeInt || t == program.TypeBool || t == program.TypeAny
}
func scalarKind(k ScalarKind) bool   { return k == Int || k == Int32 || k == Bool || k == String }
func contractKind(k ScalarKind) bool { return scalarKind(k) || k == AnyZero || k == SelectorPath }

func compareInputs(a, b InputContract) int {
	if n := strings.Compare(a.Source, b.Source); n != 0 {
		return n
	}
	if n := strings.Compare(a.Root, b.Root); n != 0 {
		return n
	}
	if n := slices.Compare(a.Path, b.Path); n != 0 {
		return n
	}
	return strings.Compare(string(a.Kind), string(b.Kind))
}

func expressionGraph(p *program.Program, states map[string]program.ExprID, mutable map[string]bool) *rejection {
	status := make([]uint8, len(p.Exprs))
	heights := make([]int, len(p.Exprs))
	var walk func(program.ExprID, int) (int, *rejection)
	walk = func(id program.ExprID, depth int) (int, *rejection) {
		if status[id] == 1 {
			return 0, reject("expression_cycle", "expressions", int(id))
		}
		if status[id] == 2 {
			return heights[id], nil
		}
		if depth > 64 {
			return 0, reject("expression_depth", "expressions", int(id))
		}
		status[id] = 1
		e := p.Exprs[id]
		edges := append([]program.ExprID{}, e.Operands...)
		if e.Op == program.OpSignalGet && !mutable[e.Value] {
			edges = append(edges, states[e.Value])
		}
		height := 1
		for _, child := range edges {
			h, failure := walk(child, depth+1)
			if failure != nil {
				return 0, failure
			}
			height = max(height, h+1)
		}
		if height > 64 {
			return 0, reject("expression_depth", "expressions", int(id))
		}
		status[id], heights[id] = 2, height
		return height, nil
	}
	for id := range p.Exprs {
		if _, failure := walk(program.ExprID(id), 1); failure != nil {
			return failure
		}
	}
	return nil
}

func staticAnalysis(p *program.Program) *rejection {
	var walk func(program.NodeID) (bool, *rejection)
	walk = func(id program.NodeID) (bool, *rejection) {
		n := p.Nodes[id]
		static := n.Kind != program.NodeExpr
		for _, a := range n.Attrs {
			if a.Kind == program.AttrExpr || a.Kind == program.AttrEvent {
				static = false
			}
		}
		for _, child := range n.Children {
			s, failure := walk(child)
			if failure != nil {
				return false, failure
			}
			static = static && s
		}
		if p.StaticMask[id] != static {
			return false, reject("static_mask", "nodes", int(id))
		}
		return static, nil
	}
	_, failure := walk(p.Root)
	return failure
}

func fixedTag(tag string) bool {
	return strings.Contains(" div span button input textarea select option optgroup label ul ol li h1 h2 h3 h4 h5 h6 p section article header footer main nav aside strong em small b i u s br hr img a dl dt dd figure figcaption pre code blockquote ", " "+tag+" ") && htmlattr.ValidTag(tag)
}

func fixedEvent(name string) bool {
	switch name {
	case "click", "input", "change", "keydown", "keyup", "focus", "blur",
		"onClick", "onInput", "onChange", "onKeyDown", "onKeyUp", "onFocus", "onBlur":
		return true
	default:
		return false
	}
}

func fixedAttribute(name string, kind ScalarKind) bool {
	if name == "class" || name == "id" || name == "title" {
		return kind == String
	}
	if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "aria-") {
		return scalarKind(kind)
	}
	if name == "tabindex" {
		return kind == Int || kind == Int32
	}
	if name == "value" {
		return scalarKind(kind)
	}
	return strings.Contains(" checked disabled hidden selected required readonly multiple ", " "+name+" ") && kind == Bool
}

// Compare a marked fixed skeleton with the HTML parser's physical tree. This
// rejects implicit closing, reparenting, void children and invalid select trees.
func parserTopology(p *program.Program) bool {
	var source strings.Builder
	expected := []string{}
	var emit func(program.NodeID)
	emit = func(id program.NodeID) {
		n := p.Nodes[id]
		if n.Kind != program.NodeElement {
			source.WriteByte('x')
			return
		}
		marker := strconv.Itoa(int(id))
		expected = append(expected, n.Tag+":"+marker)
		source.WriteString("<" + n.Tag + " data-aot-node=\"" + marker + "\">")
		text := false
		for _, child := range n.Children {
			if p.Nodes[child].Kind != program.NodeElement {
				if !text {
					expected = append(expected, "text")
					text = true
				}
			} else {
				text = false
			}
			emit(child)
		}
		if !strings.Contains(" input br hr img ", " "+n.Tag+" ") {
			source.WriteString("</" + n.Tag + ">")
		}
		expected = append(expected, "/"+n.Tag+":"+marker)
	}
	emit(p.Root)
	nodes, err := htmltree.ParseFragment(strings.NewReader(source.String()), &htmltree.Node{Type: htmltree.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return false
	}
	actual := []string{}
	var inspect func(*htmltree.Node)
	inspect = func(n *htmltree.Node) {
		marker := ""
		if n.Type == htmltree.TextNode {
			actual = append(actual, "text")
		} else if n.Type == htmltree.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key == "data-aot-node" {
					marker = attr.Val
				}
			}
			actual = append(actual, n.Data+":"+marker)
		} else {
			actual = append(actual, "unsupported")
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			inspect(child)
		}
		if n.Type == htmltree.ElementNode {
			actual = append(actual, "/"+n.Data+":"+marker)
		}
	}
	for _, node := range nodes {
		inspect(node)
	}
	return slices.Equal(expected, actual)
}

func literalSubset(u Unit) *rejection {
	for i, e := range u.Program.Exprs {
		proof := u.Contract.Expressions[i]
		if e.Op != program.OpLitString && e.Op != program.OpLitInt && e.Op != program.OpLitBool {
			return reject("opcode_unsupported", "expressions", i)
		}
		if len(e.Operands) != 0 {
			return reject("arity", "expressions", i)
		}
		if !proof.Pure {
			return reject("purity", "expressions", i)
		}
		switch e.Op {
		case program.OpLitString:
			if e.Type != program.TypeString || proof.Kind != String {
				return reject("type_mismatch", "expressions", i)
			}
			if len(e.Value) > 4096 {
				return reject("string_limit", "expressions", i)
			}
		case program.OpLitInt:
			if e.Type != program.TypeInt || proof.Kind != Int && proof.Kind != Int32 {
				return reject("type_mismatch", "expressions", i)
			}
			value, err := strconv.ParseInt(e.Value, 10, 32)
			if err != nil || strconv.FormatInt(value, 10) != e.Value {
				return reject("integer_literal", "expressions", i)
			}
		case program.OpLitBool:
			if e.Type != program.TypeBool || proof.Kind != Bool {
				return reject("type_mismatch", "expressions", i)
			}
			if e.Value != "true" && e.Value != "false" {
				return reject("boolean_literal", "expressions", i)
			}
		}
	}
	if len(u.Contract.Inputs) != 0 {
		return reject("input_unsupported", "inputs", -1)
	}
	return nil
}
