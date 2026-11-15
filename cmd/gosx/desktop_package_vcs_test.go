package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstallerBuildRecognizesOnlyEmptyInvalidGitPlaceholder(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable; Go already skips automatic VCS stamping")
	}
	unsetGitLocation(t)
	t.Setenv("GOFLAGS", "")
	root := t.TempDir()
	marker := filepath.Join(root, ".git")
	if err := os.Mkdir(marker, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(marker); err != nil {
			t.Errorf("remove empty test placeholder: %v", err)
		}
	})
	work := filepath.Join(root, "generated")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if !installerBuildHasEmptyGitPlaceholder(work) {
		t.Fatal("empty invalid Git placeholder was not recognized")
	}

	t.Run("explicit buildvcs", func(t *testing.T) {
		t.Setenv("GOFLAGS", "-buildvcs=true")
		if installerBuildHasEmptyGitPlaceholder(work) {
			t.Fatal("explicit VCS policy must not be overridden")
		}
	})
	t.Run("explicit Git metadata", func(t *testing.T) {
		t.Setenv("GIT_DIR", filepath.Join(root, "chosen-repository"))
		if installerBuildHasEmptyGitPlaceholder(work) {
			t.Fatal("explicit Git metadata must not be overridden")
		}
	})
	t.Run("nonempty metadata", func(t *testing.T) {
		file := filepath.Join(marker, "config")
		if err := os.WriteFile(file, []byte("metadata"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(file); err != nil {
				t.Errorf("remove test metadata: %v", err)
			}
		})
		if installerBuildHasEmptyGitPlaceholder(work) {
			t.Fatal("nonempty Git metadata must not be bypassed")
		}
	})
	t.Run("linked worktree below the placeholder", func(t *testing.T) {
		// Go counts only a .git directory as a repository root, so it skips
		// the worktree's .git file, stops at the placeholder and fails there.
		lane := filepath.Join(root, "lane")
		build := filepath.Join(lane, "cmd", "app")
		if err := os.MkdirAll(build, 0o755); err != nil {
			t.Fatal(err)
		}
		pointer := "gitdir: " + filepath.Join(root, "repository", ".git", "worktrees", "lane") + "\n"
		if err := os.WriteFile(filepath.Join(lane, ".git"), []byte(pointer), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, ok := goBuildVCSRoot(build); !ok || got != filepath.Dir(marker) {
			t.Fatalf("goBuildVCSRoot(%q) = %q, %v; want the placeholder's folder %q", build, got, ok, filepath.Dir(marker))
		}
		if !installerBuildHasEmptyGitPlaceholder(build) {
			t.Fatal("a placeholder above a linked worktree was not recognized")
		}
	})
	t.Run("nearer Git repository", func(t *testing.T) {
		repository := filepath.Join(root, "repository-checkout")
		initCmd := exec.Command("git", "init", "-q", repository)
		if output, err := initCmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, output)
		}
		build := filepath.Join(repository, "cmd")
		if err := os.Mkdir(build, 0o755); err != nil {
			t.Fatal(err)
		}
		if installerBuildHasEmptyGitPlaceholder(build) {
			t.Fatal("a nearer real repository must retain VCS stamping")
		}
	})
	t.Run("nearer other version control", func(t *testing.T) {
		mercurial := filepath.Join(root, "mercurial")
		if err := os.MkdirAll(filepath.Join(mercurial, ".hg"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, ok := goBuildVCSRoot(mercurial); ok {
			t.Fatal("Go uses the nearer Mercurial root, not the Git placeholder")
		}
		if installerBuildHasEmptyGitPlaceholder(mercurial) {
			t.Fatal("a nearer non-Git repository must keep Go's behavior")
		}
	})
}

// unsetGitLocation removes GIT_DIR and GIT_WORK_TREE for the test. Setting them
// empty is not the same: Git refuses an empty GIT_DIR, so every Git command in
// the test would fail and the checks that depend on Git would pass vacuously.
func unsetGitLocation(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE"} {
		t.Setenv(key, "") // restores the original value after the test
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInstallerBuildPreservesRealRepositoryVCS(t *testing.T) {
	unsetGitLocation(t)
	t.Setenv("GOFLAGS", "")
	if err := exec.Command("git", "rev-parse", "--git-dir").Run(); err != nil {
		t.Skipf("test is not running in a Git checkout: %v", err)
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if installerBuildHasEmptyGitPlaceholder(dir) {
		t.Fatal("a verified repository must retain VCS stamping")
	}
}
