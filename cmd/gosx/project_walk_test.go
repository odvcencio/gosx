package main

import "testing"

func TestShouldSkipProjectDirSkipsWorktrees(t *testing.T) {
	for _, name := range []string{".git", ".tiller", ".worktrees", "build", "dist", "node_modules", ".tmp-x"} {
		if !shouldSkipProjectDir(name) {
			t.Fatalf("%q must be skipped", name)
		}
	}
	for _, name := range []string{"app", "public", "content", "modules"} {
		if shouldSkipProjectDir(name) {
			t.Fatalf("%q must not be skipped", name)
		}
	}
}
