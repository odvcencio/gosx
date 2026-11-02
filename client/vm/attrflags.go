package vm

import (
	"strings"

	"m31labs.dev/gosx/internal/htmlattr"
	"m31labs.dev/gosx/island/program"
)

// Attribute verdicts that depend only on an attribute's name and kind. They
// were recomputed for every attribute on every render (two passes, plus a
// lowercase copy for the "on" prefix and another inside FilterURL); a program
// is immutable once loaded, so NewVM and SwapProgram compute them once.
const (
	// attrSkip marks an authored attribute that must not reach the DOM: an
	// unsafe name or an "on" handler spelling. Event attributes are never
	// skipped; they become dispatch markers.
	attrSkip uint8 = 1 << iota
	// attrURL marks a name FilterURL sanitizes (href, src and friends).
	attrURL
)

func attrFlag(attr program.Attr) uint8 {
	var f uint8
	if attr.Kind != program.AttrEvent && (!htmlattr.SafeAuthoredName(attr.Name) || strings.HasPrefix(strings.ToLower(attr.Name), "on")) {
		f |= attrSkip
	}
	if htmlattr.IsURLAttr(attr.Name) {
		f |= attrURL
	}
	return f
}

func checkAttrFlags(attrs []program.Attr) []uint8 {
	if len(attrs) == 0 {
		return []uint8{}
	}
	flags := make([]uint8, len(attrs))
	for i := range attrs {
		flags[i] = attrFlag(attrs[i])
	}
	return flags
}

// buildAttrFlags returns one verdict slice per program node, indexed like
// prog.Nodes. Nodes without attributes get nil.
func buildAttrFlags(prog *program.Program) [][]uint8 {
	if prog == nil || len(prog.Nodes) == 0 {
		return nil
	}
	out := make([][]uint8, len(prog.Nodes))
	for i := range prog.Nodes {
		if len(prog.Nodes[i].Attrs) > 0 {
			out[i] = checkAttrFlags(prog.Nodes[i].Attrs)
		}
	}
	return out
}

// attrFlagsFor returns the load-time verdicts for a node, or nil when the node
// has no cached entry (an unsourced node, or a program edited after load); the
// caller then checks on the spot, which gives the same answer.
func (vm *VM) attrFlagsFor(source, n int) []uint8 {
	if n == 0 || source < 0 || source >= len(vm.attrFlags) {
		return nil
	}
	if f := vm.attrFlags[source]; len(f) == n {
		return f
	}
	return nil
}

func filterURLFlag(flags uint8, name, value string) string {
	if flags&attrURL == 0 {
		return value
	}
	return htmlattr.FilterURL(name, value)
}
