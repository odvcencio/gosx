package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageDeploymentBundleCopiesRuntimeDirsAndWritesArtifacts(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")

	mustWriteFile(t, filepath.Join(projectDir, "app", "page.gsx"), "package app\n")
	mustWriteFile(t, filepath.Join(projectDir, "public", "styles.css"), "body {}\n")
	mustWriteFile(t, filepath.Join(projectDir, ".env.example"), "PORT=8080\n")
	serverBinaryPath := filepath.Join(distDir, "server", "app")
	mustWriteFile(t, serverBinaryPath, "binary")

	if err := stageDeploymentBundle(projectDir, distDir, true, serverBinaryPath); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"app/page.gsx",
		"public/styles.css",
		".env.example",
		"README.md",
		"run.sh",
	} {
		if _, err := os.Stat(filepath.Join(distDir, rel)); err != nil {
			t.Fatalf("expected %s in bundle: %v", rel, err)
		}
	}

	runScript := readFile(t, filepath.Join(distDir, "run.sh"))
	for _, snippet := range []string{
		`export GOSX_APP_ROOT="${GOSX_APP_ROOT:-$DIR}"`,
		`exec "$DIR/server/app" "$@"`,
	} {
		if !strings.Contains(runScript, snippet) {
			t.Fatalf("expected %q in run.sh, got %q", snippet, runScript)
		}
	}

	readme := readFile(t, filepath.Join(distDir, "README.md"))
	if !strings.Contains(readme, "deployable GoSX bundle") {
		t.Fatalf("unexpected dist README: %q", readme)
	}
}

func TestWriteBuildReadmeWithoutServerBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "README.md")
	if err := writeBuildReadme(path, false); err != nil {
		t.Fatal(err)
	}
	readme := readFile(t, path)
	if strings.Contains(readme, "run.sh") {
		t.Fatalf("did not expect launch-script instructions in %q", readme)
	}
	if !strings.Contains(readme, "`assets/` contains immutable hashed runtime") {
		t.Fatalf("unexpected readme content %q", readme)
	}
}

func TestCSSAssetBaseNameUsesRelativePath(t *testing.T) {
	cases := map[string]string{
		"app/page.css":               "app_page",
		"app/docs/page.css":          "app_docs_page",
		"components/hero-banner.css": "components_hero_banner",
	}
	for input, want := range cases {
		if got := cssAssetBaseName(input); got != want {
			t.Fatalf("%s: expected %q, got %q", input, want, got)
		}
	}
}

func TestProjectBuildHooksLoadAndRun(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{
  "build": {
    "hooks": {
      "pre": ["printf pre > pre.txt"],
      "post": ["printf post > post.txt"]
    }
  }
}`)

	cfg, err := loadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Build.Hooks.Pre) != 1 || len(cfg.Build.Hooks.Post) != 1 {
		t.Fatalf("unexpected build hooks config %#v", cfg)
	}

	if err := runBuildHookCommands(dir, "pre-build", cfg.Build.Hooks.Pre); err != nil {
		t.Fatal(err)
	}
	if err := runBuildHookCommands(dir, "post-build", cfg.Build.Hooks.Post); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(dir, "pre.txt")); got != "pre" {
		t.Fatalf("expected pre hook output, got %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "post.txt")); got != "post" {
		t.Fatalf("expected post hook output, got %q", got)
	}
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}
