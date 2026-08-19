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
	"m31labs.dev/gosx/internal/strictcomponent"
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

	// SharedImports resolves a shared (./ or ../ prefixed) import to its Go
	// projection facts: the Go import path substituted for the relative
	// source text, and the props shape of every strict component the
	// target directory declares. gosx check (strictcheck) is the intended
	// producer, built from the target directory's own loaded programs
	// (shared components design, section 5.2); transpile never walks a
	// directory to build this map itself (design rule 1: no file input or
	// output). A shared import this map has no entry for still parses, but
	// any call through it fails with a message naming gosx check rather
	// than emitting Go that cannot type-check.
	SharedImports map[string]SharedImport
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
		sharedImports:    opts.SharedImports,
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
	src         []byte
	lang        *gotreesitter.Language
	sourceFile  string
	imports     map[string]string // alias -> path
	propsTypes  map[string]string
	propsFields map[string]map[string]string
	// structFieldTypes records, per same-file struct name, each field's raw
	// declared type text — collectStructFields' companion map, populated
	// alongside propsFields. emitStrictEach uses it to name a strict
	// <Each>'s gosx.Map callback element type without re-deriving it from
	// ir.Component (transpile.go works from the CST directly, not the IR).
	structFieldTypes map[string]map[string]string
	strictNames      map[string]struct{} // same-file gosx_component_declaration names, props or not
	// slotNames records, per same-file strict component name, the sorted
	// set of named slots that component's body declares (gosx#249) —
	// transpile's own CST-walk-based twin of ir/lower.go's l.slotHoles,
	// needed because emitStrictComponent must place a gosx.Node parameter
	// for each BEFORE the variadic children parameter (Go forbids a
	// parameter after "...T"), and emitComponentCall must supply a value
	// for each at every nested call site. Two independent implementations
	// of "which slots does this body declare" is an accepted, pre-existing
	// risk in this file: transpile has no ir.Program to read a decided
	// answer from (see emitStrictComponent's children-arity comment for
	// why that same tradeoff was made once already), so this walks the CST
	// directly through componentDeclaredSlots, the same shape
	// ir/lower.go's componentDeclaredSlots uses.
	slotNames map[string][]string
	// currentPropsType is the props type of the strict component whose body
	// is currently being emitted (empty outside strict emission) — needed
	// to resolve a same-file <Each of> or spread source's element/struct
	// type by walking structFieldTypes from the right root.
	currentPropsType string
	errs             []string
	hasStrict        bool
	strict           int
	gosxAlias        string
	injectGosx       bool
	strictProjection bool
	importNames      map[string]string
	// sharedImports resolves a shared import's raw source path text (e.g.
	// "./ui") to its Go projection facts. See Options.SharedImports.
	sharedImports map[string]SharedImport
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
	case "import_declaration":
		return t.emitImportDeclaration(n)
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
		case "const_declaration", "type_declaration":
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

// emitImportDeclaration re-emits an import declaration for the ordinary
// (non-strict-projection) transpile output, rewriting every shared (./ or
// ../ prefixed) spec that Options.SharedImports resolves to its Go import
// path, with an explicit alias so the emitted meaning does not depend on
// the resolved package's own declared identifier. transpile has no
// filesystem access of its own (design rule 1), so a shared spec this
// file's resolver has no entry for passes through unrewritten; any call
// site that actually uses it fails separately with a message naming gosx
// check (errUnresolvedSharedCall), so the unrewritten text never reaches a
// caller as successful output. A declaration with no shared spec, or run
// with no SharedImports at all, is untouched byte-for-byte.
func (t *transpiler) emitImportDeclaration(n *gotreesitter.Node) string {
	original := t.text(n)
	if len(t.sharedImports) == 0 {
		return original
	}
	file, err := parser.ParseFile(token.NewFileSet(), t.sourceFile, "package gosximportrewrite\n"+original, parser.ImportsOnly)
	if err != nil {
		return original
	}
	changed := false
	specs := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		alias := ""
		explicit := spec.Name != nil
		if explicit {
			alias = spec.Name.Name
		}
		emitPath := importPath
		if target, ok := t.sharedImports[importPath]; ok && strings.TrimSpace(target.GoImportPath) != "" {
			emitPath = target.GoImportPath
			if !explicit {
				alias = t.defaultImportName(importPath)
			}
			changed = true
		}
		if alias != "" {
			specs = append(specs, alias+" "+strconv.Quote(emitPath))
		} else {
			specs = append(specs, strconv.Quote(emitPath))
		}
	}
	if !changed {
		return original
	}
	if len(specs) == 1 && !strings.Contains(original, "(") {
		return "import " + specs[0]
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for _, spec := range specs {
		b.WriteString("\t" + spec + "\n")
	}
	b.WriteByte(')')
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
			// A shared (./ or ../ prefixed) spec projects to its resolved Go
			// import path — the alias itself is unaffected, since it is
			// derived (here and at the call site, via t.imports) from the
			// same relative path text either way. Retaining the import below
			// is what keeps the alias valid as the shared call's selector
			// root (shared components design, section 5.2).
			emitPath := importPath
			if target, ok := t.sharedImports[importPath]; ok && strings.TrimSpace(target.GoImportPath) != "" {
				emitPath = target.GoImportPath
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
			imports = append(imports, projectionImport{alias: emitAlias, path: emitPath})
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
		fieldTypes := make(map[string]string)
		var collect func(*gotreesitter.Node)
		collect = func(node *gotreesitter.Node) {
			if node == nil {
				return
			}
			if t.nodeType(node) == "field_declaration" {
				fieldType := ""
				if fieldTypeNode := t.childByField(node, "type"); fieldTypeNode != nil {
					fieldType = strings.TrimSpace(t.text(fieldTypeNode))
				}
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
					fieldTypes[field] = fieldType
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
			name := t.text(nameNode)
			t.propsFields[name] = fields
			if t.structFieldTypes == nil {
				t.structFieldTypes = make(map[string]map[string]string)
			}
			t.structFieldTypes[name] = fieldTypes
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
		if typ == "gosx_component_declaration" {
			if t.strictNames == nil {
				t.strictNames = make(map[string]struct{})
			}
			t.strictNames[name] = struct{}{}
			if bodyNode := t.childByField(child, "body"); bodyNode != nil {
				if slots := t.componentDeclaredSlots(bodyNode); len(slots) > 0 {
					if t.slotNames == nil {
						t.slotNames = make(map[string][]string)
					}
					t.slotNames[name] = slots
				}
			}
		}
		if propsType := t.extractPropsType(child); propsType != "" {
			t.propsTypes[name] = propsType
		}
	}
}

// componentDeclaredSlots reports the sorted, de-duplicated set of named
// slots bodyNode declares — every {slotName} child expression hole it
// contains (strictcomponent.IsSlotExpression). It mirrors
// ir/lower.go's lowerer.componentDeclaredSlots, including the same
// deliberate refusal to descend into an attribute value (an attribute
// expression can never place rendered markup, so counting one here would
// wrongly declare a parameter for a slot the body can never actually
// place).
func (t *transpiler) componentDeclaredSlots(bodyNode *gotreesitter.Node) []string {
	if bodyNode == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var names []string
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		switch t.nodeType(node) {
		case "jsx_attribute", "jsx_spread_attribute":
			return
		case "jsx_expression_container":
			exprNode := t.childByField(node, "expression")
			if exprNode != nil {
				if name, ok := strictcomponent.IsSlotExpression(t.text(exprNode)); ok {
					if _, dup := seen[name]; !dup {
						seen[name] = struct{}{}
						names = append(names, name)
					}
				}
			}
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(bodyNode)
	sort.Strings(names)
	return names
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

	// Each named slot this body declares (a {slotName} child expression
	// hole) takes its own gosx.Node parameter (gosx#249), positioned before
	// the variadic children parameter below — Go forbids a parameter after
	// "...T", so a slot param can never follow it. componentDeclaredSlots
	// walks the same body this function is about to emit, so the parameter
	// list and the body's own {slotName} references can never name a
	// different slot set.
	for _, slot := range t.componentDeclaredSlots(bodyNode) {
		if params != "" {
			params += ", "
		}
		params += strictcomponent.SlotBindingName(slot) + " " + t.gosxRef("Node")
	}

	// Every strict component takes children, unconditionally.
	//
	// A variadic parameter accepts zero arguments, so every call site that
	// passes none keeps compiling unchanged. The alternative — emit the
	// parameter only for a body that renders children — would need transpile
	// to decide "renders children" for itself, because transpile walks the
	// CST with no ir.Program in hand. Two implementations of one predicate
	// drift, and the drift would show up as a projected signature that does
	// not match the checked contract. So arity stays a gosx-level rule with a
	// single owner, ir.Component.AcceptsChildren, and it reports a message
	// that names the remedy instead of Go's "too many arguments".
	//
	// The parameter is spelled children because the body's {children} hole
	// projects to gosx.Expr(children) (emitExprContainer), and gosx.Expr
	// fragments a []Node.
	if params != "" {
		params += ", "
	}
	params += strictcomponent.ChildrenBinding + " ..." + t.gosxRef("Node")

	prevPropsType := t.currentPropsType
	t.currentPropsType = t.propsTypes[t.text(nameNode)]
	t.strict++
	body := t.emitDefault(bodyNode)
	t.strict--
	t.currentPropsType = prevPropsType
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

	if t.isStrictEachTag(tag) {
		return t.emitStrictEach(openNode, n)
	}

	// emitChildrenAndSlots partitions any statically slot-tagged direct
	// child out of children (gosx#249). slots is only ever consulted by
	// the two component-call branches below — <If>/<Each> (handled above)
	// and a plain HTML element (emitElementCall) never declare a slot to
	// route one into, so discarding it there is correct: ir.Lower already
	// rejects a slot="Name" attribute reaching either shape before this
	// function runs (see emitChildrenAndSlots' doc comment).
	children, slots := t.emitChildrenAndSlots(n)

	if t.isStrictConditionalTag(tag) {
		if cond, ok := t.strictConditionalCondExpr(openNode); ok {
			return t.emitStrictConditional(cond, children)
		}
	}

	if verbatim, ok := t.strictSpreadCallVerbatim(tag, openNode); ok {
		return verbatim
	}

	if isComponent(tag) {
		if propsType, ok := t.typedPropsType(tag); ok {
			return t.emitTypedComponentCall(tag, propsType, t.emitTypedAttrsForTag(tag, openNode), children, t.orderedSlotArgs(tag, slots))
		}
		if rawPath, shared := t.sharedImportPath(memberTagAlias(tag)); shared {
			t.errUnresolvedSharedCall(openNode, tag, rawPath)
			return ""
		}
		return t.emitComponentCall(tag, t.emitAttrs(openNode), children, t.orderedSlotArgs(tag, slots))
	}
	return t.emitElementCall(tag, t.emitHTMLAttrs(openNode), children)
}

// orderedSlotArgs projects slots — a call site's statically slot-tagged
// children, keyed by slot name — into positional Go argument text, one
// entry per slot tag declares (t.slotNames, sorted the same way
// emitStrictComponent orders its own slot parameters), so the returned
// slice lines up 1:1 with those parameters regardless of what the call
// site did or did not supply. A slot the call site did not tag falls back
// to the zero gosx.Node{} — the same "declared but not supplied" sentinel
// the render-entry EntrySlots path (route/fileprogram.go) leaves
// unresolved, which renders empty.
func (t *transpiler) orderedSlotArgs(tag string, slots map[string]string) []string {
	declared := t.slotNames[tag]
	if len(declared) == 0 {
		return nil
	}
	args := make([]string, len(declared))
	for i, name := range declared {
		if value, ok := slots[name]; ok {
			args[i] = value
			continue
		}
		args[i] = t.gosxRef("Node") + "{}"
	}
	return args
}

// memberTagAlias returns tag's alias segment for a dotted tag ("ui" for
// "ui.TeamMark"), or "" for a bare tag. It is a thin wrapper over
// splitMemberTag for call sites that only need the alias.
func memberTagAlias(tag string) string {
	alias, _, ok := splitMemberTag(tag)
	if !ok {
		return ""
	}
	return alias
}

// isStrictConditionalTag reports whether tag resolves to the strict <If>
// builtin rather than a same-file component named If. It mirrors the
// shadow rule in validateStrictComponentCall (ir/lower.go): the IR semantic
// gate (gosx.Compile, run before any emission in Transpile) has already
// proven the file compiles under that same rule, so by the time emission
// runs, tag=="If" outside t.strictNames can only be the builtin.
func (t *transpiler) isStrictConditionalTag(tag string) bool {
	if t.strict == 0 || tag != "If" {
		return false
	}
	_, shadowed := t.strictNames[tag]
	return !shadowed
}

// strictConditionalCondExpr extracts the verbatim source of a strict <If>'s
// cond attribute. validateStrictConditionalCall (ir/lower.go) has already
// rejected every other attribute shape before transpile emission runs, so
// this is an extraction step, not a second validation pass.
func (t *transpiler) strictConditionalCondExpr(openNode *gotreesitter.Node) (string, bool) {
	for i := 0; i < int(openNode.NamedChildCount()); i++ {
		child := openNode.NamedChild(i)
		if t.nodeType(child) != "jsx_attribute" {
			continue
		}
		nameNode := t.childByField(child, "name")
		if nameNode == nil || t.text(nameNode) != "cond" {
			continue
		}
		value, boolAttr, ok := t.emitAttrValue(child)
		if !ok || boolAttr {
			return "", false
		}
		return value, true
	}
	return "", false
}

// emitStrictConditional projects a strict <If cond={...}> call as a call to
// gosx.If, so the Go compiler proves cond is exactly bool
// (func If(cond bool, child Node) Node, helpers.go). Children collapse into
// one gosx.Fragment so If's single-child signature holds whether the source
// wrote zero, one, or many GSX children.
func (t *transpiler) emitStrictConditional(cond string, children []string) string {
	return fmt.Sprintf("%s(%s, %s(%s))", t.gosxRef("If"), cond, t.gosxRef("Fragment"), strings.Join(children, ", "))
}

// isStrictEachTag reports whether tag resolves to the strict <Each> builtin
// rather than a same-file component named Each. It mirrors
// isStrictConditionalTag's shadow rule and its reasoning: gosx.Compile
// (run before any emission in Transpile) has already proven the file
// compiles under ir/lower.go's identical carve-out, so by the time
// emission runs, tag=="Each" outside t.strictNames can only be the
// builtin.
func (t *transpiler) isStrictEachTag(tag string) bool {
	if t.strict == 0 || tag != "Each" {
		return false
	}
	_, shadowed := t.strictNames[tag]
	return !shadowed
}

// emitStrictEach projects a strict <Each of={...} as="x" [index="i"]> call
// onto gosx.Map (design spec section 2.8): the of source, the item and
// (optional) index binding names, and the Go compiler-provable element
// type together produce
//
//	gosx.Map(<of>, func(<as> <ElemType>, <index or _> int) gosx.Node {
//	    return gosx.Fragment(<children...>)
//	})
//
// ir/lower.go's validateStrictEachCall has already proven the shape,
// binding names, and loopable element type before transpile emission
// runs, so this is extraction, not a second validation pass. elementNode
// is nil for a self-closing <Each /> (no children).
func (t *transpiler) emitStrictEach(openNode, elementNode *gotreesitter.Node) string {
	var ofExpr, asName, indexName string
	for i := 0; i < int(openNode.NamedChildCount()); i++ {
		child := openNode.NamedChild(i)
		if t.nodeType(child) != "jsx_attribute" {
			continue
		}
		nameNode := t.childByField(child, "name")
		if nameNode == nil {
			continue
		}
		switch t.text(nameNode) {
		case "of":
			if value, boolAttr, ok := t.emitAttrValue(child); ok && !boolAttr {
				ofExpr = value
			}
		case "as":
			asName = trimGSXStringLiteral(child, t)
		case "index":
			indexName = trimGSXStringLiteral(child, t)
		}
	}

	var children []string
	if elementNode != nil {
		children = t.emitChildren(elementNode)
	}

	elemType := t.strictEachElemType(ofExpr)
	indexParam := "_"
	if indexName != "" {
		indexParam = indexName
	}
	body := fmt.Sprintf("%s(%s)", t.gosxRef("Fragment"), strings.Join(children, ", "))
	return fmt.Sprintf("%s(%s, func(%s %s, %s int) %s { return %s })",
		t.gosxRef("Map"), ofExpr, asName, elemType, indexParam, t.gosxRef("Node"), body)
}

// trimGSXStringLiteral reads a jsx_attribute's static string value (the
// grammar's jsx_string_literal, quotes included) and strips the quotes —
// used for as/index, which are always the static-string shape by the time
// emission runs.
func trimGSXStringLiteral(attr *gotreesitter.Node, t *transpiler) string {
	valueNode := t.childByField(attr, "value")
	if valueNode == nil {
		return ""
	}
	raw := t.text(valueNode)
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return raw[1 : len(raw)-1]
	}
	return raw
}

// strictEachElemType resolves a props-rooted <Each of> source to its
// element struct name by walking t.structFieldTypes from
// t.currentPropsType's base name — the transpiler's own re-derivation of
// the same declared-text rule ir/lower.go's admitStrictEachElemType
// enforces, needed here only to name the gosx.Map callback parameter. It
// falls back to "any" for a shape the lowerer would already have rejected
// (unreachable once gosx.Compile has run), so a transpiler bug fails as an
// invalid-Go build error instead of a panic.
func (t *transpiler) strictEachElemType(ofExpr string) string {
	path, ok := strictcomponent.ServerPropPath(ofExpr)
	if !ok || len(path) != 1 {
		return "any"
	}
	fieldType := strings.TrimSpace(t.structFieldTypes[strictStructBaseName(t.currentPropsType)][path[0]])
	if !strings.HasPrefix(fieldType, "[]") {
		return "any"
	}
	elem := strings.TrimSpace(strings.TrimPrefix(fieldType, "[]"))
	if elem == "" {
		return "any"
	}
	return elem
}

// strictStructBaseName strips a declared type's pointer prefix, package
// qualifier, and generic/array brackets down to the bare same-file struct
// name t.structFieldTypes and t.propsFields key on — transpile.go's own
// copy of ir/lower.go's propsBaseType, kept local since the two packages
// share no such helper today.
func strictStructBaseName(typeText string) string {
	typeText = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(typeText), "*"))
	if idx := strings.LastIndex(typeText, "."); idx >= 0 {
		typeText = typeText[idx+1:]
	}
	if idx := strings.Index(typeText, "["); idx >= 0 {
		typeText = typeText[:idx]
	}
	return typeText
}

// strictSpreadCallVerbatim recognizes E2 tier 1's proven call shape (design
// spec section 3.2): inside a strict body, a call to a same-file strict
// component with exactly one spread attribute and no other attributes.
// ir/lower.go's validateStrictToStrictSpreadCall has already proven the
// spread source's declared type is exactly the callee's props type before
// transpile emission runs, so the call can be emitted verbatim —
// Callee(<source>) — and the Go compiler proves the rest with zero
// synthesis. This bypasses emitTypedAttrsForType's spread rejection only
// for this proven shape; every other spread shape (a legacy caller's tier-2
// spread, or any shape the lowerer did not prove) still reaches that
// rejection with its own message.
func (t *transpiler) strictSpreadCallVerbatim(tag string, n *gotreesitter.Node) (string, bool) {
	if t.strict == 0 {
		return "", false
	}
	if _, strictCallee := t.strictNames[tag]; !strictCallee {
		return "", false
	}
	spreadCount := 0
	otherCount := 0
	var spreadExpr string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch t.nodeType(child) {
		case "jsx_spread_attribute":
			spreadCount++
			if exprNode := t.childByField(child, "expression"); exprNode != nil {
				spreadExpr = t.text(exprNode)
			}
		case "jsx_attribute":
			otherCount++
		}
	}
	if spreadCount != 1 || otherCount != 0 {
		return "", false
	}
	return tag + "(" + spreadExpr + ")", true
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

	if t.isStrictEachTag(tag) {
		return t.emitStrictEach(n, nil)
	}

	if t.isStrictConditionalTag(tag) {
		if cond, ok := t.strictConditionalCondExpr(n); ok {
			return t.emitStrictConditional(cond, nil)
		}
	}

	if verbatim, ok := t.strictSpreadCallVerbatim(tag, n); ok {
		return verbatim
	}

	if isComponent(tag) {
		// A self-closing tag has no children at all, so no direct child
		// can ever carry a static slot="Name" attribute here — nil slots
		// (orderedSlotArgs' zero value) correctly produces one zero
		// gosx.Node{} per slot tag declares, and none where it declares
		// none.
		if propsType, ok := t.typedPropsType(tag); ok {
			return t.emitTypedComponentCall(tag, propsType, t.emitTypedAttrsForTag(tag, n), nil, t.orderedSlotArgs(tag, nil))
		}
		if rawPath, shared := t.sharedImportPath(memberTagAlias(tag)); shared {
			t.errUnresolvedSharedCall(n, tag, rawPath)
			return ""
		}
		return t.emitComponentCall(tag, t.emitAttrs(n), nil, t.orderedSlotArgs(tag, nil))
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

// isProplessStrictComponent reports whether tag names a same-file strict
// component (t.strictNames) with no declared props type. emitStrictComponent
// emits such a component's Go signature with no leading props parameter at
// all, so no value — Props() included — belongs in that argument position
// at any nested call site. A strict component WITH a declared props type
// never reaches emitComponentCall in the first place (emitGSXElement routes
// it through emitTypedComponentCall via typedPropsType), so this check
// only ever needs to rule the propless case in or out.
func (t *transpiler) isProplessStrictComponent(tag string) bool {
	if _, ok := t.strictNames[tag]; !ok {
		return false
	}
	return strings.TrimSpace(t.propsTypes[tag]) == ""
}

func (t *transpiler) emitComponentCall(tag string, attrs []string, children []string, slotArgs []string) string {
	proplessStrict := t.isProplessStrictComponent(tag)

	var b strings.Builder
	fmt.Fprintf(&b, "%s(", tag)

	// wroteArg tracks whether the call has written its first argument yet,
	// so the slot loop below can prepend its own separating comma correctly
	// whether or not Props(...) came first.
	//
	// Props(...) belongs here only for an UNTYPED LEGACY callee — this call
	// shape (no typedPropsType match — see emitGSXElement) is reached by
	// both a propless strict callee and an untyped legacy one, but only
	// the legacy callee's declared parameter (`props any` or `props
	// gosx.AttrList`) can actually receive an AttrList value. A propless
	// strict callee has no leading parameter at all (emitStrictComponent
	// emits none when the component declares no props), so passing one
	// positionally there either fails to compile or, worse, silently
	// binds to the wrong parameter. This was a pre-existing bug — see the
	// CHANGELOG entry naming it and TestCheckFileAllowsProplessStrictCalleeWithChildren
	// (strictcheck/check_test.go), which fails on the unfixed code and
	// passes here — independent of and predating gosx#249's named slots.
	wroteArg := false
	if !proplessStrict && (len(attrs) > 0 || len(children) > 0) {
		b.WriteString(t.gosxRef("Props") + "(")
		b.WriteString(strings.Join(attrs, ", "))
		b.WriteByte(')')
		wroteArg = true
	}

	// A callee that declares one or more named slots (gosx#249) takes a
	// gosx.Node parameter for each, positioned before its variadic children
	// parameter (emitStrictComponent). slotArgs (orderedSlotArgs,
	// emitGSXElement) already lines up 1:1 with those parameters — a
	// statically slot-tagged child's own projection where the call site
	// supplied one, the zero gosx.Node{} where it did not — so this loop
	// only ever places what it is given, in the order it is given.
	for _, arg := range slotArgs {
		if wroteArg {
			b.WriteString(", ")
		}
		b.WriteString(arg)
		wroteArg = true
	}

	for _, child := range children {
		if child != "" {
			if wroteArg {
				b.WriteString(", ")
			}
			b.WriteString(child)
			wroteArg = true
		}
	}

	b.WriteByte(')')
	return b.String()
}

func (t *transpiler) typedPropsType(tag string) (string, bool) {
	propsType := strings.TrimSpace(t.propsTypes[tag])
	if propsType == "" || isAttrListPropsType(propsType) {
		if sharedType, ok := t.sharedPropsType(tag); ok {
			return sharedType, true
		}
		return t.gosxUIPropsType(tag)
	}
	return propsType, true
}

// isSharedImportPath reports whether path is a shared import per the shared
// components design section 4.1: a "./"- or "../"-prefixed relative
// directory reference, never a legal Go import path in module mode.
func isSharedImportPath(path string) bool {
	return strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

// sharedImportPath reports whether alias names a shared import in this
// file and, if so, returns its raw (unrewritten) source path text — the key
// Options.SharedImports resolves against.
func (t *transpiler) sharedImportPath(alias string) (string, bool) {
	raw, ok := t.imports[alias]
	if !ok || !isSharedImportPath(raw) {
		return "", false
	}
	return raw, true
}

// isSharedCallTag reports whether tag's alias names a shared import in this
// file, regardless of whether Options.SharedImports resolves it. This is
// the distinction that separates "this call needs gosx check" from "this
// call is an ordinary dotted tag resolved through a Go binding" — the two
// rows section 6.1 of the shared components design keeps apart.
func (t *transpiler) isSharedCallTag(tag string) bool {
	alias, _, ok := splitMemberTag(tag)
	if !ok {
		return false
	}
	_, shared := t.sharedImportPath(alias)
	return shared
}

// sharedComponent resolves tag's callee component from
// Options.SharedImports, which gosx check builds from the shared
// directory's own loaded programs (design section 5.2). transpile has no
// filesystem access of its own to try harder (design rule 1), so it
// reports ok only when both the import path and the component name are
// present in the supplied map.
func (t *transpiler) sharedComponent(tag string) (SharedComponent, bool) {
	alias, component, ok := splitMemberTag(tag)
	if !ok {
		return SharedComponent{}, false
	}
	rawPath, shared := t.sharedImportPath(alias)
	if !shared {
		return SharedComponent{}, false
	}
	target, ok := t.sharedImports[rawPath]
	if !ok {
		return SharedComponent{}, false
	}
	comp, ok := target.Components[component]
	if !ok || strings.TrimSpace(comp.PropsType) == "" {
		return SharedComponent{}, false
	}
	return comp, true
}

// sharedPropsType resolves tag's callee props type the same way
// gosxUIPropsType resolves a m31labs.dev/gosx/ui call: alias qualified onto
// the props type name, so emitTypedComponentCall's composite literal reads
// as an ordinary qualified Go type, exactly like the call itself.
func (t *transpiler) sharedPropsType(tag string) (string, bool) {
	alias, _, ok := splitMemberTag(tag)
	if !ok {
		return "", false
	}
	comp, ok := t.sharedComponent(tag)
	if !ok {
		return "", false
	}
	return alias + "." + comp.PropsType, true
}

// errUnresolvedSharedCall reports the one outcome the shared components
// design requires of gosx transpile with no resolver (or an incomplete
// one): a message naming gosx check as the supported path, instead of
// emitting Go that cannot type-check. rawPath is the shared import's own
// source text (e.g. "./ui"), which names the resolver gap precisely.
func (t *transpiler) errUnresolvedSharedCall(n *gotreesitter.Node, tag, rawPath string) {
	t.errorf(n, "shared component %s cannot be projected without a resolved Go import for %q; gosx transpile does not resolve a ./ or ../ import on its own — run gosx check, which resolves and type-checks a shared .gsx import", tag, rawPath)
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

func (t *transpiler) emitTypedComponentCall(tag, propsType string, attrs []string, children []string, slotArgs []string) string {
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

	// A callee that declares one or more named slots (gosx#249) takes a
	// gosx.Node parameter for each, positioned before its variadic
	// children parameter (emitStrictComponent), right after the props
	// literal above. slotArgs (orderedSlotArgs, emitGSXElement/
	// emitSelfClosing) already lines up 1:1 with those parameters.
	//
	// Before this loop existed, every child — slot-tagged or not — sat in
	// the plain children slice below in document order, and this function
	// appended them all positionally after the props literal with no
	// awareness that some belonged in an earlier, named parameter
	// position instead. That call still compiled whenever the total
	// argument count matched (a slot count of N plus the true children
	// count still fills N+variadic parameters), so a caller's markup
	// order alone — not its slot="Name" tags — silently decided which
	// child filled which slot. See staticSlotName's doc comment
	// (transpile.go) for the same defect in emitComponentCall's sibling
	// path, and TestCheckFileTypedComponentRoutesSlotByNameNotPosition
	// (strictcheck/check_test.go) for the regression test.
	for _, arg := range slotArgs {
		b.WriteString(", ")
		b.WriteString(arg)
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
	if comp, ok := t.sharedComponent(tag); ok {
		return t.emitTypedAttrsWithAliases(tag, comp.Fields, n)
	}
	return t.emitTypedAttrsForType(tag, t.propsTypes[tag], n)
}

func (t *transpiler) emitTypedAttrsForType(tag, propsType string, n *gotreesitter.Node) []string {
	baseType, _ := typedPropsLiteralType(propsType)
	if idx := strings.LastIndex(baseType, "."); idx >= 0 {
		baseType = baseType[idx+1:]
	}
	if idx := strings.Index(baseType, "["); idx >= 0 {
		baseType = baseType[:idx]
	}
	return t.emitTypedAttrsWithAliases(tag, t.propsFields[baseType], n)
}

// emitTypedAttrsWithAliases projects a typed call's named attributes into
// composite-literal fields against aliases, and applies the shared spread
// refusal. A same-file struct call (emitTypedAttrsForType, aliases from
// t.propsFields) and a shared cross-directory struct call
// (emitTypedAttrsForTag, aliases from Options.SharedImports) both resolve
// through this one function, so the two call shapes cannot drift apart —
// there is exactly one boundary, matching how route/fileprogram.go's
// localComponentProps already proves both call kinds through one function
// at render time.
func (t *transpiler) emitTypedAttrsWithAliases(tag string, aliases map[string]string, n *gotreesitter.Node) []string {
	var attrs []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch t.nodeType(child) {
		case "jsx_spread_attribute":
			_, strictCallee := t.strictNames[tag]
			if strictCallee || t.isSharedCallTag(tag) {
				// E2 tier 2 (design spec section 3.3, non-goal 3.5), and its
				// cross-directory counterpart (shared components design,
				// section 5.1 R2): a spread into a strict callee — same-file
				// or shared — is proved only at the file-renderer boundary
				// (strictSpreadProps, route/fileprogram.go), never by
				// transpile. Full transpile has no equivalent proof, so it
				// keeps failing here, naming the supported path instead of
				// the unproven one.
				t.errorf(child, "spread attributes at a strict component call site are proven by the file renderer boundary, not by gosx transpile; render %s through gosx's file router, or call it with explicit typed attributes from a strict body", tag)
				continue
			}
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
	children, _ := t.emitChildrenAndSlots(n)
	return children
}

// emitChildrenAndSlots is emitChildren's slot-aware twin (gosx#249): it
// additionally partitions a statically slot-tagged direct child's own
// projection out of the returned children list and into slots, keyed by
// slot name, mirroring ir/lower.go's partitionCallSlots at the CST level.
//
// It does not duplicate that function's validation. strictcheck.CheckFile
// always runs ir.Lower before this projection (checkPackage's builtin
// stage order — validateStrictRenderEntries and the transpile pass both
// read a *ir.Program lowering already built, so a program that reaches
// this function has already passed partitionCallSlots' checks: a
// non-static slot value, a slot on a call that is not a component, or a
// slot the callee does not declare all fail lowering first, with a
// clearer message than a Go compiler error would give, and
// strictcheck.CheckFile returns that failure without ever reaching this
// function. This function's only job, for an already-proved-valid
// program, is routing a static slot supply to the right Go parameter
// position instead of leaving it in the shared children group by
// argument-count coincidence — see staticSlotName's doc comment for what
// went wrong before this split existed.
func (t *transpiler) emitChildrenAndSlots(n *gotreesitter.Node) (children []string, slots map[string]string) {
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
			if result == "" {
				continue
			}
			if name, ok := t.staticSlotName(child); ok {
				if slots == nil {
					slots = make(map[string]string)
				}
				slots[name] = result
				continue
			}
			children = append(children, result)
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
	return children, slots
}

// staticSlotName reports child's slot="Name" attribute value, when child
// carries one spelled as a plain string literal. ok is false for a child
// with no slot attribute at all, or one whose slot value is not a static
// string literal — ir.Lower already rejects the latter shape before this
// function is ever reached (see emitChildrenAndSlots' doc comment), so
// returning ok=false for it here just keeps such a child in the ordinary
// children group, in a projection the build never reaches anyway.
//
// Before this function existed, a slot-tagged child's own projection sat
// in the plain children list like any other, and the call-site emitters
// (emitComponentCall, emitTypedComponentCall) appended every child
// positionally after the callee's declared slot parameters — filling
// slotTitle with whichever child happened to come first in document
// order, not the one actually marked slot="Title". That call still
// compiled (argument count and type both matched by coincidence whenever
// the slot count matched the non-slot child count), so nothing caught it
// except reading the generated source by hand.
func (t *transpiler) staticSlotName(child *gotreesitter.Node) (name string, ok bool) {
	var attrsHost *gotreesitter.Node
	switch t.nodeType(child) {
	case "jsx_element":
		attrsHost = t.childByField(child, "open")
	case "jsx_self_closing_element":
		attrsHost = child
	default:
		return "", false
	}
	if attrsHost == nil {
		return "", false
	}
	for i := 0; i < int(attrsHost.NamedChildCount()); i++ {
		a := attrsHost.NamedChild(i)
		if t.nodeType(a) != "jsx_attribute" {
			continue
		}
		nameNode := t.childByField(a, "name")
		if nameNode == nil || t.text(nameNode) != "slot" {
			continue
		}
		valueNode := t.childByField(a, "value")
		if valueNode == nil || t.nodeType(valueNode) != "jsx_string_literal" {
			return "", false
		}
		raw := t.text(valueNode)
		if len(raw) >= 2 {
			raw = raw[1 : len(raw)-1]
		}
		return raw, true
	}
	return "", false
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
