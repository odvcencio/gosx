// Command perfbrowserpin validates the governed CI browser lanes.
package main

import (
	"fmt"
	"os"

	"m31labs.dev/gosx/internal/perfbrowserpin"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: perfbrowserpin <workflow.yml>")
		os.Exit(2)
	}
	source, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-perf-browser-pin: read workflow: %v\n", err)
		os.Exit(1)
	}
	if err := perfbrowserpin.Validate(source); err != nil {
		fmt.Fprintf(os.Stderr, "check-perf-browser-pin: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("check-perf-browser-pin: governed perf browser pin and lane separation passed")
}
