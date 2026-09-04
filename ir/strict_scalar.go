package ir

import "strings"

// strictRendererScalarType is shared by the host-only CST lowerer and the
// TinyGo-clean island lowerer. Keep the renderer boundary in one build-neutral
// file so production runtime builds compile the same scalar contract checked
// while reading strict component schemas.
func strictRendererScalarType(typeName string) bool {
	switch strings.TrimSpace(typeName) {
	case "string", "bool",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64":
		return true
	default:
		return false
	}
}
