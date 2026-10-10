package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
	"m31labs.dev/gosx/perf/budget"
)

func setBudgetTestXattr(t *testing.T, path string, value []byte) {
	t.Helper()
	err := unix.Setxattr(path, "user.budget-test", value, 0)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EPERM) {
		t.Skip("temporary filesystem does not support user extended attributes")
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestBudgetSavePreservesExtendedAttributes(t *testing.T) {
	dir, path := budgetCommandFixture(t)
	want := []byte("keep this metadata")
	setBudgetTestXattr(t, path, want)
	if code, _, diagnostic := runBudgetTest("derive", "--budget", path, "--root", dir, "--write"); code != 0 {
		t.Fatal(code, diagnostic)
	}
	data := make([]byte, 100)
	n, err := unix.Getxattr(path, "user.budget-test", data)
	if err != nil || !bytes.Equal(data[:n], want) {
		t.Fatal("save discarded source attributes", err)
	}
}

func TestBudgetSaveRejectsExtendedAttributeChange(t *testing.T) {
	dir, path := budgetCommandFixture(t)
	setBudgetTestXattr(t, path, []byte("original"))
	inputs, err := budget.LoadDerivationInputs(path, budget.LoadOptions{RootDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	temp, err := stageBudgetAt(root, relative, []byte("obsolete proposal"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	setBudgetTestXattr(t, path, []byte("restricted"))
	if err := replaceBudgetAt(root, relative, temp, inputs); !errors.Is(err, errBudgetChanged) {
		t.Fatal("save accepted changed access metadata", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("save changed newer source", err)
	}
	data := make([]byte, 100)
	n, err := unix.Getxattr(path, "user.budget-test", data)
	if err != nil || string(data[:n]) != "restricted" {
		t.Fatal("save discarded changed attributes", err)
	}
}

func TestBudgetSavePreservesPOSIXACL(t *testing.T) {
	dir, path := budgetCommandFixture(t)
	// Linux POSIX ACL xattr version 2: owner, a named reader, owning group,
	// access mask, other. The named entry cannot be represented by mode bits.
	acl := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(acl, 2)
	entries := [][3]uint32{{1, 6, ^uint32(0)}, {2, 4, uint32(os.Getuid() + 1)}, {4, 0, ^uint32(0)}, {16, 4, ^uint32(0)}, {32, 0, ^uint32(0)}}
	for i, entry := range entries {
		at := 4 + i*8
		binary.LittleEndian.PutUint16(acl[at:], uint16(entry[0]))
		binary.LittleEndian.PutUint16(acl[at+2:], uint16(entry[1]))
		binary.LittleEndian.PutUint32(acl[at+4:], entry[2])
	}
	if err := unix.Setxattr(path, "system.posix_acl_access", acl, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EPERM) {
			t.Skip("temporary filesystem does not support POSIX ACLs")
		}
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, diagnostic := runBudgetTest("derive", "--budget", path, "--root", dir, "--write"); code != 0 {
		t.Fatal(code, diagnostic)
	}
	data := make([]byte, 100)
	n, err := unix.Getxattr(path, "system.posix_acl_access", data)
	if err != nil || !bytes.Equal(data[:n], acl) {
		t.Fatal("save discarded source ACL", err)
	}
	after, err := os.Stat(path)
	if err != nil || before.Mode() != after.Mode() {
		t.Fatal("save changed ACL permission mask", err)
	}
}
