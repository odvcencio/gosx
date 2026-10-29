package bridge

import (
	"strconv"

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
// equality against a randomized corpus. The string encoder is vm.AppendJSONString,
// which also writes signal values.

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
	out = vm.AppendJSONString(out, op.Path)
	if op.Tag != "" {
		out = append(out, `,"tag":`...)
		out = vm.AppendJSONString(out, op.Tag)
	}
	if op.Text != "" {
		out = append(out, `,"text":`...)
		out = vm.AppendJSONString(out, op.Text)
	}
	if op.AttrName != "" {
		out = append(out, `,"attrName":`...)
		out = vm.AppendJSONString(out, op.AttrName)
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
