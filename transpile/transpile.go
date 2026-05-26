// Package transpile converts GoSX source files into standard Go code.
//
// The transpiler follows a two-phase pattern (collect → emit) consistent
// with Danmuji and Ferrous Wheel:
//
//  1. Parse GoSX source using the extended grammar.
//  2. Walk the CST, emitting standard Go code. GSX expressions are
//     converted into gosx.Node-building function calls.
package transpile

import (
	"fmt"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"

	"m31labs.dev/gosx"
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Options controls transpiler behavior.
type Options struct {
	SourceFile string
	Debug      bool
}

// Transpile converts GoSX source into valid Go code that uses the gosx/node package.
func Transpile(source []byte, opts Options) (string, error) {
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return "", err
	}

	root := tree.RootNode()
	if root.HasError() {
		return "", gosx.DescribeParseError(root, source, lang)
	}

	t := &transpiler{
		src:        source,
		lang:       lang,
		sourceFile: opts.SourceFile,
		imports:    make(map[string]string),
	}

	result := t.emit(root)
	if len(t.errs) > 0 {
		return "", fmt.Errorf("transpile errors:\n%s", strings.Join(t.errs, "\n"))
	}

	return result, nil
}

type transpiler struct {
	src        []byte
	lang       *gotreesitter.Language
	sourceFile string
	imports    map[string]string // alias -> path
	propsTypes map[string]string
	errs       []string
}

func (t *transpiler) text(n *gotreesitter.Node) string {
	return string(t.src[n.StartByte():n.EndByte()])
}

func (t *transpiler) nodeType(n *gotreesitter.Node) string {
	return n.Type(t.lang)
}

func (t *transpiler) childByField(n *gotreesitter.Node, name string) *gotreesitter.Node {
	return n.ChildByFieldName(name, t.lang)
}

func (t *transpiler) errorf(n *gotreesitter.Node, format string, args ...any) {
	pos := n.StartPoint()
	msg := fmt.Sprintf("%d:%d: %s", pos.Row+1, pos.Column+1, fmt.Sprintf(format, args...))
	t.errs = append(t.errs, msg)
}

// emit dispatches on node type, returning Go source code.
func (t *transpiler) emit(n *gotreesitter.Node) string {
	switch t.nodeType(n) {
	case "source_file":
		return t.emitSourceFile(n)
	case "jsx_element":
		return t.emitGSXElement(n)
	case "jsx_self_closing_element":
		return t.emitSelfClosing(n)
	case "jsx_fragment":
		return t.emitFragment(n)
	case "jsx_expression_container":
		return t.emitExprContainer(n)
	case "jsx_text":
		return t.emitGSXText(n)
	default:
		return t.emitDefault(n)
	}
}

func (t *transpiler) emitSourceFile(n *gotreesitter.Node) string {
	var b strings.Builder
	t.collectImports(n)
	t.collectComponentProps(n)

	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		b.WriteString(t.emit(child))
		b.WriteByte('\n')
	}

	return b.String()
}

func (t *transpiler) collectImports(n *gotreesitter.Node) {
	if t.imports == nil {
		t.imports = make(map[string]string)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if t.nodeType(child) != "import_declaration" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), t.sourceFile, "package gosxtranspile\n"+t.text(child), parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			alias := ""
			if spec.Name != nil {
				alias = spec.Name.Name
				if alias == "." || alias == "_" {
					continue
				}
			} else {
				alias = path.Base(importPath)
			}
			if alias != "" {
				t.imports[alias] = importPath
			}
		}
	}
}

func (t *transpiler) collectComponentProps(n *gotreesitter.Node) {
	if t.propsTypes == nil {
		t.propsTypes = make(map[string]string)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if t.nodeType(child) != "function_declaration" {
			continue
		}
		nameNode := t.childByField(child, "name")
		if nameNode == nil {
			continue
		}
		name := t.text(nameNode)
		if !isComponent(name) {
			continue
		}
		if propsType := t.extractPropsType(child); propsType != "" {
			t.propsTypes[name] = propsType
		}
	}
}

func (t *transpiler) extractPropsType(funcDecl *gotreesitter.Node) string {
	params := t.childByField(funcDecl, "parameters")
	if params == nil {
		return ""
	}
	for i := 0; i < int(params.NamedChildCount()); i++ {
		param := params.NamedChild(i)
		if t.nodeType(param) != "parameter_declaration" {
			continue
		}
		typeNode := t.childByField(param, "type")
		if typeNode != nil {
			return strings.TrimSpace(t.text(typeNode))
		}
	}
	return ""
}

// emitDefault passes through non-GSX nodes by re-emitting their source,
// but recursively processes any GSX children within.
func (t *transpiler) emitDefault(n *gotreesitter.Node) string {
	if n.NamedChildCount() == 0 {
		return t.text(n)
	}

	var b strings.Builder
	lastEnd := n.StartByte()

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)

		// Emit any source text between the previous child and this one
		if child.StartByte() > lastEnd {
			b.Write(t.src[lastEnd:child.StartByte()])
		}

		childType := t.nodeType(child)
		if childType == "jsx_element" || childType == "jsx_self_closing_element" || childType == "jsx_fragment" {
			b.WriteString(t.emit(child))
		} else {
			b.WriteString(t.emitDefault(child))
		}

		lastEnd = child.EndByte()
	}

	// Emit trailing source after last child
	if lastEnd < n.EndByte() {
		b.Write(t.src[lastEnd:n.EndByte()])
	}

	return b.String()
}

func (t *transpiler) emitGSXElement(n *gotreesitter.Node) string {
	openNode := t.childByField(n, "open")
	if openNode == nil {
		t.errorf(n, "element missing opening tag")
		return ""
	}

	tag := t.extractTagName(openNode)
	children := t.emitChildren(n)

	if isComponent(tag) {
		if propsType, ok := t.typedPropsType(tag); ok {
			return t.emitTypedComponentCall(tag, propsType, t.emitTypedAttrsForTag(tag, openNode), children)
		}
		return t.emitComponentCall(tag, t.emitAttrs(openNode), children)
	}
	return t.emitElementCall(tag, t.emitAttrs(openNode), children)
}

func (t *transpiler) emitSelfClosing(n *gotreesitter.Node) string {
	tag := t.extractTagName(n)

	if isComponent(tag) {
		if propsType, ok := t.typedPropsType(tag); ok {
			return t.emitTypedComponentCall(tag, propsType, t.emitTypedAttrsForTag(tag, n), nil)
		}
		return t.emitComponentCall(tag, t.emitAttrs(n), nil)
	}
	return t.emitElementCall(tag, t.emitAttrs(n), nil)
}

func (t *transpiler) emitFragment(n *gotreesitter.Node) string {
	children := t.emitChildren(n)
	var b strings.Builder
	b.WriteString("gosx.Fragment(")
	for i, child := range children {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(child)
	}
	b.WriteByte(')')
	return b.String()
}

func (t *transpiler) emitExprContainer(n *gotreesitter.Node) string {
	exprNode := t.childByField(n, "expression")
	if exprNode == nil {
		return ""
	}

	// If the expression contains GSX, transpile it
	exprType := t.nodeType(exprNode)
	if exprType == "jsx_element" || exprType == "jsx_self_closing_element" || exprType == "jsx_fragment" {
		return t.emit(exprNode)
	}

	return fmt.Sprintf("gosx.Expr(%s)", t.text(exprNode))
}

func (t *transpiler) emitGSXText(n *gotreesitter.Node) string {
	text := t.text(n)
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("gosx.Text(%q)", text)
}

func (t *transpiler) emitElementCall(tag string, attrs []string, children []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gosx.El(%q", tag)

	if len(attrs) > 0 {
		b.WriteString(", gosx.Attrs(")
		b.WriteString(strings.Join(attrs, ", "))
		b.WriteByte(')')
	}

	for _, child := range children {
		if child != "" {
			b.WriteString(", ")
			b.WriteString(child)
		}
	}

	b.WriteByte(')')
	return b.String()
}

func (t *transpiler) emitComponentCall(tag string, attrs []string, children []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s(", tag)

	if len(attrs) > 0 || len(children) > 0 {
		b.WriteString("gosx.Props(")
		b.WriteString(strings.Join(attrs, ", "))
		b.WriteByte(')')
	}

	for _, child := range children {
		if child != "" {
			b.WriteString(", ")
			b.WriteString(child)
		}
	}

	b.WriteByte(')')
	return b.String()
}

func (t *transpiler) typedPropsType(tag string) (string, bool) {
	propsType := strings.TrimSpace(t.propsTypes[tag])
	if propsType == "" || isAttrListPropsType(propsType) {
		return t.gosxUIPropsType(tag)
	}
	return propsType, true
}

func (t *transpiler) gosxUIPropsType(tag string) (string, bool) {
	alias, component, ok := splitMemberTag(tag)
	if !ok || t.imports[alias] != "m31labs.dev/gosx/ui" {
		return "", false
	}
	propsType := gosxUIComponentPropsType(component)
	if propsType == "" {
		return "", false
	}
	return alias + "." + propsType, true
}

func (t *transpiler) emitTypedComponentCall(tag, propsType string, attrs []string, children []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s(%s{", tag, propsType)
	for i, attr := range attrs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(attr)
	}
	b.WriteByte('}')

	for _, child := range children {
		if child != "" {
			b.WriteString(", ")
			b.WriteString(child)
		}
	}

	b.WriteByte(')')
	return b.String()
}

func (t *transpiler) emitAttrs(n *gotreesitter.Node) []string {
	return t.emitAttrsWithMode(n, false)
}

func (t *transpiler) emitTypedAttrs(n *gotreesitter.Node) []string {
	return t.emitAttrsWithMode(n, true)
}

func (t *transpiler) emitTypedAttrsForTag(tag string, n *gotreesitter.Node) []string {
	if t.isGoSXUITag(tag) {
		return t.emitGoSXUIAttrs(tag, n)
	}
	return t.emitTypedAttrs(n)
}

func (t *transpiler) isGoSXUITag(tag string) bool {
	alias, _, ok := splitMemberTag(tag)
	return ok && t.imports[alias] == "m31labs.dev/gosx/ui"
}

func (t *transpiler) emitGoSXUIAttrs(tag string, n *gotreesitter.Node) []string {
	alias, component, _ := splitMemberTag(tag)
	var baseFields []string
	var inputFields []string
	var propFields []string
	var extraAttrs []string

	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch t.nodeType(child) {
		case "jsx_attribute":
			nameNode := t.childByField(child, "name")
			if nameNode == nil {
				continue
			}
			name := t.text(nameNode)
			value, _, ok := t.emitAttrValue(child)
			if !ok {
				continue
			}
			if field := gosxUIPropField(component, name); field != "" {
				if gosxUIUsesNestedInputProps(component) && gosxUIInputPropField(name) != "" {
					inputFields = append(inputFields, fmt.Sprintf("%s: %s", field, value))
				} else {
					propFields = append(propFields, fmt.Sprintf("%s: %s", field, value))
				}
				continue
			}
			if field := gosxUIBaseField(component, name); field != "" {
				baseFields = append(baseFields, fmt.Sprintf("%s: %s", field, value))
				continue
			}
			extraAttrs = append(extraAttrs, fmt.Sprintf("gosx.Attr(%q, %s)", name, value))
		case "jsx_spread_attribute":
			t.errorf(child, "spread attributes are not supported for typed component props")
		}
	}

	if len(extraAttrs) > 0 {
		baseFields = append(baseFields, fmt.Sprintf("Attrs: gosx.Attrs(%s)", strings.Join(extraAttrs, ", ")))
	}
	if gosxUIUsesNestedInputProps(component) {
		inputLiteral := t.emitGoSXUIInputProps(alias, baseFields, inputFields)
		if inputLiteral != "" {
			propFields = append([]string{inputLiteral}, propFields...)
		}
		return propFields
	}
	if len(baseFields) > 0 {
		propFields = append([]string{fmt.Sprintf("BaseProps: %s.BaseProps{%s}", alias, strings.Join(baseFields, ", "))}, propFields...)
	}
	return propFields
}

func (t *transpiler) emitGoSXUIInputProps(alias string, baseFields []string, inputFields []string) string {
	var fields []string
	if len(baseFields) > 0 {
		fields = append(fields, fmt.Sprintf("BaseProps: %s.BaseProps{%s}", alias, strings.Join(baseFields, ", ")))
	}
	fields = append(fields, inputFields...)
	if len(fields) == 0 {
		return ""
	}
	return fmt.Sprintf("InputProps: %s.InputProps{%s}", alias, strings.Join(fields, ", "))
}

func (t *transpiler) emitAttrsWithMode(n *gotreesitter.Node, typed bool) []string {
	var attrs []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch t.nodeType(child) {
		case "jsx_attribute":
			attr := t.emitAttrWithMode(child, typed)
			if attr != "" {
				attrs = append(attrs, attr)
			}
		case "jsx_spread_attribute":
			if typed {
				t.errorf(child, "spread attributes are not supported for typed component props")
				continue
			}
			exprNode := t.childByField(child, "expression")
			if exprNode != nil {
				attrs = append(attrs, fmt.Sprintf("gosx.Spread(%s)", t.text(exprNode)))
			}
		}
	}
	return attrs
}

func (t *transpiler) emitAttr(n *gotreesitter.Node) string {
	return t.emitAttrWithMode(n, false)
}

func (t *transpiler) emitAttrWithMode(n *gotreesitter.Node, typed bool) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	name := t.text(nameNode)

	value, boolAttr, ok := t.emitAttrValue(n)
	if !ok {
		return ""
	}
	if boolAttr {
		if typed {
			return fmt.Sprintf("%s: true", name)
		}
		return fmt.Sprintf("gosx.BoolAttr(%q)", name)
	}
	if typed {
		return fmt.Sprintf("%s: %s", name, value)
	}
	return fmt.Sprintf("gosx.Attr(%q, %s)", name, value)
}

func (t *transpiler) emitAttrValue(n *gotreesitter.Node) (string, bool, bool) {
	valueNode := t.childByField(n, "value")
	if valueNode == nil {
		return "true", true, true
	}

	switch t.nodeType(valueNode) {
	case "jsx_string_literal":
		return t.text(valueNode), false, true
	case "jsx_attribute_expression":
		return stripGSXAttributeExpressionText(t.text(valueNode)), false, true
	case "jsx_expression_container":
		exprNode := t.childByField(valueNode, "expression")
		if exprNode != nil {
			return t.text(exprNode), false, true
		}
	}

	return "", false, false
}

func (t *transpiler) emitChildren(n *gotreesitter.Node) []string {
	var children []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		typ := t.nodeType(child)
		if typ == "jsx_opening_element" || typ == "jsx_closing_element" {
			continue
		}
		if typ == "jsx_element" || typ == "jsx_self_closing_element" ||
			typ == "jsx_expression_container" || typ == "jsx_fragment" ||
			typ == "jsx_text" {
			result := t.emit(child)
			if result != "" {
				children = append(children, result)
			}
		}
	}
	return children
}

func (t *transpiler) extractTagName(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	return t.text(nameNode)
}

func isComponent(tag string) bool {
	name := componentName(tag)
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

func componentName(tag string) string {
	if idx := strings.LastIndex(tag, "."); idx >= 0 {
		return tag[idx+1:]
	}
	return tag
}

func splitMemberTag(tag string) (string, string, bool) {
	parts := strings.Split(tag, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func isAttrListPropsType(propsType string) bool {
	switch strings.TrimSpace(propsType) {
	case "gosx.AttrList", "AttrList", "gosx.Props", "components.Props":
		return true
	default:
		return false
	}
}

func gosxUIComponentPropsType(component string) string {
	switch component {
	case "Box", "Stack", "Inline", "Grid":
		return "LayoutProps"
	case "Text", "CardTitle":
		return "TextProps"
	case "Button":
		return "ButtonProps"
	case "Card":
		return "CardProps"
	case "CardHeader", "CardContent", "CardFooter":
		return "BaseProps"
	case "Badge":
		return "BadgeProps"
	case "Field":
		return "FieldProps"
	case "Input":
		return "InputProps"
	case "Textarea":
		return "TextareaProps"
	case "Select":
		return "SelectProps"
	case "Checkbox":
		return "CheckboxProps"
	case "Tabs":
		return "TabsProps"
	case "Table":
		return "TableProps"
	default:
		return ""
	}
}

func gosxUIPropField(component, attr string) string {
	name := strings.ToLower(attr)
	if gosxUIUsesNestedInputProps(component) {
		if field := gosxUIInputPropField(attr); field != "" {
			return field
		}
		switch component {
		case "Textarea":
			if name == "rows" {
				return "Rows"
			}
		case "Select":
			if name == "options" {
				return "Options"
			}
		}
		return ""
	}

	switch component {
	case "Box", "Stack", "Inline", "Grid":
		switch name {
		case "as":
			return "As"
		case "gap":
			return "Gap"
		case "align":
			return "Align"
		case "justify":
			return "Justify"
		}
	case "Text", "CardTitle":
		switch name {
		case "as":
			return "As"
		case "tone":
			return "Tone"
		case "size":
			return "Size"
		case "weight":
			return "Weight"
		}
	case "Button":
		switch name {
		case "type":
			return "Type"
		case "href":
			return "Href"
		case "variant":
			return "Variant"
		case "size":
			return "Size"
		case "disabled":
			return "Disabled"
		}
	case "Card":
		if name == "tone" {
			return "Tone"
		}
	case "Badge":
		if name == "tone" {
			return "Tone"
		}
	case "Field":
		switch name {
		case "id":
			return "ID"
		case "label":
			return "Label"
		case "help":
			return "Help"
		case "error":
			return "Error"
		case "required":
			return "Required"
		}
	case "Input":
		return gosxUIInputPropField(attr)
	case "Checkbox":
		switch name {
		case "id":
			return "ID"
		case "name":
			return "Name"
		case "value":
			return "Value"
		case "label":
			return "Label"
		case "checked":
			return "Checked"
		case "disabled":
			return "Disabled"
		}
	case "Tabs":
		switch name {
		case "active":
			return "Active"
		case "items":
			return "Items"
		}
	case "Table":
		switch name {
		case "columns":
			return "Columns"
		case "rows":
			return "Rows"
		case "empty":
			return "Empty"
		}
	}
	return ""
}

func gosxUIInputPropField(attr string) string {
	switch strings.ToLower(attr) {
	case "id":
		return "ID"
	case "name":
		return "Name"
	case "type":
		return "Type"
	case "value":
		return "Value"
	case "placeholder":
		return "Placeholder"
	case "describedby", "aria-describedby":
		return "DescribedBy"
	case "required":
		return "Required"
	case "disabled":
		return "Disabled"
	case "invalid":
		return "Invalid"
	default:
		return ""
	}
}

func gosxUIBaseField(component, attr string) string {
	name := strings.ToLower(attr)
	switch name {
	case "class":
		return "Class"
	case "id":
		if gosxUIPropField(component, attr) == "" {
			return "ID"
		}
	}
	return ""
}

func gosxUIUsesNestedInputProps(component string) bool {
	return component == "Textarea" || component == "Select"
}

func stripGSXAttributeExpressionText(text string) string {
	if len(text) >= 2 && text[0] == '{' && text[len(text)-1] == '}' {
		return text[1 : len(text)-1]
	}
	return text
}
