package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const gosxModuleImportPath = "github.com/odvcencio/gosx"

func resolveGoSXModuleRoot(projectDir string) (string, error) {
	out, err := goListDir(projectDir, gosxModuleImportPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s module root: %w", gosxModuleImportPath, err)
	}
	if out == "" {
		return "", fmt.Errorf("resolve %s module root: empty result", gosxModuleImportPath)
	}
	return out, nil
}

func goListDir(projectDir string, importPath string) (string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", importPath)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
