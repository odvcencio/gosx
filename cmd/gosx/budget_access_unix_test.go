//go:build unix

package main

import (
	"os"
	"syscall"
	"testing"
)

func TestBudgetSavePreservesGroup(t *testing.T) {
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group == os.Getgid() {
			continue
		}
		dir, path := budgetCommandFixture(t)
		if err := os.Chown(path, os.Getuid(), group); err != nil {
			continue
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if int(before.Sys().(*syscall.Stat_t).Gid) != group {
			continue
		}
		if code, _, diagnostic := runBudgetTest("derive", "--budget", path, "--root", dir, "--write"); code != 0 {
			t.Fatal(code, diagnostic)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		a, b := before.Sys().(*syscall.Stat_t), after.Sys().(*syscall.Stat_t)
		if a.Uid != b.Uid || a.Gid != b.Gid {
			t.Fatal("save changed ownership")
		}
		return
	}
	t.Skip("no writable supplementary-group ownership on temporary filesystem")
}
