package ir

// ClientValueSchema describes plain Go data without runtime reflection or
// browser code. Ref names an entry in Component.ClientTypes. Type is an exact
// scalar builtin, slice, map (string keys), or object. References keep recursive
// record definitions finite when the IR is serialized.
type ClientValueSchema struct {
	Type    string                        `json:"type,omitempty"`
	Ref     string                        `json:"ref,omitempty"`
	Element *ClientValueSchema            `json:"element,omitempty"`
	Fields  map[string]*ClientValueSchema `json:"fields,omitempty"`
}
