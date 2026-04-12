// Command gosx is the GoSX compiler and development tool.
//
// Usage:
//
//	gosx build <dir>             Build GoSX application
//	gosx dev <dir>               Start development server with hot reload
//	gosx export <dir>            Pre-render static GoSX pages
//	gosx init [dir]              Scaffold a GoSX application or docs site
//	gosx compile <file.gsx>      Compile GoSX to Go
//	gosx check <file.gsx>        Parse and validate without emitting
//	gosx render <file.gsx>       Render component HTML to stdout
//	gosx fmt <file.gsx|dir>      Format GoSX source files
//	gosx lsp                     Start the GoSX language server over stdio
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/format"
	"github.com/odvcencio/gosx/internal/transpile"
	"github.com/odvcencio/gosx/render"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "build":
		cmdBuild()
	case "dev":
		cmdDev()
	case "export":
		cmdExport()
	case "init":
		cmdInit()
	case "compile":
		cmdCompile()
	case "check":
		cmdCheck()
	case "render":
		cmdRender()
	case "fmt", "format":
		cmdFmt()
	case "lsp":
		cmdLSP()
	case "perf":
		cmdPerf()
	case "repl":
		cmdRepl()
	case "version":
		fmt.Println("gosx v0.1.0")
	case "help", "-h", "--help":
		usage()
	default:
		fatal("unknown command: %s\nrun 'gosx help' for usage", cmd)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `gosx - Go-native web application platform

Usage:
  gosx <command> [arguments]

Commands:
  build <dir>          Build GoSX application
  dev <dir>            Start development server with hot reload
  export <dir>         Pre-render static GoSX pages
  init [dir]           Scaffold a GoSX application or docs site
  compile <file>       Compile .gsx file to Go
  check <file>         Parse and validate
  render <file> [comp] Render component to HTML
  fmt <path>           Format GoSX source files
  lsp                  Start the GoSX language server
  perf <url>           Profile browser runtime performance
  repl <url>           Interactive browser runtime explorer
  version              Print version

Init templates:
  gosx init my-app
  gosx init my-docs --template docs

`)
}

func cmdBuild() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: gosx build [--dev|--prod] <dir>")
		os.Exit(1)
	}
	dev := true
	dir := os.Args[2]
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--dev":
			dev = true
		case "--prod":
			dev = false
		default:
			dir = arg
		}
	}
	if err := RunBuild(dir, dev); err != nil {
		fmt.Fprintf(os.Stderr, "build error: %v\n", err)
		os.Exit(1)
	}
}

func cmdDev() {
	dir := argOrDefault(2, ".")
	if err := RunDev(dir); err != nil {
		fmt.Fprintf(os.Stderr, "gosx dev: %v\n", err)
		os.Exit(1)
	}
}

func cmdExport() {
	dir := argOrDefault(2, ".")
	if err := RunExport(dir); err != nil {
		fmt.Fprintf(os.Stderr, "gosx export: %v\n", err)
		os.Exit(1)
	}
}

func cmdCompile() {
	file := requireArg(2, "compile")
	source, err := os.ReadFile(file)
	if err != nil {
		fatal("read %s: %v", file, err)
	}

	output, err := transpile.Transpile(source, transpile.Options{SourceFile: file})
	if err != nil {
		fatal("compile: %v", err)
	}
	fmt.Print(output)
}

func cmdCheck() {
	file := requireArg(2, "check")
	if err := runCheck(file, os.Stderr); err != nil {
		fatal("check: %v", err)
	}
}

func runCheck(file string, stderr io.Writer) error {
	source, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}

	prog, err := gosx.Compile(source)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "ok: %d components\n", len(prog.Components))
	for _, c := range prog.Components {
		fmt.Fprintf(stderr, "  %s", c.Name)
		if c.PropsType != "" {
			fmt.Fprintf(stderr, "(%s)", c.PropsType)
		}
		fmt.Fprintln(stderr)
	}
	return nil
}

func cmdRender() {
	file := requireArg(2, "render")
	source, err := os.ReadFile(file)
	if err != nil {
		fatal("read %s: %v", file, err)
	}

	prog, err := gosx.Compile(source)
	if err != nil {
		fatal("compile: %v", err)
	}

	componentName := ""
	if len(os.Args) > 3 {
		componentName = os.Args[3]
	} else if len(prog.Components) > 0 {
		componentName = prog.Components[0].Name
	} else {
		fatal("no components found")
	}

	html, err := render.HTML(prog, componentName, render.Options{Indent: "  "})
	if err != nil {
		fatal("render: %v", err)
	}
	fmt.Println(html)
}

func cmdFmt() {
	if len(os.Args) < 3 {
		fmtUsage(os.Stderr)
		os.Exit(1)
	}

	args := os.Args[2:]
	check := false
	if len(args) > 0 && args[0] == "--check" {
		check = true
		args = args[1:]
	}
	if len(args) == 0 {
		fmtUsage(os.Stderr)
		os.Exit(1)
	}

	path := args[0]
	if path == "help" || path == "-h" || path == "--help" {
		fmtUsage(os.Stdout)
		return
	}

	var err error
	if check {
		_, err = RunFmtCheck(path, os.Stderr)
	} else {
		_, err = RunFmt(path, os.Stderr)
	}
	if err != nil {
		fatal("fmt: %v", err)
	}
}

func RunFmt(path string, stderr io.Writer) (int, error) {
	return runFmt(path, stderr, false)
}

func RunFmtCheck(path string, stderr io.Writer) (int, error) {
	return runFmt(path, stderr, true)
}

func runFmt(path string, stderr io.Writer, check bool) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}

	if !info.IsDir() {
		if err := formatFile(path, check); err != nil {
			return 0, err
		}
		return 1, nil
	}

	count := 0
	var firstErr error
	err = filepath.Walk(path, func(p string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if firstErr == nil {
				firstErr = walkErr
			}
			fmt.Fprintf(stderr, "gosx fmt: %s: %v\n", p, walkErr)
			return nil
		}
		if fi.IsDir() || filepath.Ext(fi.Name()) != ".gsx" {
			return nil
		}
		if err := formatFile(p, check); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", p, err)
			}
			fmt.Fprintf(stderr, "gosx fmt: %s: %v\n", p, err)
			return nil
		}
		count++
		return nil
	})
	if err != nil {
		return count, err
	}
	if firstErr != nil {
		return count, firstErr
	}
	if check {
		fmt.Fprintf(stderr, "gosx: verified %d files\n", count)
	} else {
		fmt.Fprintf(stderr, "gosx: formatted %d files\n", count)
	}
	return count, nil
}

func formatFile(path string, check bool) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	formatted, err := formatFileStable(source)
	if err != nil {
		return err
	}
	if check {
		if bytes.Equal(source, formatted) {
			return nil
		}
		return fmt.Errorf("needs formatting")
	}
	return os.WriteFile(path, formatted, 0644)
}

func formatFileStable(source []byte) ([]byte, error) {
	current := append([]byte(nil), source...)
	for i := 0; i < 8; i++ {
		next, err := format.Source(current)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(current, next) {
			return next, nil
		}
		current = next
	}
	return nil, fmt.Errorf("formatter did not converge")
}

func fmtUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx fmt - Format GoSX source files

Usage:
  gosx fmt <file.gsx|dir>
  gosx fmt --check <file.gsx|dir>

Examples:
  gosx fmt app/layout.gsx
  gosx fmt ./app
  gosx fmt --check .

`)
}

func requireArg(idx int, cmd string) string {
	if len(os.Args) <= idx {
		fatal("%s requires a file argument", cmd)
	}
	return os.Args[idx]
}

func argOrDefault(idx int, def string) string {
	if len(os.Args) <= idx {
		return def
	}
	return os.Args[idx]
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gosx: "+format+"\n", args...)
	os.Exit(1)
}
