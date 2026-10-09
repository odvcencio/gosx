//go:build !tinygo && !js

package ir

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"maps"
	"path"
	"strconv"

	"m31labs.dev/gosx/internal/gsxparse"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Go regions and expression payloads are copied from source byte ranges.
// Scaffold tokens introduce node placeholders and typed prop assignments;
// they never print or normalize an authored expression.
func aotProjectSource(p *Program, source []byte, lang *gotreesitter.Language) (aotCheckingFile, error) {
	tree, err := gsxparse.Parse(lang, source)
	if err != nil {
		return aotCheckingFile{}, err
	}
	defer tree.Release()
	l := &lowerer{src: source, srcStr: string(source), lang: lang}
	result := aotCheckingFile{regions: map[Span]aotCheckRegion{}, components: map[string]aotCheckRegion{}, implicit: map[int]string{}, synthetic: map[string]aotCheckRegion{}}
	var out bytes.Buffer
	text := func(n *gotreesitter.Node) string {
		if n == nil {
			return ""
		}
		return l.text(n)
	}
	copySpan := func(start, end int) {
		result.copies = append(result.copies, aotCheckCopy{start, end, out.Len()})
		out.Write(source[start:end])
	}
	expression := func(n *gotreesitter.Node) error { return aotEmitExpression(l, n, &out, copySpan) }
	activeStates := map[string]bool{}
	expr := func(n *gotreesitter.Node, span Span) error {
		start := out.Len()
		if err := expression(n); err != nil {
			return err
		}
		if l.nodeType(n) == "identifier" && activeStates[l.text(n)] {
			out.WriteString(".Get()")
		}
		result.regions[span] = aotCheckRegion{start, out.Len()}
		return nil
	}
	propsTypes := map[string]*gotreesitter.Node{}
	root := tree.RootNode()
	for i := 0; i < int(root.NamedChildCount()); i++ {
		declaration := root.NamedChild(i)
		params := l.childByField(declaration, "parameters")
		if params == nil {
			continue
		}
		for j := 0; j < int(params.NamedChildCount()); j++ {
			param := params.NamedChild(j)
			if l.childByField(param, "name") != nil {
				propsTypes[text(l.childByField(declaration, "name"))] = l.childByField(param, "type")
				break
			}
		}
	}
	var jsx func(*gotreesitter.Node) error
	jsx = func(n *gotreesitter.Node) error {
		kind := l.nodeType(n)
		if kind == "jsx_text" || kind == "jsx_raw_text" {
			return nil
		}
		if kind == "jsx_expression_container" {
			out.WriteString("{ _ = (")
			if err := expr(l.childByField(n, "expression"), l.span(n)); err != nil {
				return err
			}
			out.WriteString("); }\n")
			return nil
		}
		if kind != "jsx_element" && kind != "jsx_self_closing_element" && kind != "jsx_fragment" && kind != "jsx_raw_text_element" {
			return fmt.Errorf("evidence_shape_mismatch: unsupported JSX region %s", kind)
		}
		out.WriteString("{\n")
		open := n
		if kind == "jsx_element" || kind == "jsx_raw_text_element" {
			open = l.childByField(n, "open")
		}
		tag := l.extractTagName(open)
		props := propsTypes[tag]
		var attributes []*gotreesitter.Node
		if open != nil {
			for i := 0; i < int(open.NamedChildCount()); i++ {
				attr := open.NamedChild(i)
				if l.nodeType(attr) == "jsx_attribute" || l.nodeType(attr) == "jsx_spread_attribute" {
					attributes = append(attributes, attr)
				}
			}
		}
		if props != nil {
			for _, attr := range attributes {
				if l.nodeType(attr) != "jsx_spread_attribute" {
					continue
				}
				out.WriteString("var _ ")
				copySpan(int(props.StartByte()), int(props.EndByte()))
				out.WriteString(" = (")
				if err := expr(l.childByField(attr, "expression"), l.span(attr)); err != nil {
					return err
				}
				out.WriteString(");\n")
			}
			out.WriteString("_ = ")
			copySpan(int(props.StartByte()), int(props.EndByte()))
			out.WriteString("{\n")
		}
		for _, attr := range attributes {
			if props != nil && l.nodeType(attr) == "jsx_spread_attribute" {
				continue
			}
			name := text(l.childByField(attr, "name"))
			value := l.childByField(attr, "value")
			if name == "slot" && value != nil && l.nodeType(value) == "jsx_string_literal" {
				continue
			}
			if _, inline := legacyInlineEventType(name); inline && props == nil && value != nil && l.nodeType(value) == "jsx_string_literal" {
				code, err := strconv.Unquote(text(value))
				if err != nil {
					return fmt.Errorf("evidence_shape_mismatch: inline handler: %w", err)
				}
				out.WriteString("_ = ")
				fnStart := out.Len()
				out.WriteString("func(){" + code + "}")
				result.regions[l.span(attr)] = aotCheckRegion{fnStart, out.Len()}
				out.WriteString(";\n")
				continue
			}
			if props != nil {
				out.WriteString(name + ": ")
			} else {
				out.WriteString("_ = (")
			}
			if value == nil && l.nodeType(attr) == "jsx_spread_attribute" {
				value = l.childByField(attr, "expression")
			} else if value != nil && (l.nodeType(value) == "jsx_attribute_expression" || l.nodeType(value) == "jsx_expression_container") {
				inner := l.childByField(value, "expression")
				if inner != nil {
					value = inner
				} else {
					start, end := int(value.StartByte())+1, int(value.EndByte())-1
					begin := out.Len()
					if err := aotEmitOpaqueExpression(lang, source[start:end], &out, func(a, b int) { copySpan(start+a, start+b) }); err != nil {
						return err
					}
					if native, err := parser.ParseExpr(string(source[start:end])); err == nil {
						if ident, ok := native.(*ast.Ident); ok && activeStates[ident.Name] {
							out.WriteString(".Get()")
						}
					}
					result.regions[l.span(attr)] = aotCheckRegion{begin, out.Len()}
					if props != nil {
						out.WriteString(",\n")
					} else {
						out.WriteString(");\n")
					}
					continue
				}
			}
			if value != nil {
				if err := expr(value, l.span(attr)); err != nil {
					return err
				}
			} else {
				begin := out.Len()
				out.WriteString("true")
				result.regions[l.span(attr)] = aotCheckRegion{begin, out.Len()}
			}
			if props != nil {
				out.WriteString(",\n")
			} else {
				out.WriteString(");\n")
			}
		}
		if props != nil {
			out.WriteString("}\n")
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			typ := l.nodeType(child)
			if (typ == "jsx_element" || typ == "jsx_self_closing_element" || typ == "jsx_fragment" || typ == "jsx_raw_text_element" || typ == "jsx_expression_container" || typ == "jsx_text" || typ == "jsx_raw_text") && child != open {
				if err := jsx(child); err != nil {
					return err
				}
			}
		}
		out.WriteString("}\n")
		return nil
	}
	var goRegion func(*gotreesitter.Node) error
	goRegion = func(n *gotreesitter.Node) error {
		typ := l.nodeType(n)
		if typ == "gsx_ternary_expression" {
			return expression(n)
		}
		if typ == "jsx_element" || typ == "jsx_self_closing_element" || typ == "jsx_fragment" || typ == "jsx_raw_text_element" {
			out.WriteString("func() Node {\n")
			if err := jsx(n); err != nil {
				return err
			}
			out.WriteString("return Node{} }()")
			return nil
		}
		start := int(n.StartByte())
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			copySpan(start, int(child.StartByte()))
			if err := goRegion(child); err != nil {
				return err
			}
			start = int(child.EndByte())
		}
		copySpan(start, int(n.EndByte()))
		return nil
	}
	start := 0
	for i := 0; i < int(root.NamedChildCount()); i++ {
		n := root.NamedChild(i)
		copySpan(start, int(n.StartByte()))
		typ := l.nodeType(n)
		name := text(l.childByField(n, "name"))
		clear(activeStates)
		for _, comp := range p.Components {
			if comp.Name == name && comp.Scope != nil {
				for _, sig := range comp.Scope.Signals {
					activeStates[sig.Local] = true
				}
				for _, computed := range comp.Scope.Computeds {
					activeStates[computed.Name] = true
				}
			}
		}
		begin := out.Len()
		if typ == "gosx_component_declaration" {
			out.WriteString("func " + name + "(")
			params := l.childByField(n, "parameters")
			if params != nil {
				for j := 0; j < int(params.NamedChildCount()); j++ {
					param := params.NamedChild(j)
					pn, pt := l.childByField(param, "name"), l.childByField(param, "type")
					if pn == nil || pt == nil {
						return result, fmt.Errorf("evidence_shape_mismatch: component parameter")
					}
					if j > 0 {
						out.WriteString(",")
					}
					copySpan(int(pn.StartByte()), int(pn.EndByte()))
					out.WriteByte(' ')
					copySpan(int(pt.StartByte()), int(pt.EndByte()))
				}
			}
			out.WriteString(") Node {")
			body := l.childByField(n, "body")
			if body == nil {
				return result, fmt.Errorf("evidence_shape_mismatch: component body")
			}
			bodyStart := int(body.StartByte()) + 1
			for j := 0; j < int(body.NamedChildCount()); j++ {
				child := body.NamedChild(j)
				copySpan(bodyStart, int(child.StartByte()))
				if err := goRegion(child); err != nil {
					return result, err
				}
				bodyStart = int(child.EndByte())
			}
			copySpan(bodyStart, int(body.EndByte())-1)
			out.WriteString("}\n")
		} else {
			if err := goRegion(n); err != nil {
				return result, err
			}
		}
		if typ == "gosx_component_declaration" || typ == "function_declaration" {
			result.components[name] = aotCheckRegion{begin, out.Len()}
		}
		start = int(n.EndByte())
	}
	copySpan(start, len(source))
	result.bytes = append([]byte(nil), out.Bytes()...)
	return result, nil
}

// Finish only after collecting authored declarations from every selected
// package file. No synthetic binding may hide one of those declarations.
func aotFinishProjection(p *Program, result aotCheckingFile, packageNames map[string]bool) (aotCheckingFile, error) {
	scopeNames := maps.Clone(packageNames)
	for _, imp := range p.Imports {
		name := imp.Alias
		if name == "" {
			name = path.Base(imp.Path)
		}
		if name != "_" && name != "." {
			scopeNames[name] = true
		}
	}
	var out bytes.Buffer
	out.Write(result.bytes)
	// These are the compiler's implicit Node and signal bindings. Explicit
	// declarations/imports retain their original scope and are never replaced.
	if !scopeNames["Node"] {
		begin := out.Len()
		out.WriteString("\ntype Node = struct{}\n")
		result.synthetic["Node"] = aotCheckRegion{begin, out.Len()}
	}
	if !scopeNames[aotConditionalHelper] && bytes.Contains(out.Bytes(), []byte(aotConditionalHelper+"(")) {
		begin := out.Len()
		out.WriteString("\nfunc " + aotConditionalHelper + "[T any](c bool,a,b T) T {if c {return a};return b}\n")
		result.synthetic[aotConditionalHelper] = aotCheckRegion{begin, out.Len()}
	}
	signalImported := false
	for _, imp := range p.Imports {
		if imp.Alias == "signal" || imp.Alias == "" && path.Base(imp.Path) == "signal" {
			signalImported = true
		}
	}
	if !signalImported && !scopeNames["signal"] && bytes.Contains(p.aotBindings.source, []byte("signal.")) {
		data := append([]byte(nil), out.Bytes()...)
		line := bytes.IndexByte(data, '\n') + 1
		insertion := []byte("import signal " + strconv.Quote(signalImportPath) + "\n")
		out.Reset()
		out.Write(data[:line])
		out.Write(insertion)
		out.Write(data[line:])
		for k, r := range result.regions {
			if r.start >= line {
				r.start += len(insertion)
				r.end += len(insertion)
				result.regions[k] = r
			}
		}
		for k, r := range result.synthetic {
			if r.start >= line {
				r.start += len(insertion)
				r.end += len(insertion)
				result.synthetic[k] = r
			}
		}
		for k, r := range result.components {
			if r.start >= line {
				r.start += len(insertion)
				r.end += len(insertion)
				result.components[k] = r
			}
		}
		for i := range result.copies {
			if result.copies[i].checkStart >= line {
				result.copies[i].checkStart += len(insertion)
			}
		}
	}
	result.bytes = append([]byte(nil), out.Bytes()...)
	return aotScaffoldLocals(p, result, scopeNames)
}

func aotEmitExpression(l *lowerer, n *gotreesitter.Node, out *bytes.Buffer, copySpan func(int, int)) error {
	if n == nil {
		return fmt.Errorf("evidence_shape_mismatch: missing source expression")
	}
	if l.nodeType(n) == "gsx_ternary_expression" {
		out.WriteString(aotConditionalHelper + "(")
		for i, field := range []string{"condition", "consequence", "alternative"} {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := aotEmitExpression(l, l.childByField(n, field), out, copySpan); err != nil {
				return err
			}
		}
		out.WriteByte(')')
		return nil
	}
	start := int(n.StartByte())
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		copySpan(start, int(child.StartByte()))
		if err := aotEmitExpression(l, child, out, copySpan); err != nil {
			return err
		}
		start = int(child.EndByte())
	}
	copySpan(start, int(n.EndByte()))
	return nil
}

// Attribute expressions are opaque external tokens in the GoSX grammar.
// Reparse their exact payload with the same grammar, never an admission lexer.
func aotEmitOpaqueExpression(lang *gotreesitter.Language, source []byte, out *bytes.Buffer, copySpan func(int, int)) error {
	prefix := []byte("package example\nfunc expression(){_ = (")
	wrapped := append(append(append([]byte(nil), prefix...), source...), []byte(") }\n")...)
	tree, err := gsxparse.Parse(lang, wrapped)
	if err != nil {
		return err
	}
	defer tree.Release()
	l := &lowerer{src: wrapped, srcStr: string(wrapped), lang: lang}
	lo := len(prefix)
	hi := lo + len(source)
	var expr *gotreesitter.Node
	var find func(*gotreesitter.Node)
	find = func(n *gotreesitter.Node) {
		if int(n.StartByte()) >= lo && int(n.EndByte()) <= hi {
			expr = n
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if expr == nil {
				find(n.NamedChild(i))
			}
		}
	}
	find(tree.RootNode())
	if expr == nil || tree.RootNode().HasError() {
		return fmt.Errorf("evidence_shape_mismatch: opaque expression does not parse")
	}
	copySpan(0, int(expr.StartByte())-lo)
	if err := aotEmitExpression(l, expr, out, func(a, b int) { copySpan(a-lo, b-lo) }); err != nil {
		return err
	}
	copySpan(int(expr.EndByte())-lo, len(source))
	return nil
}
