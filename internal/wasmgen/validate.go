package wasmgen

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

type binaryReader struct {
	data []byte
	pos  int
	err  error
}

func (r *binaryReader) fail(message string) {
	if r.err == nil {
		r.err = fmt.Errorf("wasm byte %d: %s", r.pos, message)
	}
}

func (r *binaryReader) byte() byte {
	if r.err != nil {
		return 0
	}
	if r.pos >= len(r.data) {
		r.fail("truncated input")
		return 0
	}
	b := r.data[r.pos]
	r.pos++
	return b
}

func (r *binaryReader) take(size uint32) []byte {
	if r.err != nil {
		return nil
	}
	if uint64(size) > uint64(len(r.data)-r.pos) {
		r.fail("length exceeds input")
		return nil
	}
	end := r.pos + int(size)
	b := r.data[r.pos:end]
	r.pos = end
	return b
}

func (r *binaryReader) u32() uint32 {
	start := r.pos
	var value uint32
	for i := uint(0); i < 5 && r.err == nil; i++ {
		b := r.byte()
		if i == 4 && b&0xf0 != 0 {
			r.fail("u32 LEB overflow")
			return 0
		}
		value |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			if !bytes.Equal(r.data[start:r.pos], AppendU32(nil, value)) {
				r.fail("nonminimal u32 LEB")
			}
			return value
		}
	}
	r.fail("unterminated u32 LEB")
	return 0
}

func (r *binaryReader) signed(bits uint) int64 {
	start := r.pos
	var value uint64
	for i := uint(0); i < (bits+6)/7 && r.err == nil; i++ {
		b := r.byte()
		if i == (bits+6)/7-1 {
			mask := byte(0x78)
			if bits == 64 {
				mask = 0x7e
			}
			if b&0x80 != 0 || b&mask != 0 && b&mask != mask {
				r.fail("signed LEB overflow")
				return 0
			}
		}
		value |= uint64(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			shift := 7 * (i + 1)
			if b&0x40 != 0 && shift < 64 {
				value |= ^uint64(0) << shift
			}
			result := int64(value)
			encoded := AppendI64(nil, result)
			if bits == 32 {
				result = int64(int32(result))
				encoded = AppendI32(nil, int32(result))
			}
			if !bytes.Equal(r.data[start:r.pos], encoded) {
				r.fail("nonminimal signed LEB")
			}
			return result
		}
	}
	r.fail("unterminated signed LEB")
	return 0
}

func (r *binaryReader) count() uint32 {
	n := r.u32()
	if uint64(n) > uint64(len(r.data)-r.pos) {
		r.fail("count exceeds input")
		return 0
	}
	return n
}

func (r *binaryReader) name() string {
	b := r.take(r.u32())
	if !utf8.Valid(b) {
		r.fail("invalid UTF-8 name")
	}
	return string(b)
}

func (r *binaryReader) end() {
	if r.pos != len(r.data) {
		r.fail("unconsumed bytes")
	}
}

type validationEnv struct {
	types     []Signature
	functions []uint32
	defined   []uint32
	globals   []bool
}

// Validate checks the encoder's bounded integer core subset, canonical LEB,
// declarations and instruction stacks. The compiler checks the island ABI
// and metadata schema separately; this routine never executes a module.
func Validate(binary []byte) error {
	if len(binary) < len(header) || len(binary) > MaxModuleBytes || !bytes.Equal(binary[:8], header[:]) {
		return fmt.Errorf("invalid module header or size")
	}
	r := binaryReader{data: binary, pos: 8}
	env := validationEnv{}
	for _, expected := range []byte{1, 2, 3, 5, 6, 7, 10, 11, 0} {
		if r.byte() != expected {
			r.fail("unsupported, duplicate or misordered section")
		}
		s := binaryReader{data: r.take(r.u32())}
		if r.err != nil {
			return r.err
		}
		switch expected {
		case 1:
			for n := s.count(); n != 0 && s.err == nil; n-- {
				if s.byte() != 0x60 {
					s.fail("unsupported function type")
				}
				sig := Signature{}
				for n := s.count(); n != 0 && s.err == nil; n-- {
					sig.Params = append(sig.Params, ValueType(s.byte()))
				}
				results := s.count()
				if results > 1 {
					s.fail("multiple results")
				} else if results == 1 {
					sig.Result = ValueType(s.byte())
					if sig.Result != I32 && sig.Result != I64 {
						s.fail("unsupported result type")
					}
				}
				if err := checkSignature(sig); err != nil {
					s.fail("unsupported scalar type")
				}
				if len(env.types) > 0 && compareSignatures(env.types[len(env.types)-1], sig) >= 0 {
					s.fail("duplicate or unsorted type")
				}
				env.types = append(env.types, sig)
			}
		case 2, 3:
			for n := s.count(); n != 0 && s.err == nil; n-- {
				if expected == 2 {
					s.name()
					s.name()
					if s.byte() != 0 {
						s.fail("unsupported import kind")
					}
				}
				typ := s.u32()
				if uint64(typ) >= uint64(len(env.types)) {
					s.fail("function type index")
				}
				env.functions = append(env.functions, typ)
				if expected == 3 {
					env.defined = append(env.defined, typ)
				}
			}
		case 5:
			if s.u32() != 1 || s.byte() != 1 || s.u32() != MemoryPages || s.u32() != MemoryPages {
				s.fail("memory must have fixed three-page limits")
			}
		case 6:
			for n := s.count(); n != 0 && s.err == nil; n-- {
				if s.byte() != byte(I32) {
					s.fail("unsupported global type")
				}
				mutable := s.byte()
				if mutable > 1 || s.byte() != 0x41 {
					s.fail("unsupported global initializer")
				}
				s.signed(32)
				if s.byte() != 0x0b {
					s.fail("global initializer end")
				}
				env.globals = append(env.globals, mutable == 1)
			}
		case 7:
			previous, memory := "", false
			for n, i := s.count(), uint32(0); i < n && s.err == nil; i++ {
				name, kind, index := s.name(), s.byte(), s.u32()
				if i > 0 && name <= previous {
					s.fail("duplicate or unsorted export")
				}
				previous = name
				if name == "memory" {
					if kind != 2 || index != 0 || memory {
						s.fail("memory export")
					}
					memory = true
				} else if kind != 0 || uint64(index) >= uint64(len(env.functions)) {
					s.fail("unsupported export kind or index")
				}
			}
			if !memory {
				s.fail("missing memory export")
			}
		case 10:
			n := s.count()
			if uint64(n) != uint64(len(env.defined)) {
				s.fail("function/code count mismatch")
			}
			for i := uint32(0); i < n && s.err == nil; i++ {
				body := binaryReader{data: s.take(s.u32())}
				if s.err != nil {
					break
				}
				if err := validateCode(&body, env.types[env.defined[i]], env); err != nil {
					s.fail(fmt.Sprintf("function %d: %v", i, err))
				}
			}
		case 11:
			if s.u32() != 1 || s.byte() != 0 || s.byte() != 0x41 || s.signed(32) != ConstantOffset || s.byte() != 0x0b {
				s.fail("unsupported active data segment")
			}
			size := s.u32()
			if size > MaxDataBytes {
				s.fail("data byte limit")
			}
			s.take(size)
		case 0:
			if s.name() != CustomName {
				s.fail("unsupported custom section")
			}
			s.take(uint32(len(s.data) - s.pos)) // Opaque compiler-owned metadata.
		}
		s.end()
		if s.err != nil {
			return fmt.Errorf("section %d: %w", expected, s.err)
		}
	}
	r.end()
	return r.err
}
