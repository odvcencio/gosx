package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type projectConfig struct {
	Build projectBuildConfig `json:"build"`
}

type projectBuildConfig struct {
	Hooks projectBuildHooks `json:"hooks"`
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
	if err := json.Unmarshal(data, &cfg); err != nil {
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
