package main

import (
	"fmt"
	"os"

	"m31labs.dev/gosx/lsp"
)

func cmdLSP() {
	if err := lsp.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "gosx lsp: %v\n", err)
		os.Exit(1)
	}
}
