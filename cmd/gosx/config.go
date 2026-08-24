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
	Hooks  projectBuildHooks   `json:"hooks"`
	Bundle bundlepolicy.Config `json:"bundle"`
}

type projectBuildHooks struct {
	Pre  []string `json:"pre"`
	Post []string `json:"post"`
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
