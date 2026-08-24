package bundlepolicy

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestValidateConfigRejectsUnsafeEntriesAndConflicts(t *testing.T) {
	cfg := Config{
		Allow:       []string{"app/data", "app/data"},
		AllowPublic: []string{"public/state.db", "public/state.db"},
		Exclude:     []string{"public", "public/state.db"},
	}
	diagnostics := ValidateConfig(cfg)
	if len(diagnostics) < 3 {
		t.Fatalf("ValidateConfig returned %d diagnostics, want aggregated failures: %v", len(diagnostics), diagnostics)
	}
	message := diagnostics.Error()
	for _, want := range []string{
		"duplicate build.bundle.allow entry",
		"duplicate build.bundle.allowPublic entry",
		"build.bundle.allowPublic conflicts with build.bundle.exclude",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("diagnostics missing %q: %s", want, message)
		}
	}
	for _, raw := range []string{"", "../secret", "/tmp/file", "app\\secret", "other/file", "app/./file", "app/../file"} {
		if got := ValidateConfig(Config{Allow: []string{raw}}); len(got) == 0 {
			t.Errorf("ValidateConfig accepted unsafe allow %q", raw)
		}
	}
	if got := ValidateConfig(Config{AllowPublic: []string{"public"}}); len(got) == 0 {
		t.Error("ValidateConfig accepted public root as allowPublic")
	}
}

func TestValidateTreeAggregatesSecretsStateSymlinksAndSpecialFiles(t *testing.T) {
	project := t.TempDir()
	for _, root := range []string{"app", "content", "public"} {
		if err := os.MkdirAll(filepath.Join(project, root), 0755); err != nil {
			t.Fatal(err)
		}
	}
	mustBundleFile(t, filepath.Join(project, "app", ".env.example"), "secret")
	mustBundleFile(t, filepath.Join(project, "content", "records.sqlite"), "state")
	mustBundleFile(t, filepath.Join(project, "public", "token.pem"), "secret")
	if err := os.Symlink(filepath.Join(project, "public", "token.pem"), filepath.Join(project, "public", "linked.css")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(project, "app", "events.sock"), 0600); err != nil {
		t.Fatal(err)
	}
	diagnostics := ValidateProject(project, Config{Exclude: []string{"app/.env.example", "public/linked.css", "app/events.sock"}})
	if len(diagnostics) < 5 {
		t.Fatalf("ValidateProject returned %d diagnostics, want all hard denials: %s", len(diagnostics), diagnostics.Error())
	}
	message := diagnostics.Error()
	for _, want := range []string{"app/.env.example", "content/records.sqlite", "public/token.pem", "public/linked.css", "app/events.sock"} {
		if !strings.Contains(message, want) {
			t.Errorf("aggregated diagnostics missing %q: %s", want, message)
		}
	}
}

func TestAllowPublicExactMutableFileAndAllowImmutableServerData(t *testing.T) {
	project := t.TempDir()
	for _, root := range []string{"app", "content", "public"} {
		if err := os.MkdirAll(filepath.Join(project, root), 0755); err != nil {
			t.Fatal(err)
		}
	}
	mustBundleFile(t, filepath.Join(project, "app", "catalog.json"), "{}")
	mustBundleFile(t, filepath.Join(project, "app", "cache.db"), "db")
	mustBundleFile(t, filepath.Join(project, "public", "public.db"), "db")
	cfg := Config{
		Allow:       []string{"app/catalog.json", "app/cache.db"},
		AllowPublic: []string{"public/public.db"},
	}
	if diagnostics := ValidateProject(project, cfg); !diagnostics.Empty() {
		t.Fatalf("explicit immutable/public allowances rejected: %s", diagnostics.Error())
	}
	dist := filepath.Join(t.TempDir(), "dist")
	if err := CopyTree(filepath.Join(project, "app"), filepath.Join(dist, "app"), RootApp, cfg); err != nil {
		t.Fatal(err)
	}
	if err := CopyTree(filepath.Join(project, "public"), filepath.Join(dist, "public"), RootPublic, cfg); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"app/catalog.json", "app/cache.db", "public/public.db"} {
		if _, err := os.Stat(filepath.Join(dist, filepath.FromSlash(rel))); err != nil {
			t.Errorf("allowed file %s was not staged: %v", rel, err)
		}
	}
	if path, ok := PublicPath(filepath.Join(dist, "public"), "public.db", cfg.AllowPublic, nil); !ok || path == "" {
		t.Error("allowPublic did not expose the exact public database file")
	}
	if _, ok := PublicPath(filepath.Join(dist, "public"), "../app/catalog.json", cfg.AllowPublic, nil); ok {
		t.Error("public path traversal was accepted")
	}
}

func TestAllowPublicRequiresExistingRegularFile(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "public", "state.db"), 0755); err != nil {
		t.Fatal(err)
	}
	diagnostics := ValidateProject(project, Config{AllowPublic: []string{"public/missing.db", "public/state.db"}})
	message := diagnostics.Error()
	if !strings.Contains(message, "public/missing.db") || !strings.Contains(message, "public/state.db") {
		t.Fatalf("missing exact-file allowance diagnostics: %s", message)
	}
}

func TestCopyTreeRejectsSymlinkWithoutDereference(t *testing.T) {
	project := t.TempDir()
	src := filepath.Join(project, "public")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	mustBundleFile(t, filepath.Join(src, "real.css"), "body{}")
	if err := os.Symlink(filepath.Join(src, "real.css"), filepath.Join(src, "alias.css")); err != nil {
		t.Fatal(err)
	}
	err := CopyTree(src, filepath.Join(t.TempDir(), "out"), RootPublic, Config{})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("CopyTree error = %v, want symlink rejection", err)
	}
}

func TestPublicPathRejectsSecretsSymlinksAndMutableStateByDefault(t *testing.T) {
	public := t.TempDir()
	mustBundleFile(t, filepath.Join(public, "site.css"), "body{}")
	mustBundleFile(t, filepath.Join(public, ".env"), "TOKEN=secret")
	mustBundleFile(t, filepath.Join(public, "state.db"), "db")
	if err := os.Symlink(filepath.Join(public, "site.css"), filepath.Join(public, "alias.css")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env", "state.db", "alias.css"} {
		if _, ok := PublicPath(public, name, nil, nil); ok {
			t.Errorf("PublicPath served denied %s", name)
		}
	}
	if _, ok := PublicPath(public, "site.css", nil, nil); !ok {
		t.Error("PublicPath denied ordinary asset")
	}
}

func TestDecodePolicyFileIsStrict(t *testing.T) {
	if _, err := DecodePolicyFile([]byte(`{"allowPublic":["public/site.css"],"unknown":true}`)); err == nil {
		t.Error("DecodePolicyFile accepted unknown field")
	}
	if _, err := DecodePolicyFile([]byte(`{"allowPublic":["public/state.db"],"exclude":["public/state.db"]}`)); err == nil {
		t.Error("DecodePolicyFile accepted conflicting policy")
	}
}

func TestDiagnosticsErrorIsSortedAndActionable(t *testing.T) {
	message := (Diagnostics{
		{Path: "public/z", Message: "z"},
		{Path: "app/a", Message: "a"},
	}).Error()
	if strings.Index(message, "app/a") > strings.Index(message, "public/z") {
		t.Fatalf("diagnostics were not sorted: %s", message)
	}
	if !strings.Contains(message, "secrets and symlinks can never be allowed") {
		t.Fatalf("diagnostics omitted resolution guidance: %s", message)
	}
}

func mustBundleFile(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}
