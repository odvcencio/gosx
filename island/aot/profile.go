// Package aot defines proved scalar inputs for ahead-of-time island compilation.
package aot

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"m31labs.dev/gosx/island/program"
)

type Profile uint32

const ScalarDOMV1 Profile = 1

// Limits are fixed by ScalarDOMV1; callers cannot widen them.
type Limits struct {
	Nodes, Expressions, Handlers, Computeds                         uint32
	NodeDepth, ExpressionDepth, ComputedDepth                       uint32
	Signals, Inputs, Programs, Instances, SharedNames               uint32
	Values, StringBytes, InputBytes, CommittedStringBytes           uint32
	Patches, PendingEvents, QueueBytes, ConstantsBytes, MemoryBytes uint32
}

func ProfileLimits() Limits {
	return Limits{256, 1024, 32, 16, 64, 64, 64, 16, 16, 16, 16, 64,
		512, 4096, 32768, 16384, 128, 32, 256 * 1024, 16384, 196608}
}

type ScalarKind string

const (
	Int          ScalarKind = "int"
	Int32        ScalarKind = "int32"
	Bool         ScalarKind = "bool"
	String       ScalarKind = "string"
	AnyZero      ScalarKind = "any-zero"
	SelectorPath ScalarKind = "selector-path"
)

// ScalarContract has a closed, ordered JSON representation. Locations and
// diagnostics do not enter the identity of an artifact.
type ScalarContract struct {
	Version     uint32               `json:"version"`
	Component   string               `json:"component"`
	Expressions []ExpressionContract `json:"expressions"`
	Inputs      []InputContract      `json:"inputs"`
	Signals     []StateContract      `json:"signals"`
	Computeds   []StateContract      `json:"computeds"`
	Bindings    []BindingContract    `json:"bindings"`
}

type ExpressionContract struct {
	Expr program.ExprID `json:"expr"`
	Kind ScalarKind     `json:"kind"`
	Pure bool           `json:"pure"`
}

type InputContract struct {
	ID     uint32           `json:"id"`
	Source string           `json:"source"`
	Root   string           `json:"root"`
	Path   []string         `json:"path"`
	Kind   ScalarKind       `json:"kind"`
	Exprs  []program.ExprID `json:"exprs"`
}

type StateContract struct {
	Slot uint32     `json:"slot"`
	Name string     `json:"name"`
	Kind ScalarKind `json:"kind"`
}

type BindingContract struct {
	ID         uint32           `json:"id"`
	Nodes      []program.NodeID `json:"nodes"`
	Kind       program.NodeKind `json:"kind"`
	Tag        string           `json:"tag"`
	Attributes []string         `json:"attributes"`
}

// Unit binds source evidence to a snapshot of the existing binary program.
// Consumers revalidate the identity before accepting mutable caller data.
type Unit struct {
	Component    string
	Program      *program.Program
	ProgramBytes []byte
	ProgramSHA   [32]byte
	Contract     ScalarContract
	ContractSHA  [32]byte
	Digest       [32]byte
}

// NewUnit canonicalizes the contract arrays and snapshots the program without
// narrowing any count or string length in the version-1 encoder.
func NewUnit(component string, p *program.Program, c ScalarContract) (Unit, error) {
	if component == "" || !utf8.ValidString(component) || c.Component != component || c.Version != 1 {
		return Unit{}, fmt.Errorf("invalid scalar contract identity")
	}
	if p == nil || p.Version != "" || p.Surface != program.SurfaceDOM || len(p.Funcs) != 0 || len(p.EngineNodes) != 0 || p.MaxCallDepth != 0 {
		return Unit{}, fmt.Errorf("unsupported program envelope")
	}
	if err := validateWireRanges(p); err != nil {
		return Unit{}, err
	}
	raw, err := program.EncodeBinary(p)
	if err != nil {
		return Unit{}, err
	}
	copyProgram, err := program.DecodeBinary(raw)
	if err != nil {
		return Unit{}, err
	}
	// Marshal/unmarshal owns all contract slices as well as its program tables.
	if c.Expressions == nil {
		c.Expressions = []ExpressionContract{}
	}
	if c.Inputs == nil {
		c.Inputs = []InputContract{}
	}
	if c.Signals == nil {
		c.Signals = []StateContract{}
	}
	if c.Computeds == nil {
		c.Computeds = []StateContract{}
	}
	if c.Bindings == nil {
		c.Bindings = []BindingContract{}
	}
	c.Inputs = append([]InputContract{}, c.Inputs...)
	c.Bindings = append([]BindingContract{}, c.Bindings...)
	for i := range c.Inputs {
		if c.Inputs[i].Path == nil {
			c.Inputs[i].Path = []string{}
		}
		if c.Inputs[i].Exprs == nil {
			c.Inputs[i].Exprs = []program.ExprID{}
		}
	}
	for i := range c.Bindings {
		if c.Bindings[i].Nodes == nil {
			c.Bindings[i].Nodes = []program.NodeID{}
		}
		if c.Bindings[i].Attributes == nil {
			c.Bindings[i].Attributes = []string{}
		}
	}
	contractBytes, err := json.Marshal(c)
	if err != nil {
		return Unit{}, err
	}
	var owned ScalarContract
	if err := json.Unmarshal(contractBytes, &owned); err != nil {
		return Unit{}, err
	}
	u := Unit{Component: component, Program: copyProgram, ProgramBytes: raw,
		ProgramSHA: sha256.Sum256(raw), Contract: owned, ContractSHA: sha256.Sum256(contractBytes)}
	identity := append([]byte("gosx-aot-unit-v1\x00"), binary.LittleEndian.AppendUint32(nil, uint32(len(component)))...)
	identity = append(identity, component...)
	identity = append(identity, u.ProgramSHA[:]...)
	identity = append(identity, u.ContractSHA[:]...)
	u.Digest = sha256.Sum256(identity)
	return u, nil
}

func validateWireRanges(p *program.Program) error {
	counts := []int{len(p.Props), len(p.Nodes), len(p.Exprs), len(p.Signals), len(p.Computeds), len(p.Handlers), len(p.StaticMask)}
	strings := []string{p.Name, p.Version}
	for _, prop := range p.Props {
		strings = append(strings, prop.Name)
	}
	for _, node := range p.Nodes {
		counts = append(counts, len(node.Attrs), len(node.Children))
		strings = append(strings, node.Tag, node.Text)
		for _, attr := range node.Attrs {
			strings = append(strings, attr.Name, attr.Value, attr.Event)
		}
	}
	for _, expr := range p.Exprs {
		counts = append(counts, len(expr.Operands))
		strings = append(strings, expr.Value)
	}
	for _, signal := range p.Signals {
		strings = append(strings, signal.Name)
	}
	for _, computed := range p.Computeds {
		strings = append(strings, computed.Name)
	}
	for _, handler := range p.Handlers {
		counts = append(counts, len(handler.Body))
		strings = append(strings, handler.Name)
	}
	for _, count := range counts {
		if count > 65535 {
			return fmt.Errorf("program count exceeds binary version-1 range")
		}
	}
	unique := make(map[string]bool)
	for _, value := range strings {
		if len(value) > 65535 || !utf8.ValidString(value) {
			return fmt.Errorf("program string exceeds binary version-1 range or is invalid UTF-8")
		}
		unique[value] = true
	}
	if len(unique) > 65535 {
		return fmt.Errorf("program string table exceeds binary version-1 range")
	}
	return nil
}

// ContractBindings groups adjacent source text into physical DOM bindings.
// It rejects cycles, repeated ownership and non-fixed node kinds.
func ContractBindings(p *program.Program) ([]BindingContract, error) {
	if p == nil || int(p.Root) >= len(p.Nodes) {
		return nil, fmt.Errorf("invalid binding root")
	}
	bindings := []BindingContract{}
	seen := make([]bool, len(p.Nodes))
	var walk func(program.NodeID, int) error
	walk = func(id program.NodeID, depth int) error {
		if int(id) >= len(p.Nodes) || depth > 64 || seen[id] {
			return fmt.Errorf("invalid binding graph at node %d", id)
		}
		seen[id] = true
		n := p.Nodes[id]
		if n.Kind != program.NodeElement && n.Kind != program.NodeText && n.Kind != program.NodeExpr {
			return fmt.Errorf("unsupported binding node %d", id)
		}
		b := BindingContract{ID: uint32(len(bindings)), Nodes: []program.NodeID{id}, Kind: n.Kind, Tag: n.Tag, Attributes: []string{}}
		if n.Kind != program.NodeElement {
			if len(n.Children) != 0 || len(n.Attrs) != 0 {
				return fmt.Errorf("invalid text node %d", id)
			}
			b.Kind, b.Tag = program.NodeText, ""
		}
		for _, attr := range n.Attrs {
			if attr.Kind != program.AttrEvent {
				b.Attributes = append(b.Attributes, attr.Name)
			}
		}
		bindings = append(bindings, b)
		for i := 0; i < len(n.Children); {
			child := n.Children[i]
			if err := walk(child, depth+1); err != nil {
				return err
			}
			i++
			if p.Nodes[child].Kind == program.NodeElement {
				continue
			}
			binding := len(bindings) - 1
			for i < len(n.Children) {
				next := n.Children[i]
				if int(next) >= len(p.Nodes) {
					return fmt.Errorf("invalid child %d", next)
				}
				if p.Nodes[next].Kind == program.NodeElement {
					break
				}
				if err := walk(next, depth+1); err != nil {
					return err
				}
				bindings[binding].Nodes = append(bindings[binding].Nodes, next)
				bindings = bindings[:len(bindings)-1]
				i++
			}
		}
		return nil
	}
	if err := walk(p.Root, 1); err != nil {
		return nil, err
	}
	for i, visited := range seen {
		if !visited {
			return nil, fmt.Errorf("unowned binding node %d", i)
		}
	}
	return bindings, nil
}
