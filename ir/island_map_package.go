//go:build !tinygo && !js

package ir

import (
	"errors"
	"fmt"
	"go/ast"
	"go/types"

	"m31labs.dev/gosx/island/program"
)

var errIslandMapTypeUnresolved = errors.New("island map type is unresolved")

// islandPackageMapTypes uses the same selected sibling files and source
// projection as AOT admission. Incomplete evidence fails rather than guessing.
func islandPackageMapTypes(p *Program, component string) (map[string]program.ExprType, error) {
	checked, err := aotCheckSource(p)
	if err != nil {
		return nil, err
	}
	result, err := islandMapTypesFromChecked(checked, component)
	if !errors.Is(err, errIslandMapTypeUnresolved) {
		return result, err
	}
	checked, err = checkIslandSource(p, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errIslandMapTypeUnresolved, err)
	}
	result, err = islandMapTypesFromChecked(checked, component)
	if errors.Is(err, errIslandMapTypeUnresolved) {
		return nil, fmt.Errorf("%w; ensure imported dependencies are available locally and run go mod download if needed", err)
	}
	return result, err
}

func islandMapTypesFromChecked(checked *aotCheckedSource, component string) (map[string]program.ExprType, error) {
	if checked == nil || checked.info == nil {
		return nil, fmt.Errorf("package type evidence for %s is unavailable", component)
	}
	var fn *ast.FuncDecl
	for _, decl := range checked.file.Decls {
		candidate, ok := decl.(*ast.FuncDecl)
		if ok && candidate.Recv == nil && candidate.Name.Name == component {
			fn = candidate
			break
		}
	}
	if fn == nil || fn.Type.Params == nil {
		return nil, fmt.Errorf("package type evidence for component %s is unavailable", component)
	}
	var props types.Type
	for _, param := range fn.Type.Params.List {
		for _, name := range param.Names {
			if name.Name == "props" {
				if object := checked.info.Defs[name]; object != nil {
					props = object.Type()
				}
			}
		}
	}
	if props == nil {
		return nil, fmt.Errorf("%w: package type evidence for %s props is unavailable", errIslandMapTypeUnresolved, component)
	}
	if hasIslandEncodingMethod(props, "MarshalJSON", "MarshalText") {
		return nil, fmt.Errorf("%s props use a custom JSON/text encoder that island map indexing cannot prove", component)
	}
	fields, ok := types.Unalias(props).Underlying().(*types.Struct)
	if !ok {
		if invalidIslandMapType(props) {
			return nil, fmt.Errorf("%w: package type for %s props", errIslandMapTypeUnresolved, component)
		}
		return nil, fmt.Errorf("package type evidence for %s props is not a struct", component)
	}
	result := make(map[string]program.ExprType)
	for i := 0; i < fields.NumFields(); i++ {
		field := fields.Field(i)
		if invalidIslandMapType(field.Type()) {
			return nil, fmt.Errorf("%w: package type for %s.%s", errIslandMapTypeUnresolved, component, field.Name())
		}
		m, ok := types.Unalias(field.Type()).Underlying().(*types.Map)
		if !ok {
			continue
		}
		if invalidIslandMapType(m.Key()) || invalidIslandMapType(m.Elem()) {
			return nil, fmt.Errorf("%w: package map type for %s.%s", errIslandMapTypeUnresolved, component, field.Name())
		}
		key, ok := types.Unalias(m.Key()).Underlying().(*types.Basic)
		if !ok || key.Kind() != types.String && key.Info()&types.IsInteger == 0 {
			result[field.Name()] = unsupportedMapIndexType
			continue
		}
		// Custom JSON/text encoders can change the map, its values, or a
		// separately transported lookup key.
		if hasIslandEncodingMethod(field.Type(), "MarshalJSON", "MarshalText") ||
			hasIslandEncodingMethod(m.Elem(), "MarshalJSON", "MarshalText") ||
			hasIslandEncodingMethod(m.Key(), "MarshalJSON", "MarshalText") {
			result[field.Name()] = unsupportedMapIndexType
			continue
		}
		elem, ok := types.Unalias(m.Elem()).Underlying().(*types.Basic)
		if !ok {
			result[field.Name()] = unsupportedMapIndexType
			continue
		}
		switch elem.Kind() {
		case types.Bool:
			result[field.Name()] = program.TypeBool
		case types.String:
			result[field.Name()] = program.TypeString
		default:
			if elem.Info()&(types.IsInteger|types.IsFloat) != 0 {
				result[field.Name()] = program.TypeAny
			} else {
				result[field.Name()] = unsupportedMapIndexType
			}
		}
	}
	return result, nil
}

func hasIslandEncodingMethod(typ types.Type, names ...string) bool {
	for _, name := range names {
		if method, _, _ := types.LookupFieldOrMethod(typ, true, nil, name); method != nil {
			return true
		}
	}
	return false
}

func invalidIslandMapType(typ types.Type) bool {
	if typ == nil {
		return true
	}
	basic, ok := types.Unalias(typ).Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Invalid
}
