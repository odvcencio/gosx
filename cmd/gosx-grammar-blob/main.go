// Command gosx-grammar-blob regenerates the embedded GSX grammar blob.
//
// gosx.Language() loads gosx-grammar.blob in preference to generating the
// language from GosxGrammar() at runtime, so any edit to grammar.go only
// takes effect once the blob is rebuilt. Run this after changing the grammar:
//
//	go run ./cmd/gosx-grammar-blob
//
// It writes gosx-grammar.blob at the module root.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"m31labs.dev/gosx"
)

func main() {
	out := "gosx-grammar.blob"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}

	_, blob, err := gosx.GenerateLanguageAndBlob(gosx.GosxGrammar())
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate grammar:", err)
		os.Exit(1)
	}
	if len(blob) == 0 {
		fmt.Fprintln(os.Stderr, "generate grammar: empty blob")
		os.Exit(1)
	}

	abs, err := filepath.Abs(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve path:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(abs, blob, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write blob:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes)\n", abs, len(blob))
}
