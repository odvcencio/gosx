// Package wasmgen encodes the integer-only core WebAssembly subset used by
// compiled islands. Encoding uses Go alone and does not invoke an assembler.
package wasmgen

import (
	"fmt"
	"math"
	"unicode/utf8"
)

// ValueType is a core WebAssembly value type; Void denotes no result.
type ValueType byte

const (
	Void ValueType = 0
	I32  ValueType = 0x7f
	I64  ValueType = 0x7e

	MemoryPages    = 3
	ConstantOffset = 1024
	MaxDataBytes   = 16384
	MaxModuleBytes = 65536
	CustomName     = "gosx.aot.v1"
)

// Signature preserves parameter order and permits at most one integer result.
type Signature struct {
	Params []ValueType
	Result ValueType
}

// Import is a function import. Its position is its function index.
type Import struct {
	Module, Name string
	Signature    Signature
}

// Function contains instructions including the final end opcode. Locals are
// grouped as i32 then i64; parameters precede both groups in the local indices.
type Function struct {
	Signature            Signature
	I32Locals, I64Locals uint32
	Body                 []byte
}

// Global is an internal i32 cursor with a constant initializer.
type Global struct {
	Mutable bool
	Initial int32
}

// Export identifies a function by its index, including imported functions.
// Memory is exported automatically as "memory"; globals cannot be exported.
type Export struct {
	Name     string
	Function uint32
}

// Module preserves function/global order chosen by the compiler. Encode
// canonicalizes signatures and exports without modifying these input slices.
// Data occupies one active segment at ConstantOffset; Metadata is opaque.
type Module struct {
	Imports   []Import
	Functions []Function
	Globals   []Global
	Exports   []Export
	Data      []byte
	Metadata  []byte
}

// FunctionIndex returns the index of a defined function after all imports.
func (m Module) FunctionIndex(local uint32) (uint32, error) {
	index := uint64(len(m.Imports)) + uint64(local)
	if uint64(local) >= uint64(len(m.Functions)) || index > math.MaxUint32 {
		return 0, fmt.Errorf("defined function index out of range")
	}
	return uint32(index), nil
}

func (m Module) check() error {
	if len(m.Data) > MaxDataBytes || uint64(len(m.Imports))+uint64(len(m.Functions)) > math.MaxUint32 {
		return fmt.Errorf("module table or data limit")
	}
	// This lower bound prevents copying oversized instruction/name buffers.
	size := uint64(len(m.Data)) + uint64(len(m.Metadata))
	for i, imp := range m.Imports {
		if !utf8.ValidString(imp.Module) || !utf8.ValidString(imp.Name) {
			return fmt.Errorf("import %d: invalid UTF-8 name", i)
		}
		if err := checkSignature(imp.Signature); err != nil {
			return fmt.Errorf("import %d: %w", i, err)
		}
		size += uint64(len(imp.Module)) + uint64(len(imp.Name)) + uint64(len(imp.Signature.Params)) + 4
		if size > MaxModuleBytes {
			return fmt.Errorf("module byte limit")
		}
	}
	for i, fn := range m.Functions {
		if err := checkSignature(fn.Signature); err != nil {
			return fmt.Errorf("function %d: %w", i, err)
		}
		if uint64(len(fn.Signature.Params))+uint64(fn.I32Locals)+uint64(fn.I64Locals) > 65536 {
			return fmt.Errorf("function %d: local count limit", i)
		}
		if len(fn.Body) == 0 || fn.Body[len(fn.Body)-1] != 0x0b {
			return fmt.Errorf("function %d: missing final end", i)
		}
		size += uint64(len(fn.Body)) + uint64(len(fn.Signature.Params)) + 4
		if size > MaxModuleBytes {
			return fmt.Errorf("module byte limit")
		}
	}
	names := map[string]bool{"memory": true}
	for i, exp := range m.Exports {
		if !utf8.ValidString(exp.Name) || names[exp.Name] {
			return fmt.Errorf("export %d: invalid or duplicate name", i)
		}
		if uint64(exp.Function) >= uint64(len(m.Imports))+uint64(len(m.Functions)) {
			return fmt.Errorf("export %d: function index out of range", i)
		}
		names[exp.Name] = true
		size += uint64(len(exp.Name)) + 3
		if size > MaxModuleBytes {
			return fmt.Errorf("module byte limit")
		}
	}
	if size+uint64(len(m.Globals))*5 > MaxModuleBytes {
		return fmt.Errorf("module byte limit")
	}
	return nil
}

func checkSignature(s Signature) error {
	if len(s.Params) > 65536 || s.Result != Void && s.Result != I32 && s.Result != I64 {
		return fmt.Errorf("unsupported signature")
	}
	for _, typ := range s.Params {
		if typ != I32 && typ != I64 {
			return fmt.Errorf("unsupported parameter type")
		}
	}
	return nil
}
