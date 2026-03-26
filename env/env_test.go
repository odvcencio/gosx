package env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDirLoadsModeSpecificFiles(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = os.Unsetenv("GOSX_TEST_APP_NAME")
		_ = os.Unsetenv("GOSX_TEST_SHARED")
		_ = os.Unsetenv("GOSX_TEST_COLOR")
	})
	writeEnvFile(t, dir, ".env", "GOSX_TEST_APP_NAME=base\nGOSX_TEST_SHARED=base\n")
	writeEnvFile(t, dir, ".env.local", "GOSX_TEST_APP_NAME=local\n")
	writeEnvFile(t, dir, ".env.production", "GOSX_TEST_APP_NAME=prod\nGOSX_TEST_COLOR=blue\n")
	writeEnvFile(t, dir, ".env.production.local", "GOSX_TEST_COLOR=green\n")

	if err := LoadDir(dir, "production"); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("GOSX_TEST_APP_NAME"); got != "prod" {
		t.Fatalf("expected mode-specific APP_NAME, got %q", got)
	}
	if got := os.Getenv("GOSX_TEST_SHARED"); got != "base" {
		t.Fatalf("expected shared value, got %q", got)
	}
	if got := os.Getenv("GOSX_TEST_COLOR"); got != "green" {
		t.Fatalf("expected .env.production.local override, got %q", got)
	}
}

func TestLoadDirDoesNotOverrideExistingEnvByDefault(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, ".env", "APP_NAME=file\n")
	t.Setenv("APP_NAME", "shell")

	if err := LoadDir(dir, "development"); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("APP_NAME"); got != "shell" {
		t.Fatalf("expected pre-existing env to win, got %q", got)
	}
}

func TestLoadDirWithOptionsCanOverrideExistingEnv(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, ".env", "APP_NAME=file\n")
	t.Setenv("APP_NAME", "shell")

	if err := LoadDirWithOptions(dir, Options{Mode: "development", Override: true}); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("APP_NAME"); got != "file" {
		t.Fatalf("expected file to override env, got %q", got)
	}
}

func TestLoadDirParsesQuotedValues(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = os.Unsetenv("GOSX_TEST_DOUBLE")
		_ = os.Unsetenv("GOSX_TEST_SINGLE")
	})
	writeEnvFile(t, dir, ".env", "GOSX_TEST_DOUBLE=\"hello\\nworld\"\nGOSX_TEST_SINGLE='literal value'\n")

	if err := LoadDir(dir, "development"); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("GOSX_TEST_DOUBLE"); got != "hello\nworld" {
		t.Fatalf("unexpected double-quoted value %q", got)
	}
	if got := os.Getenv("GOSX_TEST_SINGLE"); got != "literal value" {
		t.Fatalf("unexpected single-quoted value %q", got)
	}
}

func TestLoadDirCanDiscoverModeFromBaseEnv(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = os.Unsetenv("GOSX_ENV")
		_ = os.Unsetenv("GOSX_TEST_MODE_VALUE")
	})
	writeEnvFile(t, dir, ".env", "GOSX_ENV=production\n")
	writeEnvFile(t, dir, ".env.production", "GOSX_TEST_MODE_VALUE=from-mode\n")

	if err := LoadDir(dir, ""); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("GOSX_TEST_MODE_VALUE"); got != "from-mode" {
		t.Fatalf("expected mode-specific file to load, got %q", got)
	}
}

func TestModePrefersGoSXEnv(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	t.Setenv("GOSX_ENV", "staging")

	if got := Mode(); got != "staging" {
		t.Fatalf("expected GOSX_ENV to win, got %q", got)
	}
}

func writeEnvFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
