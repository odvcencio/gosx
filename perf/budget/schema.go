package budget

import (
	"errors"
	"regexp"
	"strings"
	"sync"
)

var schemaPatterns sync.Map

func schemaPattern(pattern string) (*regexp.Regexp, error) {
	if cached, ok := schemaPatterns.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	cached, _ := schemaPatterns.LoadOrStore(pattern, re)
	return cached.(*regexp.Regexp), nil
}

// Embedded schemas are checked once at initialization, before any input loads.
func checkSchemaShape(raw any, definitions map[string]any, definitionMap bool) error {
	s, ok := raw.(map[string]any)
	if !ok || s == nil {
		return errors.New("invalid schema shape")
	}
	if definitionMap {
		for _, child := range s {
			if err := checkSchemaShape(child, definitions, false); err != nil {
				return err
			}
		}
		return nil
	}
	fail := func() error { return errors.New("invalid schema shape") }
	if ref, exists := s["$ref"]; exists {
		name, ok := ref.(string)
		if !ok || !strings.HasPrefix(name, "#/$defs/") || definitions[strings.TrimPrefix(name, "#/$defs/")] == nil {
			return fail()
		}
	}
	if pattern, exists := s["pattern"]; exists {
		text, ok := pattern.(string)
		if !ok {
			return fail()
		}
		if _, err := schemaPattern(text); err != nil {
			return fail()
		}
	}
	if format, exists := s["format"]; exists && format != "date" && format != "date-time" {
		return fail()
	}
	if kind, exists := s["type"]; exists {
		kinds := []any{kind}
		if list, ok := kind.([]any); ok {
			kinds = list
			if len(list) == 0 {
				return fail()
			}
		}
		for _, k := range kinds {
			switch k {
			case "object", "array", "string", "number", "integer", "boolean", "null":
			default:
				return fail()
			}
			if k == "array" && s["items"] == nil {
				return fail()
			}
		}
	}
	if properties, exists := s["properties"]; exists {
		props, ok := properties.(map[string]any)
		if !ok {
			return fail()
		}
		for _, child := range props {
			if err := checkSchemaShape(child, definitions, false); err != nil {
				return err
			}
		}
	}
	if required, exists := s["required"]; exists {
		list, ok := required.([]any)
		if !ok {
			return fail()
		}
		for _, key := range list {
			if _, ok := key.(string); !ok {
				return fail()
			}
		}
	}
	for _, key := range []string{"items", "propertyNames", "additionalProperties"} {
		if child, exists := s[key]; exists {
			if key == "additionalProperties" {
				if _, ok := child.(bool); ok {
					continue
				}
			}
			if err := checkSchemaShape(child, definitions, false); err != nil {
				return err
			}
		}
	}
	if choices, exists := s["oneOf"]; exists {
		list, ok := choices.([]any)
		if !ok || len(list) == 0 {
			return fail()
		}
		for _, child := range list {
			if err := checkSchemaShape(child, definitions, false); err != nil {
				return err
			}
		}
	}
	return nil
}
