package typeoracle

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
)

// Field is one exported fact about a struct field: its name and its
// resolved go/types.Type. FieldSet reports Field values in declaration
// order, matching types.Struct's own field order.
type Field struct {
	Name string
	Type types.Type
}

// LookupType resolves name as a package-scope type declared somewhere in
// Session's checked files — a props struct, a sibling .go file's type, or
// any other package-level type declaration. It reports ok=false for an
// unknown name or for a Session with no checked Package (see Session's own
// doc comment for when that happens).
func (s *Session) LookupType(name string) (types.Type, bool) {
	if s == nil || s.Package == nil {
		return nil, false
	}
	obj := s.Package.Scope().Lookup(name)
	if obj == nil {
		return nil, false
	}
	typeName, ok := obj.(*types.TypeName)
	if !ok {
		return nil, false
	}
	return typeName.Type(), true
}

// PropsType resolves component's declared props struct type — the same
// name ir.Component.PropsType records for it (Session.Files carries the
// compiled *ir.Program this reads from) — and reports its field set. It
// reports ok=false when component names no known strict component, when
// that component declares no props type, or when the named type does not
// resolve to a struct (should not happen for a props type the strict
// syntax gate already accepted, but query methods never panic on an
// unexpected shape).
func (s *Session) PropsType(component string) (*types.Struct, bool) {
	if s == nil {
		return nil, false
	}
	name, ok := s.componentProps[component]
	if !ok {
		return nil, false
	}
	return s.StructType(name)
}

// StructType resolves typeName to its underlying *types.Struct. It reports
// ok=false when typeName is unknown or does not resolve to a struct.
func (s *Session) StructType(typeName string) (*types.Struct, bool) {
	typ, ok := s.LookupType(typeName)
	if !ok {
		return nil, false
	}
	structType, ok := typ.Underlying().(*types.Struct)
	if !ok {
		return nil, false
	}
	return structType, true
}

// FieldSet resolves typeName's field set, in declaration order. It reports
// ok=false under the same conditions as StructType.
func (s *Session) FieldSet(typeName string) ([]Field, bool) {
	structType, ok := s.StructType(typeName)
	if !ok {
		return nil, false
	}
	fields := make([]Field, structType.NumFields())
	for i := range fields {
		v := structType.Field(i)
		fields[i] = Field{Name: v.Name(), Type: v.Type()}
	}
	return fields, true
}

// FieldType resolves one field of typeName. It reports ok=false when
// typeName is unknown, is not a struct, or has no field named field.
func (s *Session) FieldType(typeName, field string) (types.Type, bool) {
	structType, ok := s.StructType(typeName)
	if !ok {
		return nil, false
	}
	for i := 0; i < structType.NumFields(); i++ {
		v := structType.Field(i)
		if v.Name() == field {
			return v.Type(), true
		}
	}
	return nil, false
}

// EvalExpr type-checks the standalone Go expression source against
// Session's checked package scope and returns its resolved type. It is a
// thin wrapper over go/types.CheckExpr, so exprSrc may reference any
// package-scope declaration (a type, a const, a func) but not a local
// variable — there is no enclosing function body for a package-scope
// evaluation to place one in. That covers the intended use (does a literal,
// a const, or a reference to another package-level value assign to a given
// field's type), not general expression evaluation inside a component body.
func (s *Session) EvalExpr(exprSrc string) (types.Type, error) {
	if s == nil || s.Package == nil {
		return nil, fmt.Errorf("typeoracle: no checked package to evaluate %q against", exprSrc)
	}
	// Parsed against s.Fset itself, not a fresh FileSet: go/types.CheckExpr
	// resolves expr's own token.Pos values against the fset it is given,
	// and go/token.Pos values are base-offset integers scoped to whichever
	// FileSet registered them. Parsing expr into a different, freshly
	// created FileSet and then handing s.Fset to CheckExpr would leave
	// expr's positions resolving against whatever unrelated .gsx or
	// sibling .go file happens to occupy the same offset range in s.Fset —
	// wrong-file, wrong-line output from anything that later formats
	// expr's position, not a compile failure, so it would stay invisible
	// until something did.
	expr, err := parser.ParseExprFrom(s.Fset, "<typeoracle expression>", exprSrc, 0)
	if err != nil {
		return nil, fmt.Errorf("typeoracle: parse expression %q: %w", exprSrc, err)
	}
	info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)}
	if err := types.CheckExpr(s.Fset, s.Package, token.NoPos, expr, info); err != nil {
		return nil, err
	}
	tv, ok := info.Types[expr]
	if !ok {
		return nil, fmt.Errorf("typeoracle: expression %q produced no type", exprSrc)
	}
	return tv.Type, nil
}

// AssignableToField reports whether the Go expression exprSrc's type is
// assignable to typeName's named field — the query API's answer to "can
// this value fill this props field" (the package doc's own example query).
// The bool return is meaningless when err != nil (exprSrc failed to parse
// or type-check, or the field does not exist).
func (s *Session) AssignableToField(exprSrc, typeName, field string) (bool, error) {
	fieldType, ok := s.FieldType(typeName, field)
	if !ok {
		return false, fmt.Errorf("typeoracle: %s has no field %s", typeName, field)
	}
	exprType, err := s.EvalExpr(exprSrc)
	if err != nil {
		return false, err
	}
	return types.AssignableTo(exprType, fieldType), nil
}
