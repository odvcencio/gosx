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
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx"
)

// Options controls transpiler behavior.
type Options struct {
	SourceFile string
	Debug      bool

	// strictProjection is used by the package checker to emit only declarations
	// that the strict renderer can execute. It intentionally remains internal:
	// ordinary callers should transpile the complete source file.
	strictProjection bool
	importNames      map[string]string
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
	// Transpilation must apply the same semantic gate as the IR renderer. In
	// particular, strict components may not contain Go statements that would
	// survive in generated Go but be ignored by file-routed IR rendering.
	if _, err := gosx.Compile(source); err != nil {
		return "", err
	}

	t := &transpiler{
		src:              source,
		lang:             lang,
		sourceFile:       opts.SourceFile,
		imports:          make(map[string]string),
		strictProjection: opts.strictProjection,
		importNames:      opts.importNames,
	}

	result := t.emit(root)
	if len(t.errs) > 0 {
		return "", fmt.Errorf("transpile errors:\n%s", strings.Join(t.errs, "\n"))
	}

	// Last line of defense: the transpiler must never hand back Go that does
	// not parse. A malformed GSX construct can walk to a CST that
	// root.HasError() does not flag while still emitting wreckage — an
	// unterminated <script>, for example, used to emit a few bare tokens and
	// silently drop every declaration after it. Failing here converts that
	// class of silent corruption into a build error.
	if _, err := parser.ParseFile(token.NewFileSet(), opts.SourceFile, result, parser.SkipObjectResolution); err != nil {
		return "", fmt.Errorf("transpile produced invalid Go (%w); this is a GoSX bug or malformed GSX in %s", err, opts.SourceFile)
	}

	return result, nil
}

type transpiler struct {
	src              []byte
	lang             *gotreesitter.Language
	sourceFile       string
	imports          map[string]string // alias -> path
	propsTypes       map[string]string
	propsFields      map[string]map[string]string
	errs             []string
	hasStrict        bool
	strict           int
	gosxAlias        string
	injectGosx       bool
	strictProjection bool
	importNames      map[string]string
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
	case "gosx_component_declaration":
		return t.emitStrictComponent(n)
	case "jsx_element":
		return t.emitGSXElement(n)
	case "jsx_raw_text_element":
		return t.emitRawTextElement(n)
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
	t.collectImports(n)
	t.collectStructFields(n)
	t.collectComponentProps(n)
	t.resolveGoSXQualifier()
	if t.strictProjection {
		return t.emitStrictSourceFile(n)
	}

	var b strings.Builder

	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		b.WriteString(t.emit(child))
		b.WriteByte('\n')
		if t.nodeType(child) == "package_clause" && t.hasStrict && t.injectGosx {
			fmt.Fprintf(&b, "import %s %q\n", t.gosxAlias, "m31labs.dev/gosx")
		}
	}

	return b.String()
}

// emitStrictSourceFile projects a mixed GoSX file into the subset that the
// strict renderer and package checker share: package, imports used by retained
// declarations, types, and strict components. Legacy funcs and top-level DSL
// values are omitted even if they contain identifiers that ordinary Go cannot
// resolve (route/data/request and application helpers are interpreted later by
// the legacy runtime).
func (t *transpiler) emitStrictSourceFile(n *gotreesitter.Node) string {
	var packageClause string
	var declarations []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch t.nodeType(child) {
		case "package_clause":
			packageClause = t.text(child)
		case "type_declaration":
			declaration := t.emitDefault(child)
			if t.sourceFile != "" {
				declaration = fmt.Sprintf("//line %s:%d\n%s", filepathForLineDirective(t.sourceFile), child.StartPoint().Row+1, declaration)
			}
			declarations = append(declarations, declaration)
		case "gosx_component_declaration":
			declaration := t.emitStrictComponent(child)
			if t.sourceFile != "" {
				declaration = fmt.Sprintf("//line %s:%d\n%s", filepathForLineDirective(t.sourceFile), child.StartPoint().Row+1, declaration)
			}
			declarations = append(declarations, declaration)
		}
	}

	body := strings.Join(declarations, "\n\n")
	imports := t.strictProjectionImports(n, body)
	if t.injectGosx {
		imports = append(imports, projectionImport{alias: t.gosxAlias, path: "m31labs.dev/gosx"})
	}
	sortProjectionImports(imports)

	var b strings.Builder
	b.WriteString(packageClause)
	b.WriteByte('\n')
	if len(imports) > 0 {
		b.WriteString("import (\n")
		for _, spec := range imports {
			b.WriteByte('\t')
			if spec.alias != "" {
				b.WriteString(spec.alias)
				b.WriteByte(' ')
			}
			b.WriteString(strconv.Quote(spec.path))
			b.WriteByte('\n')
		}
		b.WriteString(")\n")
	}
	if body != "" {
		b.WriteByte('\n')
		b.WriteString(body)
		b.WriteByte('\n')
	}
	return b.String()
}

type projectionImport struct {
	alias string
	path  string
}

func (t *transpiler) strictProjectionImports(n *gotreesitter.Node, body string) []projectionImport {
	selectorAliases, unresolvedNames := projectionIdentifiers(body)
	var imports []projectionImport
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if t.nodeType(child) != "import_declaration" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), t.sourceFile, "package gosxprojection\n"+t.text(child), parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			alias := t.defaultImportName(importPath)
			explicit := false
			if spec.Name != nil {
				alias = spec.Name.Name
				explicit = true
			}
			switch alias {
			case "_":
				// Side-effect imports cannot affect structural type checking.
				continue
			case ".":
				// Dot imports are file-scoped. Retain gosx when generated code uses
				// its unqualified API, and otherwise only when the retained syntax
				// has an unresolved exported identifier that could come from it.
				if !(importPath == "m31labs.dev/gosx" && t.gosxAlias == ".") && len(unresolvedNames) == 0 {
					continue
				}
			default:
				if _, used := selectorAliases[alias]; !used {
					continue
				}
			}
			emitAlias := ""
			if explicit || alias == "." {
				emitAlias = alias
			}
			imports = append(imports, projectionImport{alias: emitAlias, path: importPath})
		}
	}
	return imports
}

func (t *transpiler) defaultImportName(importPath string) string {
	if name := strings.TrimSpace(t.importNames[importPath]); name != "" {
		return name
	}
	return path.Base(importPath)
}

// projectionIdentifiers returns package aliases used as selector roots and
// exported identifiers that remain unresolved inside the projected file. The
// latter lets us discard legacy-only dot imports without discarding a dot
// import that supplies a strict prop type or expression.
func projectionIdentifiers(body string) (map[string]struct{}, map[string]struct{}) {
	selectors := make(map[string]struct{})
	unresolved := make(map[string]struct{})
	file, err := parser.ParseFile(token.NewFileSet(), "projection.go", "package gosxprojection\n"+body, 0)
	if err != nil {
		return selectors, unresolved
	}
	declared := make(map[string]struct{})
	selectorNames := make(map[*ast.Ident]struct{})
	fieldKeys := make(map[*ast.Ident]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.TypeSpec:
			declared[node.Name.Name] = struct{}{}
		case *ast.FuncDecl:
			declared[node.Name.Name] = struct{}{}
		case *ast.Field:
			for _, name := range node.Names {
				declared[name.Name] = struct{}{}
			}
		case *ast.AssignStmt:
			if node.Tok == token.DEFINE {
				for _, lhs := range node.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok {
						declared[ident.Name] = struct{}{}
					}
				}
			}
		case *ast.ValueSpec:
			for _, name := range node.Names {
				declared[name.Name] = struct{}{}
			}
		case *ast.SelectorExpr:
			selectorNames[node.Sel] = struct{}{}
			if ident, ok := node.X.(*ast.Ident); ok {
				selectors[ident.Name] = struct{}{}
			}
		case *ast.KeyValueExpr:
			if ident, ok := node.Key.(*ast.Ident); ok {
				fieldKeys[ident] = struct{}{}
			}
		}
		return true
	})
	predeclared := map[string]struct{}{
		"any": {}, "bool": {}, "byte": {}, "comparable": {}, "complex64": {}, "complex128": {},
		"error": {}, "false": {}, "float32": {}, "float64": {}, "int": {}, "int8": {}, "int16": {},
		"int32": {}, "int64": {}, "iota": {}, "nil": {}, "rune": {}, "string": {}, "true": {},
		"uint": {}, "uint8": {}, "uint16": {}, "uint32": {}, "uint64": {}, "uintptr": {},
		"append": {}, "cap": {}, "clear": {}, "close": {}, "complex": {}, "copy": {}, "delete": {},
		"imag": {}, "len": {}, "make": {}, "max": {}, "min": {}, "new": {}, "panic": {}, "print": {},
		"println": {}, "real": {}, "recover": {},
	}
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Obj != nil || ident.Name == "_" {
			return true
		}
		if _, ok := selectorNames[ident]; ok {
			return true
		}
		if _, ok := fieldKeys[ident]; ok {
			return true
		}
		if _, ok := declared[ident.Name]; ok {
			return true
		}
		if _, ok := predeclared[ident.Name]; ok {
			return true
		}
		if ident.Name != "" && unicode.IsUpper([]rune(ident.Name)[0]) {
			unresolved[ident.Name] = struct{}{}
		}
		return true
	})
	return selectors, unresolved
}

func sortProjectionImports(imports []projectionImport) {
	sort.SliceStable(imports, func(i, j int) bool {
		if imports[i].path == imports[j].path {
			return imports[i].alias < imports[j].alias
		}
		return imports[i].path < imports[j].path
	})
}

func filepathForLineDirective(name string) string {
	if abs, err := filepath.Abs(name); err == nil {
		name = abs
	}
	return filepath.ToSlash(name)
}

func (t *transpiler) collectStructFields(n *gotreesitter.Node) {
	if t.propsFields == nil {
		t.propsFields = make(map[string]map[string]string)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		decl := n.NamedChild(i)
		if t.nodeType(decl) != "type_declaration" {
			continue
		}
		t.collectStructFieldsFromTypeDecl(decl)
	}
}

func (t *transpiler) collectStructFieldsFromTypeDecl(n *gotreesitter.Node) {
	var specs []*gotreesitter.Node
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		if t.nodeType(node) == "type_spec" {
			specs = append(specs, node)
			return
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(n)
	for _, spec := range specs {
		nameNode := t.childByField(spec, "name")
		typeNode := t.childByField(spec, "type")
		if nameNode == nil || typeNode == nil || t.nodeType(typeNode) != "struct_type" {
			continue
		}
		fields := make(map[string]string)
		var collect func(*gotreesitter.Node)
		collect = func(node *gotreesitter.Node) {
			if node == nil {
				return
			}
			if t.nodeType(node) == "field_declaration" {
				for i := 0; i < int(node.NamedChildCount()); i++ {
					child := node.NamedChild(i)
					if t.nodeType(child) != "field_identifier" {
						continue
					}
					field := t.text(child)
					first, _ := utf8.DecodeRuneInString(field)
					if !unicode.IsUpper(first) {
						continue
					}
					fields[field] = field
					alias := lowerCamelField(field)
					if existing, ok := fields[alias]; ok && existing != field {
						fields[alias] = ""
					} else if !ok {
						fields[alias] = field
					}
				}
				return
			}
			for i := 0; i < int(node.NamedChildCount()); i++ {
				collect(node.NamedChild(i))
			}
		}
		collect(typeNode)
		if len(fields) > 0 {
			t.propsFields[t.text(nameNode)] = fields
		}
	}
}

func lowerCamelField(value string) string {
	runes := []rune(value)
	if len(runes) == 0 || !unicode.IsUpper(runes[0]) {
		return value
	}
	// Lower the leading acronym, stopping before the final uppercase rune when
	// it begins the following word: HTMLFor -> htmlFor, URLValue -> urlValue.
	end := 1
	for end < len(runes) && unicode.IsUpper(runes[end]) {
		if end+1 < len(runes) && unicode.IsLower(runes[end+1]) {
			break
		}
		end++
	}
	for i := 0; i < end; i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
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
			} else {
				alias = t.defaultImportName(importPath)
			}
			if alias != "" {
				t.imports[alias] = importPath
			}
		}
	}
}

func (t *transpiler) resolveGoSXQualifier() {
	if !t.hasStrict {
		// Preserve legacy output byte-for-byte. Legacy source commonly imports
		// gosx with a dot while the historical transpiler intentionally emits the
		// qualified spelling for a later build step to own.
		t.gosxAlias = "gosx"
		return
	}
	for alias, importPath := range t.imports {
		if importPath != "m31labs.dev/gosx" || alias == "_" {
			continue
		}
		t.gosxAlias = alias
		return
	}

	alias := "gosx"
	if _, exists := t.imports[alias]; exists {
		for n := 1; ; n++ {
			candidate := "gosxgen" + strconv.Itoa(n)
			if _, exists := t.imports[candidate]; !exists {
				alias = candidate
				break
			}
		}
	}
	t.gosxAlias = alias
	t.injectGosx = t.hasStrict
}

func (t *transpiler) gosxRef(name string) string {
	if t.gosxAlias == "." {
		return name
	}
	alias := t.gosxAlias
	if alias == "" {
		alias = "gosx"
	}
	return alias + "." + name
}

func (t *transpiler) collectComponentProps(n *gotreesitter.Node) {
	if t.propsTypes == nil {
		t.propsTypes = make(map[string]string)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		typ := t.nodeType(child)
		if typ != "function_declaration" && typ != "gosx_component_declaration" {
			continue
		}
		if typ == "gosx_component_declaration" {
			t.hasStrict = true
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

func (t *transpiler) emitStrictComponent(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	bodyNode := t.childByField(n, "body")
	if nameNode == nil || bodyNode == nil {
		t.errorf(n, "strict component declaration is incomplete")
		return ""
	}

	params := ""
	if paramsNode := t.childByField(n, "parameters"); paramsNode != nil {
		for i := 0; i < int(paramsNode.NamedChildCount()); i++ {
			param := paramsNode.NamedChild(i)
			if t.nodeType(param) != "gosx_component_parameter" {
				continue
			}
			paramName := t.childByField(param, "name")
			paramType := t.childByField(param, "type")
			if paramName != nil && paramType != nil {
				params = t.text(paramName) + " " + t.text(paramType)
			}
		}
	}

	t.strict++
	body := t.emitDefault(bodyNode)
	t.strict--
	return "func " + t.text(nameNode) + "(" + params + ") " + t.gosxRef("Node") + " " + body
}

func (t *transpiler) extractPropsType(funcDecl *gotreesitter.Node) string {
	params := t.childByField(funcDecl, "parameters")
	if params == nil {
		return ""
	}
	for i := 0; i < int(params.NamedChildCount()); i++ {
		param := params.NamedChild(i)
		typ := t.nodeType(param)
		if typ != "parameter_declaration" && typ != "gosx_component_parameter" {
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
	return t.emitElementCall(tag, t.emitHTMLAttrs(openNode), children)
}

// emitRawTextElement emits <script>/<style>. Their bodies are script or
// stylesheet source, so the content is passed through as raw HTML rather than
// escaped text: escaping would corrupt operators like `&&` and `<`.
func (t *transpiler) emitRawTextElement(n *gotreesitter.Node) string {
	openNode := t.childByField(n, "open")
	if openNode == nil {
		t.errorf(n, "raw text element missing opening tag")
		return ""
	}

	tag := rawTextTagName(t.text(t.childByField(openNode, "name")))
	if tag == "" {
		t.errorf(n, "raw text element has an unrecognized tag")
		return ""
	}

	var children []string
	if body := t.childByField(n, "children"); body != nil {
		switch t.nodeType(body) {
		case "jsx_expression_container":
			// <script>{ClientScript()}</script> — the Go value supplies the
			// element's content, exactly as it did before raw-text elements
			// existed. Keep the ordinary expression lowering.
			children = append(children, t.emit(body))
		default:
			if raw := rawTextBody(t.text(body)); raw != "" {
				children = append(children, t.gosxRef("RawHTML")+"("+strconv.Quote(raw)+")")
			}
		}
	}
	return t.emitElementCall(tag, t.emitHTMLAttrs(openNode), children)
}

// rawTextTagName turns the combined start-tag token (`<script`) into the tag
// name. The grammar lexes `<` and the name together; see jsx_raw_text_start_tag.
func rawTextTagName(startTag string) string {
	name := strings.TrimPrefix(startTag, "<")
	switch name {
	case "script", "style":
		return name
	default:
		return ""
	}
}

// rawTextBody strips the closing tag from a jsx_raw_text token. The external
// scanner includes `</script>` in the token so the grammar needs no separate
// closing rule, so it is removed here.
func rawTextBody(raw string) string {
	idx := strings.LastIndex(raw, "</")
	if idx < 0 {
		return raw
	}
	return raw[:idx]
}

func (t *transpiler) emitSelfClosing(n *gotreesitter.Node) string {
	tag := t.extractTagName(n)

	if isComponent(tag) {
		if propsType, ok := t.typedPropsType(tag); ok {
			return t.emitTypedComponentCall(tag, propsType, t.emitTypedAttrsForTag(tag, n), nil)
		}
		return t.emitComponentCall(tag, t.emitAttrs(n), nil)
	}
	return t.emitElementCall(tag, t.emitHTMLAttrs(n), nil)
}

func (t *transpiler) emitFragment(n *gotreesitter.Node) string {
	children := t.emitChildren(n)
	var b strings.Builder
	b.WriteString(t.gosxRef("Fragment") + "(")
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

	return fmt.Sprintf("%s(%s)", t.gosxRef("Expr"), t.text(exprNode))
}

func (t *transpiler) emitGSXText(n *gotreesitter.Node) string {
	text := t.text(n)
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("%s(%q)", t.gosxRef("Text"), text)
}

func (t *transpiler) emitElementCall(tag string, attrs []string, children []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s(%q", t.gosxRef("El"), tag)

	if len(attrs) > 0 {
		b.WriteString(", " + t.gosxRef("Attrs") + "(")
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
		b.WriteString(t.gosxRef("Props") + "(")
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
	literalType, pointer := typedPropsLiteralType(propsType)
	b.WriteString(tag)
	b.WriteByte('(')
	if pointer {
		b.WriteByte('&')
	}
	b.WriteString(literalType)
	b.WriteByte('{')
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

func typedPropsLiteralType(propsType string) (string, bool) {
	propsType = strings.TrimSpace(propsType)
	if strings.HasPrefix(propsType, "*") {
		return strings.TrimSpace(strings.TrimPrefix(propsType, "*")), true
	}
	return propsType, false
}

func (t *transpiler) emitAttrs(n *gotreesitter.Node) []string {
	return t.emitAttrsWithMode(n, false, false)
}

func (t *transpiler) emitTypedAttrs(n *gotreesitter.Node) []string {
	return t.emitAttrsWithMode(n, true, false)
}

func (t *transpiler) emitHTMLAttrs(n *gotreesitter.Node) []string {
	return t.emitAttrsWithMode(n, false, true)
}

func (t *transpiler) emitTypedAttrsForTag(tag string, n *gotreesitter.Node) []string {
	if t.isGoSXUITag(tag) {
		return t.emitGoSXUIAttrs(tag, n)
	}
	return t.emitTypedAttrsForType(t.propsTypes[tag], n)
}

func (t *transpiler) emitTypedAttrsForType(propsType string, n *gotreesitter.Node) []string {
	baseType, _ := typedPropsLiteralType(propsType)
	if idx := strings.LastIndex(baseType, "."); idx >= 0 {
		baseType = baseType[idx+1:]
	}
	if idx := strings.Index(baseType, "["); idx >= 0 {
		baseType = baseType[:idx]
	}
	aliases := t.propsFields[baseType]
	var attrs []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch t.nodeType(child) {
		case "jsx_spread_attribute":
			t.errorf(child, "spread attributes are not supported for typed component props")
		case "jsx_attribute":
			nameNode := t.childByField(child, "name")
			if nameNode == nil {
				continue
			}
			name := t.text(nameNode)
			field := name
			if resolved, known := aliases[name]; known && resolved == "" {
				t.errorf(child, "typed prop %q is ambiguous; use the exact exported Go field spelling", name)
				continue
			} else if resolved != "" {
				field = resolved
			} else if isLowerASCII(name) {
				field = upperFirst(name)
			}
			value, boolAttr, ok := t.emitAttrValue(child)
			if !ok {
				continue
			}
			if boolAttr {
				value = "true"
			}
			attrs = append(attrs, field+": "+value)
		}
	}
	return attrs
}

func isLowerASCII(value string) bool {
	return value != "" && value[0] >= 'a' && value[0] <= 'z'
}

func upperFirst(value string) string {
	if !isLowerASCII(value) {
		return value
	}
	return string(value[0]-'a'+'A') + value[1:]
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
			extraAttrs = append(extraAttrs, fmt.Sprintf("%s(%q, %s)", t.gosxRef("Attr"), name, value))
		case "jsx_spread_attribute":
			t.errorf(child, "spread attributes are not supported for typed component props")
		}
	}

	if len(extraAttrs) > 0 {
		baseFields = append(baseFields, fmt.Sprintf("Attrs: %s(%s)", t.gosxRef("Attrs"), strings.Join(extraAttrs, ", ")))
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

func (t *transpiler) emitAttrsWithMode(n *gotreesitter.Node, typed, htmlElement bool) []string {
	var attrs []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch t.nodeType(child) {
		case "jsx_attribute":
			attr := t.emitAttrWithMode(child, typed, htmlElement)
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
				attrs = append(attrs, fmt.Sprintf("%s(%s)", t.gosxRef("Spread"), t.text(exprNode)))
			}
		}
	}
	return attrs
}

func (t *transpiler) emitAttr(n *gotreesitter.Node) string {
	return t.emitAttrWithMode(n, false, false)
}

func (t *transpiler) emitAttrWithMode(n *gotreesitter.Node, typed, htmlElement bool) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	name := t.text(nameNode)
	if htmlElement && t.strict > 0 {
		switch name {
		case "className":
			name = "class"
		case "htmlFor":
			name = "for"
		}
	}

	value, boolAttr, ok := t.emitAttrValue(n)
	if !ok {
		return ""
	}
	if boolAttr {
		if typed {
			return fmt.Sprintf("%s: true", name)
		}
		return fmt.Sprintf("%s(%q)", t.gosxRef("BoolAttr"), name)
	}
	if typed {
		return fmt.Sprintf("%s: %s", name, value)
	}
	return fmt.Sprintf("%s(%q, %s)", t.gosxRef("Attr"), name, value)
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
		if typ == "jsx_element" || typ == "jsx_raw_text_element" ||
			typ == "jsx_self_closing_element" ||
			typ == "jsx_expression_container" || typ == "jsx_fragment" ||
			typ == "jsx_text" {
			result := t.emit(child)
			if result != "" {
				children = append(children, result)
			}
			continue
		}
		// Any other jsx_* child is a node kind this function does not know how
		// to emit. Report it instead of dropping it: a silent drop is how an
		// inline <script> body could disappear from the output while the
		// transpile call still reported success.
		if strings.HasPrefix(typ, "jsx_") {
			t.errorf(child, "unhandled GSX child node %q; transpiler and grammar are out of sync", typ)
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
