//go:build !tinygo

package ir

import (
	"github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx/internal/strictcomponent"
)

func (l *lowerer) functionHasParameters(node *gotreesitter.Node) bool {
	parameters := l.childByField(node, "parameters")
	if parameters == nil {
		return false
	}
	for i := 0; i < int(parameters.NamedChildCount()); i++ {
		if l.nodeType(parameters.NamedChild(i)) != "comment" {
			return true
		}
	}
	return false
}

func (l *lowerer) functionHasResults(node *gotreesitter.Node) bool {
	result := l.childByField(node, "result")
	if result == nil {
		return false
	}
	if l.nodeType(result) != "parameter_list" {
		return true
	}
	for i := 0; i < int(result.NamedChildCount()); i++ {
		if l.nodeType(result.NamedChild(i)) != "comment" {
			return true
		}
	}
	return false
}

func (l *lowerer) strictCallbackAttribute(tag, name string) bool {
	if _, strict := l.strictNames[tag]; strict {
		props := propsBaseType(l.strictProps[tag])
		field := l.structFields[props][name]
		return islandCallbackType(l.structTypes[props][field])
	}
	_, event := islandEventTypes[name]
	return !IsComponent(tag) && event
}

func (l *lowerer) collectStrictCallbackRead(tag string, attr *gotreesitter.Node, reads map[string]strictReadClass) bool {
	if l.nodeType(attr) != "jsx_attribute" {
		return false
	}
	name, value := l.childByField(attr, "name"), l.childByField(attr, "value")
	if name == nil || value == nil || !l.strictCallbackAttribute(tag, l.text(name)) {
		return false
	}
	if path, ok := strictcomponent.ServerPropPath(stripGSXAttributeExpressionText(l.text(value))); ok {
		registerStrictPropRead(reads, path, strictReadCallback)
		return true
	}
	return false
}
