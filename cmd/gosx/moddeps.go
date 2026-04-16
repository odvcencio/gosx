package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func ensureModuleDependencies(projectDir string) error {
	if err := goListDeps(projectDir, nil, "./..."); err != nil {
		return fmt.Errorf("resolve module dependencies: %w", err)
	}
	return nil
}

func ensureWASMRuntimeDependencies(projectDir string) error {
	if err := goListDeps(projectDir, []string{"GOOS=js", "GOARCH=wasm"}, gosxModuleImportPath+"/client/wasm"); err != nil {
		return fmt.Errorf("resolve wasm runtime dependencies: %w", err)
	}
	return nil
}

func goListDeps(projectDir string, extraEnv []string, packages ...string) error {
	args := []string{"list", "-deps"}
	args = append(args, packages...)

	cmd := exec.Command("go", args...)
	cmd.Dir = projectDir
	cmd.Env = append(execEnvWithoutGoFlags(), "GOFLAGS=-mod=mod", "GOWORK=off")
	cmd.Env = append(cmd.Env, extraEnv...)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func execEnvWithoutGoFlags() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOFLAGS=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}
