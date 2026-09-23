package evalparity

import (
	"fmt"
	"strings"
	"unicode"
)

// identifier turns a Case.ID such as "numeric_int_div" into the PascalCase
// Go identifier "NumericIntDiv" every backend's generated names share:
// the component is Case<ID>, and (when the case has props) the props type
// is Props<ID>. Sharing one derivation keeps route/VM's per-case
// gosx.Compile and the batched transpile program's symbol names from ever
// drifting apart.
func identifier(id string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range id {
		switch {
		case r == '_' || r == '-':
			upperNext = true
		case upperNext:
			b.WriteRune(unicode.ToUpper(r))
			upperNext = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// componentName is the func identifier source and the batched transpile
// program share for c.
func componentName(c Case) string { return "Case" + identifier(c.ID) }

// propsTypeName is the Props struct identifier source and the batched
// transpile program share for c. Only meaningful when c.PropsFields != "".
func propsTypeName(c Case) string { return "Props" + identifier(c.ID) }

// gsxSource renders c into a complete, one-component .gsx file: a legacy
// (non-strict) //gosx:island function, the syntax both route/fileeval.go's
// full reflect evaluator and ir.LowerIsland's island DSL parser accept
// without the narrow strict-component scalar grammar (see
// internal/strictcomponent/expression.go) getting in the way. Marking it
// an island costs nothing on the transpile/route paths (transpile.go and
// renderFileProgramHTML's entry-render path do not special-case IsIsland;
// see the harness design note in doc.go) and is what lets the same source
// feed ir.LowerIsland for the VM backend.
func gsxSource(c Case) []byte {
	var b strings.Builder
	b.WriteString("package app\n\n")
	b.WriteString("import gosx \"m31labs.dev/gosx\"\n\n")
	if c.PropsFields != "" {
		if c.ExtraTypes != "" {
			b.WriteString(c.ExtraTypes)
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "type %s struct {\n", propsTypeName(c))
		for _, line := range strings.Split(c.PropsFields, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fmt.Fprintf(&b, "\t%s\n", line)
		}
		b.WriteString("}\n\n")
	}
	b.WriteString("//gosx:island\n")
	if c.PropsFields != "" {
		fmt.Fprintf(&b, "func %s(props %s) gosx.Node {\n", componentName(c), propsTypeName(c))
	} else {
		fmt.Fprintf(&b, "func %s() gosx.Node {\n", componentName(c))
	}
	fmt.Fprintf(&b, "\treturn <div>{%s}</div>\n", c.Expr)
	b.WriteString("}\n")
	return []byte(b.String())
}
