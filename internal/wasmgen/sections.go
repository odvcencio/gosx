package wasmgen

import (
	"fmt"
	"slices"
)

var header = [...]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

func appendName(dst []byte, name string) []byte {
	dst = AppendU32(dst, uint32(len(name)))
	return append(dst, name...)
}

func appendSection(dst []byte, id byte, payload []byte) []byte {
	dst = append(dst, id)
	dst = AppendU32(dst, uint32(len(payload)))
	return append(dst, payload...)
}

func signatureKey(s Signature) string {
	key := make([]byte, len(s.Params)+1)
	for i, typ := range s.Params {
		key[i] = byte(typ)
	}
	key[len(s.Params)] = byte(s.Result)
	return string(key)
}

func compareSignatures(a, b Signature) int {
	if n := slices.Compare(a.Params, b.Params); n != 0 {
		return n
	}
	if a.Result < b.Result {
		return -1
	}
	if a.Result > b.Result {
		return 1
	}
	return 0
}

func (m Module) signatureTable() ([]Signature, map[string]uint32) {
	types := []Signature{}
	seen := map[string]bool{}
	add := func(s Signature) {
		key := signatureKey(s)
		if !seen[key] {
			seen[key] = true
			types = append(types, s)
		}
	}
	for _, imp := range m.Imports {
		add(imp.Signature)
	}
	for _, fn := range m.Functions {
		add(fn.Signature)
	}
	slices.SortFunc(types, compareSignatures)
	indices := make(map[string]uint32, len(types))
	for i, typ := range types {
		indices[signatureKey(typ)] = uint32(i)
	}
	return types, indices
}

// Encode writes canonical section framing, indices and declarations. Body
// stack/control validation is separate from this structural encoder.
func Encode(m Module) ([]byte, error) {
	if err := m.check(); err != nil {
		return nil, err
	}
	types, indices := m.signatureTable()
	module := append([]byte{}, header[:]...)
	b := AppendU32(nil, uint32(len(types)))
	for _, typ := range types {
		b = append(b, 0x60)
		b = AppendU32(b, uint32(len(typ.Params)))
		for _, param := range typ.Params {
			b = append(b, byte(param))
		}
		if typ.Result == Void {
			b = append(b, 0)
		} else {
			b = append(b, 1, byte(typ.Result))
		}
	}
	module = appendSection(module, 1, b)
	b = AppendU32(nil, uint32(len(m.Imports)))
	for _, imp := range m.Imports {
		b = appendName(b, imp.Module)
		b = appendName(b, imp.Name)
		b = append(b, 0) // Function import, followed by type index.
		b = AppendU32(b, indices[signatureKey(imp.Signature)])
	}
	module = appendSection(module, 2, b)
	b = AppendU32(nil, uint32(len(m.Functions)))
	for _, fn := range m.Functions {
		b = AppendU32(b, indices[signatureKey(fn.Signature)])
	}
	module = appendSection(module, 3, b)
	module = appendSection(module, 5, []byte{1, 1, MemoryPages, MemoryPages})
	b = AppendU32(nil, uint32(len(m.Globals)))
	for _, global := range m.Globals {
		mutable := byte(0)
		if global.Mutable {
			mutable = 1
		}
		b = append(b, byte(I32), mutable, 0x41)
		b = AppendI32(b, global.Initial)
		b = append(b, 0x0b)
	}
	module = appendSection(module, 6, b)
	exports := append([]Export{}, m.Exports...)
	exports = append(exports, Export{Name: "memory"})
	slices.SortFunc(exports, func(a, b Export) int { return compareNames(a.Name, b.Name) })
	b = AppendU32(nil, uint32(len(exports)))
	for _, exp := range exports {
		b = appendName(b, exp.Name)
		if exp.Name == "memory" {
			b = append(b, 2, 0)
		} else {
			b = append(b, 0)
			b = AppendU32(b, exp.Function)
		}
	}
	module = appendSection(module, 7, b)
	b = AppendU32(nil, uint32(len(m.Functions)))
	for _, fn := range m.Functions {
		groups := uint32(0)
		if fn.I32Locals != 0 {
			groups++
		}
		if fn.I64Locals != 0 {
			groups++
		}
		body := AppendU32(nil, groups)
		if fn.I32Locals != 0 {
			body = AppendU32(body, fn.I32Locals)
			body = append(body, byte(I32))
		}
		if fn.I64Locals != 0 {
			body = AppendU32(body, fn.I64Locals)
			body = append(body, byte(I64))
		}
		body = append(body, fn.Body...)
		b = AppendU32(b, uint32(len(body)))
		b = append(b, body...)
	}
	module = appendSection(module, 10, b)
	b = []byte{1, 0, 0x41} // One active segment on memory zero.
	b = AppendI32(b, ConstantOffset)
	b = append(b, 0x0b)
	b = AppendU32(b, uint32(len(m.Data)))
	b = append(b, m.Data...)
	module = appendSection(module, 11, b)
	b = appendName(nil, CustomName)
	b = append(b, m.Metadata...)
	module = appendSection(module, 0, b)
	if len(module) > MaxModuleBytes {
		return nil, fmt.Errorf("module byte limit")
	}
	return module, nil
}

func compareNames(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
