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
	t.Setenv("GIT_DIR", "")
	t.Setenv("GIT_WORK_TREE", "")
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
}

func TestInstallerBuildPreservesRealRepositoryVCS(t *testing.T) {
	t.Setenv("GIT_DIR", "")
	t.Setenv("GIT_WORK_TREE", "")
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
