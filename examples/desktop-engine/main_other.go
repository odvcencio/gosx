//go:build !windows && !darwin

package main

import (
	"fmt"
	"os"
)

// The GoSX desktop host runs on Windows and macOS today.
func main() {
	fmt.Fprintln(os.Stderr, "desktop-engine needs the GoSX desktop host, which runs on Windows and macOS")
	os.Exit(2)
}
