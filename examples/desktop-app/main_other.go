//go:build !windows

package main

import (
	"fmt"
	"os"
)

// The GoSX desktop host runs on Windows today. Elsewhere, run the engine and
// open the printed address in a browser.
func main() {
	if err := serveEngine(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
