// Command gosx is the GoSX compiler and development tool.
//
// Usage:
//
//	gosx build <dir>             Build GoSX application
//	gosx dev <dir>               Start development server with hot reload
//	gosx init [dir]              Scaffold a GoSX application or docs site
//	gosx compile <file.gsx>      Compile GoSX to Go
//	gosx check <file.gsx>        Parse and validate without emitting
//	gosx render <file.gsx>       Render component HTML to stdout
//	gosx fmt <file.gsx|dir>      Format GoSX source files
//	gosx lsp                     Start the GoSX language server over stdio
package main

import (
	"fmt"
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
  init [dir]           Scaffold a GoSX application or docs site
  compile <file>       Compile .gsx file to Go
  check <file>         Parse and validate
  render <file> [comp] Render component to HTML
  fmt <path>           Format GoSX source files
  lsp                  Start the GoSX language server
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
	source, err := os.ReadFile(file)
	if err != nil {
		fatal("read %s: %v", file, err)
	}

	prog, err := gosx.Compile(source)
	if err != nil {
		fatal("check: %v", err)
	}
	fmt.Fprintf(os.Stderr, "ok: %d components\n", len(prog.Components))
	for _, c := range prog.Components {
		fmt.Fprintf(os.Stderr, "  %s", c.Name)
		if c.PropsType != "" {
			fmt.Fprintf(os.Stderr, "(%s)", c.PropsType)
		}
		fmt.Fprintln(os.Stderr)
	}
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
	path := requireArg(2, "fmt")

	info, err := os.Stat(path)
	if err != nil {
		fatal("stat %s: %v", path, err)
	}

	if info.IsDir() {
		count := 0
		err := filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			ext := filepath.Ext(fi.Name())
			if ext == ".gsx" {
				if err := formatFile(p); err != nil {
					fmt.Fprintf(os.Stderr, "gosx fmt: %s: %v\n", p, err)
				} else {
					count++
				}
			}
			return nil
		})
		if err != nil {
			fatal("fmt: %v", err)
		}
		fmt.Fprintf(os.Stderr, "gosx: formatted %d files\n", count)
	} else {
		if err := formatFile(path); err != nil {
			fatal("fmt: %v", err)
		}
	}
}

func formatFile(path string) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	formatted, err := format.Source(source)
	if err != nil {
		return err
	}
	return os.WriteFile(path, formatted, 0644)
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
