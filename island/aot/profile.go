// Package aot defines proved scalar inputs for ahead-of-time island compilation.
package aot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
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

// NewJSONUnit reads the closed program wire shape before canonicalization.
// Duplicate or unknown fields cannot be erased into a supported program.
func NewJSONUnit(component string, data []byte, contract ScalarContract) (Unit, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(data) {
		return Unit{}, fmt.Errorf("invalid program JSON framing")
	}
	if err := validateJSONSurrogates(data); err != nil {
		return Unit{}, err
	}
	shape := json.NewDecoder(bytes.NewReader(data))
	shape.UseNumber()
	if err := programJSONShape(shape, reflect.TypeOf(program.Program{}), 0); err != nil {
		return Unit{}, err
	}
	if _, err := shape.Token(); err != io.EOF {
		return Unit{}, fmt.Errorf("trailing program JSON data")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var p program.Program
	if err := decoder.Decode(&p); err != nil {
		return Unit{}, fmt.Errorf("program JSON decode: %w", err)
	}
	p.Surface = program.SurfaceDOM
	return admittedUnit(component, &p, contract)
}

// encoding/json replaces unpaired surrogate escapes with U+FFFD. Check raw
// string escapes before either decoder can erase that distinction.
func validateJSONSurrogates(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		if len(data)-i < 2 {
			return fmt.Errorf("truncated program JSON escape")
		}
		if data[i+1] != 'u' {
			// Escaped quotes and backslashes are string content, so neither
			// changes the scan state nor starts a Unicode escape.
			i++
			continue
		}
		code, ok := jsonCodeUnit(data[i:])
		if !ok {
			return fmt.Errorf("invalid program JSON Unicode escape")
		}
		i += 5
		if code >= 0xd800 && code <= 0xdbff {
			low, ok := jsonCodeUnit(data[i+1:])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired high surrogate in program JSON string")
			}
			i += 6
		} else if code >= 0xdc00 && code <= 0xdfff {
			return fmt.Errorf("unpaired low surrogate in program JSON string")
		}
	}
	return nil
}

func jsonCodeUnit(data []byte) (uint16, bool) {
	if len(data) < 6 || data[0] != '\\' || data[1] != 'u' {
		return 0, false
	}
	var code uint16
	for _, digit := range data[2:6] {
		code <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			code |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			code |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			code |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return code, true
}

func programJSONShape(decoder *json.Decoder, typ reflect.Type, depth int) error {
	if depth > 64 {
		return fmt.Errorf("program JSON nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("program JSON token: %w", err)
	}
	if token == nil {
		return nil
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	if delimiter == '[' {
		if typ.Kind() != reflect.Slice && typ.Kind() != reflect.Array {
			return fmt.Errorf("unexpected program JSON array")
		}
		for decoder.More() {
			if err := programJSONShape(decoder, typ.Elem(), depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("invalid program JSON array")
		}
		return nil
	}
	if delimiter != '{' || (typ.Kind() != reflect.Struct && typ.Kind() != reflect.Map) {
		return fmt.Errorf("unexpected program JSON object")
	}
	fields := map[string]reflect.Type{}
	if typ.Kind() == reflect.Struct {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if field.PkgPath != "" || name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = field.Type
		}
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("invalid program JSON key")
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return fmt.Errorf("duplicate program JSON field")
		}
		seen[name] = true
		field, known := fields[name]
		if typ.Kind() == reflect.Map {
			field, known = typ.Elem(), true
		}
		if !known {
			return fmt.Errorf("unknown program JSON field")
		}
		if err := programJSONShape(decoder, field, depth+1); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return fmt.Errorf("invalid program JSON object")
	}
	return nil
}

// NewBinaryUnit accepts the complete canonical version-1 envelope. Framing
// checks precede the compatible VM decoder, which skips unknown sections.
func NewBinaryUnit(component string, data []byte, contract ScalarContract) (Unit, error) {
	if len(data) < 8 || !bytes.Equal(data[:4], []byte{'G', 'S', 'X', 0}) || binary.LittleEndian.Uint16(data[4:6]) != 1 || binary.LittleEndian.Uint16(data[6:8]) != 11 {
		return Unit{}, fmt.Errorf("invalid program binary header")
	}
	cursor := uint64(8)
	seen := [11]bool{}
	for range 11 {
		if cursor+5 > uint64(len(data)) {
			return Unit{}, fmt.Errorf("truncated program section header")
		}
		tag := data[cursor]
		length := uint64(binary.LittleEndian.Uint32(data[cursor+1 : cursor+5]))
		if tag >= 11 || seen[tag] {
			return Unit{}, fmt.Errorf("unknown or duplicate program section")
		}
		seen[tag] = true
		cursor += 5
		if length > uint64(len(data))-cursor {
			return Unit{}, fmt.Errorf("truncated program section")
		}
		cursor += length
	}
	if cursor != uint64(len(data)) {
		return Unit{}, fmt.Errorf("trailing program binary data")
	}
	p, err := program.DecodeBinary(data)
	if err != nil {
		return Unit{}, fmt.Errorf("program binary decode: %w", err)
	}
	u, err := admittedUnit(component, p, contract)
	if err != nil {
		return Unit{}, err
	}
	// Canonical equality also rejects ignored bytes inside sections, invalid
	// string references, and alternate flag encodings lost during decoding.
	if !bytes.Equal(data, u.ProgramBytes) {
		return Unit{}, fmt.Errorf("noncanonical program binary data")
	}
	return u, nil
}

func admittedUnit(component string, p *program.Program, contract ScalarContract) (Unit, error) {
	u, err := NewUnit(component, p, contract)
	if err != nil {
		return Unit{}, err
	}
	if receipt := Classify(u, ScalarDOMV1); !receipt.Eligible {
		return Unit{}, fmt.Errorf("program profile: %s[%d]: %s", receipt.Table, receipt.Index, receipt.Reason)
	}
	return u, nil
}
