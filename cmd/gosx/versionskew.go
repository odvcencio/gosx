package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"m31labs.dev/gosx"
)

// gosxModulePath is the module path this CLI ships as, and the one
// checkVersionSkew resolves against every project it inspects.
const gosxModulePath = "m31labs.dev/gosx"

// skipVersionCheckEnv, set to any non-empty value, bypasses checkVersionSkew
// entirely. It exists because the check runs a real toolchain command and can
// be wrong for a setup this package has not anticipated; the escape hatch
// lets a build proceed instead of blocking on a diagnostic.
const skipVersionCheckEnv = "GOSX_SKIP_VERSION_CHECK"

// checkVersionSkew fails fast when a project's module graph resolves a
// different m31labs.dev/gosx release than this CLI. Left unchecked,
// resolveGoSXModuleRoot resolves client sources from the module cache path of
// the pinned release, and a newer CLI fails deep inside that path with an
// obscure missing-file error instead of a version diagnostic.
func checkVersionSkew(projectDir string) error {
	if os.Getenv(skipVersionCheckEnv) != "" {
		return nil
	}
	projectVersion, hasLocalReplace, ok := resolveProjectGoSXVersion(projectDir)
	if !ok {
		return nil
	}
	return versionSkewError("v"+gosx.Version, projectVersion, hasLocalReplace)
}

// goListModule mirrors the subset of `go list -m -json` fields this check
// needs. Replace is nil unless a replace directive governs the module; when
// set, its own Version and Dir describe the replacement, not the original.
type goListModule struct {
	Version string
	Dir     string
	Replace *goListModule
}

// resolveProjectGoSXVersion asks the Go toolchain how projectDir's module
// graph resolves m31labs.dev/gosx, rather than scanning go.mod text by hand.
// `go list -m -json` already understands require and replace blocks in every
// formatting a hand-rolled scanner gets wrong (block form, space-free
// "require(", trailing comments), and it is the one place that correctly
// resolves go.work workspaces, where `go env GOMOD` still names a member
// module's go.mod even though a `use` directive overrides the version.
//
// ok is false whenever the check has nothing useful to say: m31labs.dev/gosx
// is not required, projectDir is not inside a module, or the toolchain
// invocation itself failed (offline, no go on PATH, malformed output, and so
// on). None of those are grounds to block a build over a diagnostic.
func resolveProjectGoSXVersion(projectDir string) (version string, localReplace bool, ok bool) {
	cmd := exec.Command("go", "list", "-m", "-json", gosxModulePath)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return "", false, false
	}
	var mod goListModule
	if err := json.Unmarshal(bytes.TrimSpace(out), &mod); err != nil {
		return "", false, false
	}
	return interpretGoListModule(mod)
}

// interpretGoListModule is the pure decision behind resolveProjectGoSXVersion:
// given one `go list -m -json` record for m31labs.dev/gosx, it decides the
// version to compare against the CLI's own, and whether local disk sources
// make that comparison moot. Kept separate from process execution so it is
// testable without shelling out.
func interpretGoListModule(mod goListModule) (version string, localReplace bool, ok bool) {
	if mod.Replace != nil {
		if mod.Replace.Version == "" {
			// A path replace: the target is a directory on disk, not a
			// tagged version, so the require line never governs the build.
			return "", true, true
		}
		// A module-version replace: the replaced version is what the build
		// actually uses, not the original require line.
		return mod.Replace.Version, false, true
	}
	if mod.Version == "" {
		if mod.Dir != "" {
			// Workspace mode: m31labs.dev/gosx resolved to a `use`d local
			// module rather than a required version.
			return "", true, true
		}
		// Neither a version nor a local dir: nothing this check understands.
		return "", false, false
	}
	return mod.Version, false, true
}

// versionSkewError is the pure decision behind checkVersionSkew: given the
// CLI's own version and the project's resolved m31labs.dev/gosx version,
// decide whether to fail and with what message. Kept separate from
// filesystem and process access so it is testable on its own.
func versionSkewError(cliVersion, projectVersion string, hasLocalReplace bool) error {
	if hasLocalReplace || projectVersion == "" || cliVersion == projectVersion {
		return nil
	}
	return fmt.Errorf(
		"gosx %s cannot operate on a project pinned to m31labs.dev/gosx %s. Run: go install m31labs.dev/gosx/cmd/gosx@%s, or set %s=1 to override",
		cliVersion, projectVersion, projectVersion, skipVersionCheckEnv,
	)
}
