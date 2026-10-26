//go:build !tinygo

package ir

import (
	"fmt"
	"m31labs.dev/gosx/internal/strictcomponent"
	"sort"
	"strings"
)

func (l *lowerer) clientValueSchema(typeName string, definitions map[string]*ClientValueSchema, depth int) (*ClientValueSchema, error) {
	if depth > 64 {
		return nil, fmt.Errorf("client data type nesting exceeds 64 levels")
	}
	typeName = strings.TrimSpace(typeName)
	if strictRendererScalarType(typeName) && !l.scalarTypeShadows[typeName] {
		return &ClientValueSchema{Type: typeName}, nil
	}
	for _, container := range []struct{ prefix, kind string }{{"[]", "slice"}, {"map[string]", "map"}} {
		if element, ok := strings.CutPrefix(typeName, container.prefix); ok {
			schema, err := l.clientValueSchema(element, definitions, depth+1)
			if err != nil {
				return nil, err
			}
			return &ClientValueSchema{Type: container.kind, Element: schema}, nil
		}
	}
	plain, declared := l.clientStructs[typeName]
	if !declared || !plain {
		return nil, fmt.Errorf("client data type %s must use scalar builtins, slices, string-keyed maps, or same-file records with explicitly named exported fields", typeName)
	}
	if definitions[typeName] == nil {
		definition := &ClientValueSchema{Type: "object", Fields: make(map[string]*ClientValueSchema)}
		definitions[typeName] = definition
		var names []string
		for name := range l.structTypes[typeName] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			field, err := l.clientValueSchema(l.structTypes[typeName][name], definitions, depth+1)
			if err != nil {
				return nil, fmt.Errorf("field %s.%s: %w", typeName, name, err)
			}
			definition.Fields[name] = field
		}
	}
	return &ClientValueSchema{Ref: typeName}, nil
}

// clientReadSchema preserves only the selected path until its leaf. A whole
// collection/record read expands that leaf's declared data shape instead.
func (l *lowerer) clientReadSchema(typeName string, path []string, definitions map[string]*ClientValueSchema) (*ClientValueSchema, error) {
	if len(path) == 0 {
		return l.clientValueSchema(typeName, definitions, 0)
	}
	key, mapKey := strictcomponent.MapKey(path[0])
	if mapKey {
		if typeName != "map[string]string" || len(path) != 1 {
			return nil, fmt.Errorf("unsupported client map path")
		}
		return &ClientValueSchema{Type: "map", Fields: map[string]*ClientValueSchema{key: {Type: "string"}}}, nil
	}
	fieldType, ok := l.structTypes[propsBaseType(typeName)][path[0]]
	if !ok {
		return nil, fmt.Errorf("unknown client field %s.%s", typeName, path[0])
	}
	field, err := l.clientReadSchema(fieldType, path[1:], definitions)
	if err != nil {
		return nil, err
	}
	return &ClientValueSchema{Type: "object", Fields: map[string]*ClientValueSchema{path[0]: field}}, nil
}

func mergeClientSchema(current, next *ClientValueSchema) *ClientValueSchema {
	if current == nil {
		return next
	}
	if current.Ref != "" || current.Element != nil {
		return current
	}
	if next.Ref != "" || next.Element != nil {
		return next
	}
	if current.Fields != nil && next.Fields != nil {
		for name, field := range next.Fields {
			current.Fields[name] = mergeClientSchema(current.Fields[name], field)
		}
	}
	return current
}

func (l *lowerer) collectClientProps(component *Component) error {
	definitions := make(map[string]*ClientValueSchema)
	var paths []string
	for path, class := range l.strictReads[component.Name] {
		if class.has(strictReadScalar | strictReadState | strictReadLoopSource) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	var root *ClientValueSchema
	for _, path := range paths {
		segments := strings.Split(path, ".")
		resolved := l.walkStrictHops("props", propsBaseType(component.PropsType), segments)
		if resolved.failKind != strictHopOK {
			continue
		}
		value, err := l.clientReadSchema(component.PropsType, segments, definitions)
		if err != nil {
			return fmt.Errorf("island %s prop %s: %w", component.Name, path, err)
		}
		root = mergeClientSchema(root, value)
	}
	if root != nil {
		component.ClientProps = root.Fields
	}
	if len(definitions) != 0 {
		component.ClientTypes = definitions
	}
	return nil
}
