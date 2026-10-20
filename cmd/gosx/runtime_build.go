package main

import (
	"fmt"
	"os"
	"strings"
)

func RunBuildRuntime(outDir string) error {
	return RunBuildRuntimeWithOptions(outDir, buildRuntimeOptions{})
}

func cmdBuildRuntime() {
	outDir := "build"
	opts := buildRuntimeOptions{RepoRoot: "."}
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--ouroboros-out":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "build-runtime error: --ouroboros-out requires a directory")
				os.Exit(1)
			}
			opts.OuroborosOut = args[i]
		case "--inventory":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "build-runtime error: --inventory requires a file")
				os.Exit(1)
			}
			opts.InventoryPath = args[i]
		case "--root":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "build-runtime error: --root requires a directory")
				os.Exit(1)
			}
			opts.RepoRoot = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				fmt.Fprintf(os.Stderr, "build-runtime error: unknown flag %s\n", arg)
				os.Exit(1)
			}
			outDir = arg
		}
	}
	if err := RunBuildRuntimeWithOptions(outDir, opts); err != nil {
		fmt.Fprintf(os.Stderr, "build-runtime error: %v\n", err)
		os.Exit(1)
	}
}
