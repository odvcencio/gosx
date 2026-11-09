package main

import (
	"fmt"
	"io"
	"os"

	"m31labs.dev/gosx/scene/selenabuild"
)

// cmdSelena runs `gosx selena build|check [dir]`.
func cmdSelena() {
	if err := runSelenaCommand(os.Args[2:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "selena error: %v\n", err)
		os.Exit(1)
	}
}

func selenaUsage(w io.Writer) {
	fmt.Fprint(w, `gosx selena - Compile Selena materials into build artifacts

Usage:
  gosx selena build [dir]   write <file>.sel.json next to every .sel source
  gosx selena check [dir]   fail when an artifact is missing or out of date

dir defaults to the current directory. build.selena in gosx.config.json sets
the targets (default ["wgsl", "gles"]), material library modules, and the
specular variant. gosx build runs "gosx selena build" before compiling Go, so
packages can //go:embed the artifacts and load them with
scene.DecodeSelenaArtifact and scene.MaterialFromSelenaArtifact. Run
"gosx selena check" in CI to catch a .sel edit without a rebuilt artifact.

`)
}

func runSelenaCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		selenaUsage(stdout)
		return nil
	}
	if len(args) > 2 {
		return fmt.Errorf("usage: gosx selena build|check [dir]")
	}
	dir := "."
	if len(args) == 2 {
		dir = args[1]
	}
	cfg, err := loadProjectConfig(dir)
	if err != nil {
		return err
	}
	switch args[0] {
	case "build":
		// An explicit build runs even when build.selena.disabled keeps the
		// step out of gosx build.
		opts := cfg.Build.Selena
		opts.Disabled = false
		return runSelenaBuildStep(dir, opts, stdout)
	case "check":
		if err := selenabuild.Check(dir, cfg.Build.Selena); err != nil {
			return err
		}
		sources, err := selenabuild.Sources(dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Selena: %d sources, artifacts up to date\n", len(sources))
		return nil
	default:
		return fmt.Errorf("unknown selena command %q (want build or check)", args[0])
	}
}

// runSelenaBuildStep compiles the project's .sel sources into artifacts. gosx
// build calls it before compiling Go code so embedded artifacts are current.
// A project with no .sel sources prints nothing.
func runSelenaBuildStep(dir string, opts selenabuild.Options, stdout io.Writer) error {
	if opts.Disabled {
		return nil
	}
	report, err := selenabuild.Build(dir, opts)
	if err != nil {
		return err
	}
	if len(report.Sources) == 0 {
		return nil
	}
	materials := len(report.Sources) - len(report.Libraries)
	fmt.Fprintf(stdout, "  Selena: %d artifacts (%d written)\n", materials, len(report.Written))
	for _, path := range report.Written {
		fmt.Fprintf(stdout, "    wrote %s\n", path)
	}
	return nil
}
