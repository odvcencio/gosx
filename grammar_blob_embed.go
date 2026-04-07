package gosx

import _ "embed"

// embeddedGrammarBlob is the precompiled GoSX tree-sitter language blob.
//
// Refresh this file only when the grammar changes.
//
//go:embed gosx-grammar.blob
var embeddedGrammarBlob []byte

// GrammarBlob returns a copy of the embedded GoSX grammar blob.
func GrammarBlob() []byte {
	if len(embeddedGrammarBlob) == 0 {
		return nil
	}
	out := make([]byte, len(embeddedGrammarBlob))
	copy(out, embeddedGrammarBlob)
	return out
}
