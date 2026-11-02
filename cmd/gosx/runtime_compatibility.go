package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"

	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
)

func checkProjectRuntimeCompatibility(dir string) error {
	root, err := resolveGoSXModuleRoot(dir)
	if err != nil {
		return err
	}
	return checkRuntimeSourceCompatibility(root)
}

// checkRuntimeSourceCompatibility compares the selected project's runtime
// source with the compiler's contract before replacing any build output.
func checkRuntimeSourceCompatibility(root string) error {
	path := filepath.Join(root, "client", "runtime", "wasm", "abi.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return fmt.Errorf("read GoSX runtime contract: %w", err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			for i, name := range value.Names {
				if name.Name != "wasmManifestIdentity" || i >= len(value.Values) {
					continue
				}
				literal, ok := value.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					break
				}
				identity, err := strconv.Unquote(literal.Value)
				if err == nil && identity == runtimewasm.ManifestIdentity() {
					return nil
				}
				return fmt.Errorf("GoSX compiler and project runtime have incompatible ABI contracts; use matching GoSX source or install the CLI for the project's pinned version")
			}
		}
	}
	return fmt.Errorf("GoSX project runtime has no readable ABI identity in %s", path)
}
