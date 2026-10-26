package route

import (
	"fmt"
	"reflect"
	"sort"

	"m31labs.dev/gosx/ir"
	islandprogram "m31labs.dev/gosx/island/program"
)

// Strict Go selectors use declared field names. Avoid sending a second copy of
// every collection under its attribute alias, while retaining aliases read by
// legacy event expressions in the same island.
func clientIslandProps(component *ir.Component, program *islandprogram.Program, values map[string]any) map[string]any {
	if len(component.ClientProps) == 0 {
		return values
	}
	result := make(map[string]any, len(component.ClientProps))
	aliases := make(map[string]string, len(component.ClientProps))
	for name := range component.ClientProps {
		result[name] = values[name]
		aliases[lowerCamelInitialism(name)] = name
	}
	addAlias := func(alias string) {
		if name, known := aliases[alias]; known {
			result[alias] = values[name]
		}
	}
	for _, expression := range program.Exprs {
		if expression.Op == islandprogram.OpPropGet && expression.Value != "props" {
			addAlias(expression.Value)
		}
		if expression.Op != islandprogram.OpIndex || len(expression.Operands) != 2 {
			continue
		}
		receiver, key := program.Exprs[expression.Operands[0]], program.Exprs[expression.Operands[1]]
		if receiver.Op != islandprogram.OpPropGet || receiver.Value != "props" {
			continue
		}
		if key.Op == islandprogram.OpLitString {
			addAlias(key.Value)
		} else {
			for alias := range aliases {
				addAlias(alias)
			}
		}
	}
	return result
}

type clientValueProjection struct {
	run func(reflect.Value, int) (any, error)
}

type clientProjectionKey struct {
	schema *ir.ClientValueSchema
	typeOf reflect.Type
}

// projectClientValue proves the Go type once, then copies only declared client
// fields. In particular it never invokes MarshalJSON or follows embedded
// fields. A memoized projection supports recursive record types; runtime depth
// is bounded independently so cyclic values cannot hang a render.
func projectClientValue(value any, schema *ir.ClientValueSchema, definitions map[string]*ir.ClientValueSchema) (any, error) {
	if value == nil {
		return nil, fmt.Errorf("client value is untyped nil")
	}
	cache := make(map[clientProjectionKey]*clientValueProjection)
	var compile func(*ir.ClientValueSchema, reflect.Type, int) (*clientValueProjection, error)
	compile = func(shape *ir.ClientValueSchema, rt reflect.Type, depth int) (*clientValueProjection, error) {
		if shape == nil || depth > 64 {
			return nil, fmt.Errorf("invalid client data schema or nesting beyond 64 levels")
		}
		for references := 0; shape.Ref != ""; references++ {
			if references >= 64 || definitions[shape.Ref] == nil {
				return nil, fmt.Errorf("invalid client record reference %s", shape.Ref)
			}
			shape = definitions[shape.Ref]
		}
		key := clientProjectionKey{shape, rt}
		if previous := cache[key]; previous != nil {
			return previous, nil
		}
		projection := &clientValueProjection{}
		cache[key] = projection
		var run func(reflect.Value, int) (any, error)
		switch shape.Type {
		case "slice":
			if rt.Kind() != reflect.Slice {
				return nil, fmt.Errorf("client value has type %s, want a slice", rt)
			}
			element, err := compile(shape.Element, rt.Elem(), depth+1)
			if err != nil {
				return nil, fmt.Errorf("slice element: %w", err)
			}
			run = func(rv reflect.Value, depth int) (any, error) {
				if rv.IsNil() {
					return nil, nil
				}
				result := make([]any, rv.Len())
				for index := range result {
					item, err := element.run(rv.Index(index), depth+1)
					if err != nil {
						return nil, fmt.Errorf("element %d: %w", index, err)
					}
					result[index] = item
				}
				return result, nil
			}
		case "map":
			if rt.Kind() != reflect.Map || rt.Key() != reflect.TypeFor[string]() {
				return nil, fmt.Errorf("client value has type %s, want a string-keyed map", rt)
			}
			if shape.Element != nil {
				element, err := compile(shape.Element, rt.Elem(), depth+1)
				if err != nil {
					return nil, fmt.Errorf("map element: %w", err)
				}
				run = func(rv reflect.Value, depth int) (any, error) {
					if rv.IsNil() {
						return nil, nil
					}
					result := make(map[string]any, rv.Len())
					iterator := rv.MapRange()
					for iterator.Next() {
						item, err := element.run(iterator.Value(), depth+1)
						if err != nil {
							return nil, err
						}
						result[iterator.Key().String()] = item
					}
					return result, nil
				}
			} else {
				fields := make(map[string]*clientValueProjection)
				for name, field := range shape.Fields {
					project, err := compile(field, rt.Elem(), depth+1)
					if err != nil {
						return nil, err
					}
					fields[name] = project
				}
				run = func(rv reflect.Value, depth int) (any, error) {
					result := make(map[string]any, len(fields))
					for name, project := range fields {
						item := rv.MapIndex(reflect.ValueOf(name))
						if !item.IsValid() {
							item = reflect.Zero(rt.Elem())
						}
						projected, err := project.run(item, depth+1)
						if err != nil {
							return nil, err
						}
						result[name] = projected
					}
					return result, nil
				}
			}
		case "object":
			if rt.Kind() != reflect.Struct {
				return nil, fmt.Errorf("client value has type %s, want a declared record", rt)
			}
			type fieldProjection struct {
				name    string
				index   int
				project *clientValueProjection
			}
			var names []string
			for name := range shape.Fields {
				names = append(names, name)
			}
			sort.Strings(names)
			fields := make([]fieldProjection, 0, len(names))
			for _, name := range names {
				field, found := rt.FieldByName(name)
				if !found || !field.IsExported() || len(field.Index) != 1 {
					return nil, fmt.Errorf("client record %s requires an explicitly declared exported field %s", rt, name)
				}
				project, err := compile(shape.Fields[name], field.Type, depth+1)
				if err != nil {
					return nil, fmt.Errorf("field %s: %w", name, err)
				}
				fields = append(fields, fieldProjection{name, field.Index[0], project})
			}
			run = func(rv reflect.Value, depth int) (any, error) {
				result := make(map[string]any, len(fields))
				for _, field := range fields {
					item, err := field.project.run(rv.Field(field.index), depth+1)
					if err != nil {
						return nil, fmt.Errorf("field %s: %w", field.name, err)
					}
					result[field.name] = item
				}
				return result, nil
			}
		default:
			if !strictScalarFieldType(shape.Type) {
				return nil, fmt.Errorf("unsupported client scalar %s", shape.Type)
			}
			if _, err := requireStrictScalarType(reflect.Zero(rt).Interface(), shape.Type); err != nil {
				return nil, err
			}
			run = func(rv reflect.Value, _ int) (any, error) { return rv.Interface(), nil }
		}
		projection.run = func(rv reflect.Value, depth int) (any, error) {
			if depth > 64 {
				return nil, fmt.Errorf("client data exceeds 64 levels or contains a cycle")
			}
			return run(rv, depth)
		}
		return projection, nil
	}
	rv := reflect.ValueOf(value)
	projection, err := compile(schema, rv.Type(), 0)
	if err != nil {
		return nil, err
	}
	return projection.run(rv, 0)
}
