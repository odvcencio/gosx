package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx"
)

// checkVersionSkew fails fast when a project's go.mod pins a different
// m31labs.dev/gosx release than this CLI. Left unchecked, resolveGoSXModuleRoot
// resolves client sources from the module cache path of the pinned release,
// and a newer CLI fails deep inside that path with an obscure missing-file
// error instead of a version diagnostic.
func checkVersionSkew(projectDir string) error {
	goModPath, err := projectGoModPath(projectDir)
	if err != nil || goModPath == "" {
		return nil
	}
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return nil
	}
	projectVersion, hasLocalReplace := parseGoSXRequirement(string(data))
	if projectVersion == "" {
		return nil
	}
	return versionSkewError("v"+gosx.Version, projectVersion, hasLocalReplace)
}

// projectGoModPath resolves the go.mod that governs projectDir, the same way
// moduleInfo does for modules.go generation. An empty result means projectDir
// is not inside a module, which is not this check's problem to report.
func projectGoModPath(projectDir string) (string, error) {
	cmd := exec.Command("go", "env", "GOMOD")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" || path == os.DevNull {
		return "", nil
	}
	return path, nil
}

// parseGoSXRequirement scans go.mod text for the m31labs.dev/gosx require
// version and whether a local-path replace directive redirects it. It is a
// minimal scanner rather than golang.org/x/mod/modfile because the module does
// not otherwise depend on that package.
func parseGoSXRequirement(goMod string) (version string, hasLocalReplace bool) {
	scanner := bufio.NewScanner(strings.NewReader(goMod))
	inRequireBlock := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		switch {
		case line == "require (":
			inRequireBlock = true
		case inRequireBlock && line == ")":
			inRequireBlock = false
		case inRequireBlock:
			if v, ok := matchGoSXRequireFields(line); ok {
				version = v
			}
		case strings.HasPrefix(line, "require "):
			if v, ok := matchGoSXRequireFields(strings.TrimPrefix(line, "require ")); ok {
				version = v
			}
		case strings.HasPrefix(line, "replace "):
			if matchGoSXLocalReplaceFields(strings.TrimPrefix(line, "replace ")) {
				hasLocalReplace = true
			}
		}
	}
	return version, hasLocalReplace
}

// matchGoSXRequireFields reads one require-line body ("m31labs.dev/gosx
// v0.31.4" or "m31labs.dev/gosx v0.31.4 // indirect") and returns the pinned
// version.
func matchGoSXRequireFields(rest string) (string, bool) {
	fields := strings.Fields(rest)
	if len(fields) < 2 || fields[0] != "m31labs.dev/gosx" {
		return "", false
	}
	return fields[1], true
}

// matchGoSXLocalReplaceFields reports whether a replace-line body ("m31labs.dev/gosx
// [oldver] => target [newver]") redirects m31labs.dev/gosx to a local
// filesystem path rather than another module version.
func matchGoSXLocalReplaceFields(rest string) bool {
	fields := strings.Fields(rest)
	if len(fields) == 0 || fields[0] != "m31labs.dev/gosx" {
		return false
	}
	arrow := -1
	for i, field := range fields {
		if field == "=>" {
			arrow = i
			break
		}
	}
	if arrow < 0 || arrow+1 >= len(fields) {
		return false
	}
	target := fields[arrow+1]
	if strings.Contains(target, "@") {
		return false
	}
	return strings.HasPrefix(target, ".") || filepath.IsAbs(target)
}

// versionSkewError is the pure decision behind checkVersionSkew: given the
// CLI's own version and the project's pinned m31labs.dev/gosx version, decide
// whether to fail and with what message. Kept separate from filesystem and
// process access so it is testable on its own.
func versionSkewError(cliVersion, projectVersion string, hasLocalReplace bool) error {
	if hasLocalReplace || projectVersion == "" || cliVersion == projectVersion {
		return nil
	}
	return fmt.Errorf(
		"gosx %s cannot operate on a project pinned to m31labs.dev/gosx %s. Run: go install m31labs.dev/gosx/cmd/gosx@%s",
		cliVersion, projectVersion, projectVersion,
	)
}
