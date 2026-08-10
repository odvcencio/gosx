package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	goModuleCommandFlags         = "-mod=mod -buildvcs=false"
	goModuleCommandFlagsReadonly = "-mod=readonly -buildvcs=false"
)

func ensureModuleDependencies(projectDir string) error {
	if err := ensureModuleDependenciesWithFlags(projectDir, goModuleCommandFlags); err != nil {
		return fmt.Errorf("resolve module dependencies: %w", err)
	}
	return nil
}

func ensureModuleDependenciesWithFlags(projectDir, moduleFlags string) error {
	return goListDeps(projectDir, moduleFlags, nil, "./...")
}

func ensureWASMRuntimeDependencies(projectDir string) error {
	return ensureWASMRuntimeDependenciesWithFlags(projectDir, goModuleCommandFlags)
}

func ensureWASMRuntimeDependenciesWithFlags(projectDir, moduleFlags string) error {
	if err := goListDeps(projectDir, moduleFlags, []string{"GOOS=js", "GOARCH=wasm"}, gosxModuleImportPath+"/client/wasm"); err != nil {
		return fmt.Errorf("resolve wasm runtime dependencies: %w", err)
	}
	return nil
}

func goListDeps(projectDir, moduleFlags string, extraEnv []string, packages ...string) error {
	args := []string{"list", "-deps"}
	args = append(args, packages...)

	cmd := exec.Command("go", args...)
	cmd.Dir = projectDir
	cmd.Env = append(execEnvWithoutGoFlags(), "GOFLAGS="+defaultString(moduleFlags, goModuleCommandFlags), "GOWORK=off")
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
