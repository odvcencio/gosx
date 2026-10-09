//go:build !tinygo

package ir

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Capture declarations before lowering any component: package declarations
// bind even when they appear after the use. Field and selector names do not
// introduce lexical bindings and are deliberately absent from this set.
func (l *lowerer) collectAOTBindings(root *gotreesitter.Node) {
	b := &aotSourceBindings{declared: make(map[string]bool), imports: l.imports}
	for _, imp := range l.prog.Imports {
		if imp.Alias == "" && imp.Path != signalImportPath {
			// An import path does not prove an external package's declared name.
			b.unresolvedImports = true
		}
	}
	var visit func(*gotreesitter.Node)
	visit = func(n *gotreesitter.Node) {
		if n == nil {
			return
		}
		switch l.nodeType(n) {
		case "type_spec", "type_alias", "function_declaration", "gosx_component_declaration":
			if name := l.childByField(n, "name"); name != nil {
				b.declared[l.text(name)] = true
			}
		case "var_spec", "const_spec", "parameter_declaration", "variadic_parameter_declaration", "type_parameter_declaration", "gosx_component_parameter":
			// These declarations can have multiple names with the same field.
			typ, value := l.childByField(n, "type"), l.childByField(n, "value")
			for i := 0; i < int(n.NamedChildCount()); i++ {
				child := n.NamedChild(i)
				if child == typ || child == value {
					break
				}
				if l.nodeType(child) == "identifier" || l.nodeType(child) == "type_identifier" {
					b.declared[l.text(child)] = true
				}
			}
		case "short_var_declaration", "range_clause", "receive_statement":
			for _, name := range l.extractAssignedNames(l.childByField(n, "left")) {
				b.declared[name] = true
			}
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(root)
	lang := l.lang
	b.packageNames = func(source []byte) (string, map[string]bool, error) {
		tree, err := gotreesitter.NewParser(lang).Parse(source)
		if err != nil {
			return "", nil, err
		}
		defer tree.Release()
		root := tree.RootNode()
		if root == nil || root.HasError() {
			return "", nil, fmt.Errorf("unresolved package declarations")
		}
		other := &lowerer{src: source, srcStr: string(source), lang: lang, prog: &Program{}, imports: NewImportTable()}
		other.collectAOTBindings(root)
		for i := 0; i < int(root.NamedChildCount()); i++ {
			if child := root.NamedChild(i); other.nodeType(child) == "package_clause" {
				other.lowerPackageClause(child)
			}
		}
		return other.prog.Package, other.prog.aotBindings.declared, nil
	}
	l.prog.aotBindings = b
}

// A compiler-supplied directory extends the declaration evidence to sibling
// files. Work on a private copy; admission must not mutate its caller's IR.
func aotPackageSource(src *Program) (*Program, error) {
	if src.aotBindings == nil {
		return nil, fmt.Errorf("source binding evidence is required")
	}
	if src.Dir == "" {
		return src, nil // Standalone compilation unit.
	}
	entries, err := os.ReadDir(src.Dir)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve package declarations")
	}
	b := *src.aotBindings
	b.declared = maps.Clone(b.declared)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if ext := filepath.Ext(name); ext != ".go" && ext != ".gsx" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src.Dir, name))
		if err != nil || b.packageNames == nil {
			return nil, fmt.Errorf("cannot resolve package declarations")
		}
		pkg, names, err := b.packageNames(data)
		if err != nil || pkg != src.Package {
			return nil, fmt.Errorf("unresolved package declarations")
		}
		maps.Copy(b.declared, names)
	}
	copy := *src
	copy.aotBindings = &b
	return &copy, nil
}

func (b *aotSourceBindings) universe(name string) bool {
	if b == nil || b.unresolvedImports || b.declared[name] {
		return false
	}
	if _, imported := b.imports.Lookup(name); imported {
		return false
	}
	// The profile uses only these predefined Go names. Declaration and
	// import evidence above proves they are unshadowed; no runtime type
	// checker is needed to resolve this closed set.
	switch name {
	case "int", "int32", "bool", "string", "true", "false", "len":
		return true
	default:
		return false
	}
}

func (l *lowerer) aotSignalConstructor(call *gotreesitter.Node) string {
	b := l.prog.aotBindings
	if b == nil || b.unresolvedImports {
		return ""
	}
	pkg, name := l.callName(l.childByField(call, "function"))
	if pkg == "" {
		if !b.declared[name] && b.imports.HasDotImport(signalImportPath) {
			return name
		}
		return ""
	}
	if b.declared[pkg] {
		return ""
	}
	if path, ok := b.imports.Lookup(pkg); ok {
		if path == signalImportPath {
			return pkg
		}
		return ""
	}
	if strings.TrimSpace(pkg) == "signal" {
		return pkg // GoSX's implicit signal import.
	}
	return ""
}
