//go:build !tinygo

package ir

import (
	"fmt"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"go/token"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SchemaOrigin identifies the authored declaration; zero line/column mean unknown.
type SchemaOrigin struct {
	File         string
	Line, Column int
}

// SchemaField describes one direct exported field, not an embedded or aliased type.
type SchemaField struct {
	Name, Type string
	Origin     SchemaOrigin
}

// StructSchema describes an ordinary same-package Go struct. It supplies compiler
// visibility only; compiling actual Go remains necessary to prove the declaration.
type StructSchema struct {
	Name   string
	Fields []SchemaField
	Origin SchemaOrigin
}

// SchemaSet is an immutable, validated snapshot. Construct it with NewSchemaSet.
// A nil pointer or zero value supplies no external declarations. It performs no
// filesystem access, package loading, Go execution or source injection.
type SchemaSet struct {
	packageName string
	structs     []StructSchema
}

func schemaOrigin(origin SchemaOrigin) string {
	return fmt.Sprintf("%s:%d:%d", origin.File, origin.Line, origin.Column)
}
func exportedSchemaName(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return token.IsIdentifier(name) && unicode.IsUpper(r)
}

// NewSchemaSet copies and validates same-package named scalar/nested/slice schemas.
// Field aliases match GSX's existing exported-name and lower-camel convention;
// Go type aliases, imported types, embedded fields and cycles are not supported.
func NewSchemaSet(packageName string, structs []StructSchema) (*SchemaSet, error) {
	if len(structs) == 0 && packageName == "" {
		return &SchemaSet{}, nil
	}
	if !token.IsIdentifier(packageName) || packageName == "_" {
		return nil, fmt.Errorf("schema package %q is not a Go package identifier", packageName)
	}
	snapshot := &SchemaSet{packageName: packageName, structs: make([]StructSchema, len(structs))}
	for i, decl := range structs {
		snapshot.structs[i] = decl
		snapshot.structs[i].Fields = append([]SchemaField(nil), decl.Fields...)
	}
	sort.Slice(snapshot.structs, func(i, j int) bool {
		a, b := snapshot.structs[i], snapshot.structs[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return schemaOrigin(a.Origin) < schemaOrigin(b.Origin)
	})
	names := map[string]StructSchema{}
	for i := range snapshot.structs {
		decl := &snapshot.structs[i]
		if !token.IsIdentifier(decl.Name) || decl.Name == "_" {
			return nil, fmt.Errorf("schema %s: invalid struct name %q", schemaOrigin(decl.Origin), decl.Name)
		}
		if strictRendererScalarType(decl.Name) || decl.Name == "any" || decl.Name == "error" || decl.Name == "comparable" || decl.Name == "complex64" || decl.Name == "complex128" {
			return nil, fmt.Errorf("schema %s: predeclared type name %q is not supported", schemaOrigin(decl.Origin), decl.Name)
		}
		if decl.Origin.File == "" || decl.Origin.Line < 0 || decl.Origin.Column < 0 {
			return nil, fmt.Errorf("schema %s: invalid source provenance", decl.Name)
		}
		if old, ok := names[decl.Name]; ok {
			return nil, fmt.Errorf("duplicate schema %s: %s and %s", decl.Name, schemaOrigin(old.Origin), schemaOrigin(decl.Origin))
		}
		names[decl.Name] = *decl
		sort.Slice(decl.Fields, func(i, j int) bool {
			a, b := decl.Fields[i], decl.Fields[j]
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			if a.Type != b.Type {
				return a.Type < b.Type
			}
			return schemaOrigin(a.Origin) < schemaOrigin(b.Origin)
		})
		previous := ""
		for j := range decl.Fields {
			field := &decl.Fields[j]
			if field.Origin.File == "" {
				field.Origin = decl.Origin
			}
			if !exportedSchemaName(field.Name) || field.Origin.Line < 0 || field.Origin.Column < 0 {
				return nil, fmt.Errorf("schema %s: invalid field %q at %s", decl.Name, field.Name, schemaOrigin(field.Origin))
			}
			if previous == field.Name {
				return nil, fmt.Errorf("schema %s: duplicate field %s", decl.Name, field.Name)
			}
			previous = field.Name
			if strings.TrimSpace(field.Type) != field.Type || field.Type == "" {
				return nil, fmt.Errorf("schema %s.%s: invalid type %q", decl.Name, field.Name, field.Type)
			}
		}
	}
	for _, decl := range snapshot.structs {
		for _, field := range decl.Fields {
			if strictRendererScalarType(field.Type) {
				continue
			}
			name := strings.TrimPrefix(field.Type, "[]")
			if _, ok := names[name]; !ok {
				return nil, fmt.Errorf("schema %s.%s at %s: unsupported type %q; use an exact scalar or declared same-package struct or []struct", decl.Name, field.Name, schemaOrigin(field.Origin), field.Type)
			}
		}
	}
	active, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if active[name] {
			return fmt.Errorf("schema cycle involving %s", name)
		}
		if done[name] {
			return nil
		}
		active[name] = true
		for _, field := range names[name].Fields {
			if strictRendererScalarType(field.Type) {
				continue
			}
			if err := visit(strings.TrimPrefix(field.Type, "[]")); err != nil {
				return err
			}
		}
		delete(active, name)
		done[name] = true
		return nil
	}
	for _, decl := range snapshot.structs {
		if err := visit(decl.Name); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

// Structs returns a defensive copy, suitable for emitter metadata tables.
func (s *SchemaSet) Structs() []StructSchema {
	if s == nil {
		return nil
	}
	out := make([]StructSchema, len(s.structs))
	for i, decl := range s.structs {
		out[i] = decl
		out[i].Fields = append([]SchemaField(nil), decl.Fields...)
	}
	return out
}

// PackageName returns the package whose ordinary Go declarations are described.
func (s *SchemaSet) PackageName() string {
	if s == nil {
		return ""
	}
	return s.packageName
}

// Aliases returns a fresh table using the same ambiguity rules as GSX fields.
func SchemaAliases(decl StructSchema) map[string]string {
	aliases := make(map[string]string)
	for _, field := range decl.Fields {
		aliases[field.Name] = field.Name
		alias := lowerCamelInitialism(field.Name)
		if old, ok := aliases[alias]; ok && old != field.Name {
			aliases[alias] = ""
		} else if !ok {
			aliases[alias] = field.Name
		}
	}
	return aliases
}

func (l *lowerer) mergeExplicitSchemas(root *gotreesitter.Node) {
	if l.schemas == nil || len(l.schemas.structs) == 0 {
		return
	}
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if l.nodeType(child) == "package_clause" {
			l.lowerPackageClause(child)
		}
	}
	if l.schemas.packageName != l.prog.Package {
		l.errorf(root, "schema package %q does not match source package %q", l.schemas.packageName, l.prog.Package)
		return
	}
	declared := map[string]*gotreesitter.Node{}
	var visit func(*gotreesitter.Node)
	visit = func(node *gotreesitter.Node) {
		if l.nodeType(node) == "import_spec" {
			if name := l.childByField(node, "name"); name != nil {
				declared[l.text(name)] = name
			}
			return
		}
		if l.nodeType(node) == "type_spec" || l.nodeType(node) == "type_alias" || l.nodeType(node) == "function_declaration" || l.nodeType(node) == "gosx_component_declaration" {
			if name := l.childByField(node, "name"); name != nil {
				declared[l.text(name)] = name
			}
			return
		}
		if l.nodeType(node) == "var_spec" || l.nodeType(node) == "const_spec" {
			for i := 0; i < int(node.NamedChildCount()); i++ {
				name := node.NamedChild(i)
				if l.nodeType(name) == "identifier" {
					declared[l.text(name)] = name
				}
			}
			return
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			visit(node.NamedChild(i))
		}
	}
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if l.nodeType(child) == "import_declaration" || l.nodeType(child) == "type_declaration" || l.nodeType(child) == "var_declaration" || l.nodeType(child) == "const_declaration" || l.nodeType(child) == "function_declaration" || l.nodeType(child) == "gosx_component_declaration" {
			visit(child)
		}
	}
	for _, decl := range l.schemas.structs {
		if node, ok := declared[decl.Name]; ok {
			l.errorf(node, "source declaration %s conflicts with schema at %s", decl.Name, schemaOrigin(decl.Origin))
			continue
		}
		fields := make(map[string]string, len(decl.Fields))
		for _, field := range decl.Fields {
			fields[field.Name] = field.Type
		}
		l.structTypes[decl.Name] = fields
		l.structFields[decl.Name] = SchemaAliases(decl)
	}
}
