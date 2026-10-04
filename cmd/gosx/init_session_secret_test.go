package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/localapp"
)

func TestRunInitSessionSecrets(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("builds and starts scaffold servers in subprocesses; covered by test-cli")
	}
	for _, template := range []string{initTemplateApp, initTemplateDocs} {
		t.Run(template, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "scaffold")
			if err := RunInit(dir, "example.com/scaffold", template); err != nil {
				t.Fatal(err)
			}
			ignore := "\n" + readFile(t, filepath.Join(dir, ".gitignore"))
			if !strings.Contains(ignore, "\n.env\n") || !strings.Contains(ignore, "\n.env.*\n") {
				t.Fatal("scaffold must ignore .env and its variants")
			}
			addLocalGoSXReplace(t, dir)
			tidyModule(t, dir)
			mustWriteFile(t, filepath.Join(dir, "session_secret_test.go"), scaffoldSessionSecretTests)
			goTestModule(t, dir)
			binary := filepath.Join(t.TempDir(), "app")
			if built, err := buildServerBinaryIfPresent(dir, binary); err != nil || !built {
				t.Fatalf("build scaffold: built=%t err=%v", built, err)
			}
			localEnv, err := localapp.Environment(nil, "0")
			if err != nil {
				t.Fatal(err)
			}
			var productionSecret string
			for _, entry := range localEnv {
				if value, ok := strings.CutPrefix(entry, "SESSION_SECRET="); ok {
					productionSecret = value
				}
			}
			for _, tc := range []struct{ name, mode, secret, dev string }{
				{"missing", "production", "", ""},
				{"env placeholder", "production", "change-me-in-production", ""},
				{"app placeholder", "production", "gosx-app-session-secret", ""},
				{"docs placeholder", "production", "gosx-docs-session-secret", ""},
				{"whitespace", "production", "   ", ""},
				{"short in dev", "development", "short", ""},
				{"staging", "staging", "", ""},
				{"unset mode", "", "", ""},
				{"production overrides dev flag", "production", "", "1"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, binary)
					cmd.Dir = dir
					cmd.Env = scaffoldSecretEnv(dir, tc.mode, tc.secret, tc.dev, "0")
					output, err := cmd.CombinedOutput()
					if err == nil || ctx.Err() != nil || !strings.Contains(string(output), "SESSION_SECRET") {
						t.Fatalf("scaffold should refuse startup: err=%v output=%s", err, output)
					}
				})
			}
			for _, tc := range []struct{ name, mode, secret, dev string }{
				{"production", "production", productionSecret, ""},
				{"development", "development", "change-me-in-production", ""},
				{"dev flag", "", "", "1"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					port, err := pickFreePort()
					if err != nil {
						t.Fatal(err)
					}
					cmd := exec.Command(binary)
					cmd.Dir = dir
					cmd.Env = scaffoldSecretEnv(dir, tc.mode, tc.secret, tc.dev, port)
					logPath := filepath.Join(t.TempDir(), "server.log")
					logFile, err := os.Create(logPath)
					if err != nil {
						t.Fatal(err)
					}
					defer logFile.Close()
					cmd.Stderr = logFile
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
					if err := waitForAppReady("http://127.0.0.1:"+port, 20*time.Second); err != nil {
						t.Fatalf("serve scaffold: %v\n%s", err, readFile(t, logPath))
					}
					logged := strings.Contains(readFile(t, logPath), "random per-process development session secret")
					if logged != (tc.mode != "production") {
						t.Fatalf("development secret log present=%t", logged)
					}
				})
			}
		})
	}
}

func TestRunInitProductionExportUsesDisposableSecret(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("builds scaffold servers in subprocesses; covered by test-cli")
	}
	t.Setenv("GOSX_ENV", "production")
	t.Setenv("SESSION_SECRET", "change-me-in-production")
	for _, template := range []string{initTemplateApp, initTemplateDocs} {
		t.Run(template, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "scaffold")
			if err := RunInit(dir, "example.com/scaffold", template); err != nil {
				t.Fatal(err)
			}
			addLocalGoSXReplace(t, dir)
			tidyModule(t, dir)
			if err := RunExport(dir); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "dist", "static", "index.html")); err != nil {
				t.Fatal(err)
			}
			if os.Getenv("SESSION_SECRET") != "change-me-in-production" {
				t.Fatal("export replaced the parent session secret")
			}
		})
	}
}

func scaffoldSecretEnv(dir, mode, secret, dev, port string) []string {
	var filtered []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "SESSION_SECRET", "GOSX_ENV", "GOSX_DEV", "GOSX_APP_ROOT", "PORT", "PUBLIC_URL", "GOWORK":
		default:
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, "SESSION_SECRET="+secret, "GOSX_ENV="+mode, "GOSX_DEV="+dev,
		"GOSX_APP_ROOT="+dir, "PORT=127.0.0.1:"+port, "PUBLIC_URL=http://127.0.0.1:"+port, "GOWORK=off")
}

const scaffoldSessionSecretTests = `package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDevelopmentSecretsAreRandom(t *testing.T) {
	t.Setenv("GOSX_ENV", "development")
	t.Setenv("SESSION_SECRET", "")
	one, err := sessionSecret()
	if err != nil { t.Fatal(err) }
	two, err := sessionSecret()
	if err != nil { t.Fatal(err) }
	decoded, err := base64.RawURLEncoding.DecodeString(one)
	if err != nil || len(decoded) != 32 || one == two {
		t.Fatal("development secrets must contain 32 random bytes and differ between generations")
	}
}

func TestConfiguredSecretIsPreserved(t *testing.T) {
	t.Setenv("GOSX_ENV", "production")
	configured := strings.Repeat("0123456789abcdef", 4)
	t.Setenv("SESSION_SECRET", configured)
	got, err := sessionSecret()
	if err != nil || got != configured { t.Fatalf("configured secret changed: %v", err) }
}
`
