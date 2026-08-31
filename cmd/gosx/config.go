package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/internal/bundlepolicy"
)

type projectConfig struct {
	Build projectBuildConfig `json:"build"`
}

type projectBuildConfig struct {
	Hooks   projectBuildHooks   `json:"hooks"`
	Bundle  bundlepolicy.Config `json:"bundle"`
	Runtime projectBuildRuntime `json:"runtime"`
}

type projectBuildHooks struct {
	Pre  []string `json:"pre"`
	Post []string `json:"post"`
}

// projectBuildRuntime is build.runtime in gosx.config.json. It controls two
// independent runtime-asset emission concerns that bundlepolicy does not
// cover: whether `gosx build` writes .js.map source-map sidecars, and which
// optional runtime feature chunks it skips entirely. Both default to today's
// behavior — sourceMaps true, nothing excluded — so a project with no
// gosx.config.json, or one with no build.runtime block, builds byte-
// identical output to before this field existed.
type projectBuildRuntime struct {
	// SourceMaps is a pointer so an absent key defaults to true (emit .map
	// sidecars) while an explicit `"sourceMaps": false` opts out.
	SourceMaps *bool `json:"sourceMaps"`
	// Exclude names runtime asset roles to drop from the build entirely, so
	// they never appear in dist/assets/runtime or dist/build.json. Each
	// entry must be one of runtimeAssetRoles() (cmd/gosx/size.go). It never
	// affects WASM compilation or wasm_exec.js emission — a future
	// build.runtime.features allow-list, not this field, owns that.
	Exclude []string `json:"exclude"`
}

// sourceMapsEnabled reports whether gosx build should write .js.map sidecars
// for the runtime JS payloads. Absent config keeps today's default: true.
func (r projectBuildRuntime) sourceMapsEnabled() bool {
	return r.SourceMaps == nil || *r.SourceMaps
}

// excludesRole reports whether build.runtime.exclude names role. A blank
// role (the core loader chain: bootstrap.js, bootstrap-lite.js,
// bootstrap-runtime.js, patch.js) is never excludable and always reports
// false.
func (r projectBuildRuntime) excludesRole(role string) bool {
	if role == "" {
		return false
	}
	for _, excluded := range r.Exclude {
		if excluded == role {
			return true
		}
	}
	return false
}

// validateRuntimeExcludeRoles rejects build.runtime.exclude entries that are
// blank, unknown, or duplicated. The valid set is runtimeAssetRoles(), the
// same runtime-asset role vocabulary cmd/gosx/size.go already reports.
func validateRuntimeExcludeRoles(exclude []string) error {
	valid := runtimeAssetRoleSet()
	seen := make(map[string]struct{}, len(exclude))
	for _, raw := range exclude {
		name := strings.TrimSpace(raw)
		if name == "" {
			return fmt.Errorf("build.runtime.exclude entries cannot be empty")
		}
		if _, ok := valid[name]; !ok {
			return fmt.Errorf("build.runtime.exclude: unknown runtime asset role %q (valid roles: %s)", name, strings.Join(runtimeAssetRoles(), ", "))
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("build.runtime.exclude: duplicate role %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func loadProjectConfig(dir string) (projectConfig, error) {
	path := filepath.Join(dir, "gosx.config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return projectConfig{}, nil
		}
		return projectConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg projectConfig
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return projectConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return projectConfig{}, fmt.Errorf("decode %s: multiple JSON values are not allowed", path)
		}
		return projectConfig{}, fmt.Errorf("decode %s: trailing data: %w", path, err)
	}
	if diagnostics := bundlepolicy.ValidateConfig(cfg.Build.Bundle); !diagnostics.Empty() {
		return projectConfig{}, fmt.Errorf("decode %s: %w", path, diagnostics)
	}
	if err := validateRuntimeExcludeRoles(cfg.Build.Runtime.Exclude); err != nil {
		return projectConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return cfg, nil
}

func runBuildHookCommands(dir string, phase string, commands []string) error {
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		cmd := exec.Command("sh", "-lc", command)
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s hook %q: %w", phase, command, err)
		}
	}
	return nil
}

func writeBundlePolicySidecar(root string, cfg bundlepolicy.Config) error {
	data, err := bundlepolicy.EncodePolicyFile(bundlepolicy.PolicyFileFor(cfg))
	if err != nil {
		return fmt.Errorf("encode bundle policy: %w", err)
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("create bundle policy root: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bundle-policy.json"), data, 0644); err != nil {
		return fmt.Errorf("write bundle policy: %w", err)
	}
	return nil
}

func printBundlePolicyWarnings(cfg bundlepolicy.Config) {
	for _, rel := range cfg.Allow {
		fmt.Fprintf(os.Stderr, "GoSX bundle: WARNING allow keeps immutable server data %s\n", rel)
	}
	for _, rel := range cfg.AllowPublic {
		fmt.Fprintf(os.Stderr, "GoSX bundle: WARNING allowPublic exposes %s anonymously; verify this is intentional\n", rel)
	}
	for _, rel := range cfg.Exclude {
		fmt.Fprintf(os.Stderr, "GoSX bundle: WARNING exclude omits %s from the artifact\n", rel)
	}
}
