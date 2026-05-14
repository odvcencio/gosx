package schema

import _ "embed"

//go:embed schema.json
var jsonSchema []byte

// JSONSchema returns the embedded strict SceneIR JSON Schema document.
func JSONSchema() []byte {
	return append([]byte(nil), jsonSchema...)
}
