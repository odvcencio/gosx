//go:build !tinygo

package ir

import "strings"

// isFormStateType resolves the framework form schema through the file's own
// import, including explicit aliases. A similarly named application type
// does not carry the framework action/CSRF contract.
func isFormStateType(prog *Program, typeName string) bool {
	for _, imp := range prog.Imports {
		if imp.Path != "m31labs.dev/gosx/route" || imp.Alias == "_" {
			continue
		}
		alias := imp.Alias
		if alias == "" {
			alias = "route"
		}
		name := alias + ".FormState"
		if alias == "." {
			name = "FormState"
		}
		if name == typeName {
			return true
		}
	}
	return false
}

func (l *lowerer) copyFormActionPaths(propsType string, reads map[string]strictReadClass) map[string]string {
	var paths map[string]string
	for path := range reads {
		parts := strings.Split(path, ".")
		if len(parts) < 2 || parts[len(parts)-1] != "ActionURL" {
			continue
		}
		parent := l.walkStrictHops("props", propsBaseType(propsType), parts[:len(parts)-1])
		if parent.failKind != strictHopOK || !isFormStateType(l.prog, parent.leafType) {
			continue
		}
		if paths == nil {
			paths = make(map[string]string)
		}
		paths[path] = strings.Join(parts[:len(parts)-1], ".") + ".CSRFToken"
	}
	return paths
}

func (l *lowerer) collectFormStateSchemas() {
	for _, imp := range l.prog.Imports {
		alias := imp.Alias
		if alias == "" {
			alias = "route"
		}
		name := alias + ".FormState"
		if alias == "." {
			name = "FormState"
		}
		if imp.Path != "m31labs.dev/gosx/route" || imp.Alias == "_" {
			continue
		}
		l.structTypes[name] = map[string]string{
			"ActionURL": "string", "CSRFToken": "string", "Message": "string",
			"OK": "bool", "Status": "int",
			"Values": "map[string]string", "FieldErrors": "map[string]string", "Flash": "map[string]string",
		}
	}
}
