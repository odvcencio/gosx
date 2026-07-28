package bridge

import (
	"strconv"
	"unicode/utf8"

	"m31labs.dev/gosx/client/vm"
)

// Hand-rolled JSON encoding for []vm.PatchOp.
//
// Every reconcile ships its op list across the WASM boundary as a JSON string.
// Measured on the js/wasm target with a 100-row list build (800 ops, 43,681
// bytes), the old encoding/json call took about 380 microseconds, while the
// string copy across syscall/js took 5 and the browser's JSON.parse took 168.
// The encode, not the boundary, was the cost. This encoder brings it to about
// 151 microseconds, and the one-op batch a counter click produces from 650
// nanoseconds to 237.
//
// The output stays byte-identical to encoding/json's, so patch.js, the runtime
// tests and every recorded fixture keep working. patch_json_test.go pins that
// equality against a randomized corpus.

const lowerHex = "0123456789abcdef"

// lineSeparator and paragraphSeparator are valid inside a JSON string but
// break a JavaScript source literal, so encoding/json escapes both.
const (
	lineSeparator      = '\u2028'
	paragraphSeparator = '\u2029'
)

// appendPatchesJSON writes the JSON array form of patches into out.
//
// Field order and omission rules follow the struct tags on vm.PatchOp:
// kind and path always appear; tag, text, attrName and children appear only
// when non-empty.
func appendPatchesJSON(out []byte, patches []vm.PatchOp) []byte {
	if patches == nil {
		return append(out, "null"...)
	}
	out = append(out, '[')
	for i := range patches {
		if i > 0 {
			out = append(out, ',')
		}
		out = appendPatchOpJSON(out, &patches[i])
	}
	return append(out, ']')
}

// patchesJSONSize estimates the encoded length so the buffer needs no regrow.
//
// The walk touches lengths only, never string contents, so it costs a few
// nanoseconds per op. Escaping can still push a payload past the estimate; that
// costs one grow, which the previous fixed guess paid on every batch.
func patchesJSONSize(patches []vm.PatchOp) int {
	// Two brackets, plus one comma between each pair of ops.
	total := 2 + len(patches)
	for i := range patches {
		op := &patches[i]
		// {"kind":NNN,"path":"..."} — 22 bytes of frame.
		total += 22 + len(op.Path)
		if op.Tag != "" {
			total += 9 + len(op.Tag)
		}
		if op.Text != "" {
			total += 10 + len(op.Text)
		}
		if op.AttrName != "" {
			total += 14 + len(op.AttrName)
		}
		if n := len(op.Children); n > 0 {
			total += 14 + 8*n
		}
	}
	return total
}

func appendPatchOpJSON(out []byte, op *vm.PatchOp) []byte {
	out = append(out, `{"kind":`...)
	out = strconv.AppendUint(out, uint64(op.Kind), 10)
	out = append(out, `,"path":`...)
	out = appendJSONString(out, op.Path)
	if op.Tag != "" {
		out = append(out, `,"tag":`...)
		out = appendJSONString(out, op.Tag)
	}
	if op.Text != "" {
		out = append(out, `,"text":`...)
		out = appendJSONString(out, op.Text)
	}
	if op.AttrName != "" {
		out = append(out, `,"attrName":`...)
		out = appendJSONString(out, op.AttrName)
	}
	if len(op.Children) > 0 {
		out = append(out, `,"children":[`...)
		for i, child := range op.Children {
			if i > 0 {
				out = append(out, ',')
			}
			out = strconv.AppendInt(out, int64(child), 10)
		}
		out = append(out, ']')
	}
	return append(out, '}')
}

// jsonSafeByte reports whether a byte below 0x80 can go into a JSON string
// unescaped.
//
// The set matches encoding/json's htmlSafeSet: printable ASCII minus the quote
// and the backslash, minus '<', '>' and '&'. encoding/json escapes those three
// by default so a patch payload cannot break out of a <script> block, and the
// output must match byte for byte.
func jsonSafeByte(b byte) bool {
	if b < 0x20 {
		return false
	}
	switch b {
	case '"', '\\', '<', '>', '&':
		return false
	}
	return true
}

// appendJSONString writes s as a quoted JSON string, matching
// encoding/json.Marshal with HTML escaping left on.
func appendJSONString(out []byte, s string) []byte {
	out = append(out, '"')
	start := 0
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			if jsonSafeByte(b) {
				i++
				continue
			}
			out = append(out, s[start:i]...)
			switch b {
			case '\\', '"':
				out = append(out, '\\', b)
			case '\b':
				out = append(out, '\\', 'b')
			case '\f':
				out = append(out, '\\', 'f')
			case '\n':
				out = append(out, '\\', 'n')
			case '\r':
				out = append(out, '\\', 'r')
			case '\t':
				out = append(out, '\\', 't')
			default:
				// Control bytes other than \b, \f, \n, \r and \t, plus
				// <, > and &.
				out = append(out, '\\', 'u', '0', '0', lowerHex[b>>4], lowerHex[b&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 becomes an escaped replacement character, as
			// encoding/json does.
			out = append(out, s[start:i]...)
			out = append(out, `\ufffd`...)
			i += size
			start = i
			continue
		}
		if r == lineSeparator || r == paragraphSeparator {
			// Line and paragraph separators are valid JSON but break a
			// JavaScript source literal, so encoding/json escapes them.
			out = append(out, s[start:i]...)
			out = append(out, '\\', 'u', '2', '0', '2', lowerHex[r&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	out = append(out, s[start:]...)
	return append(out, '"')
}
