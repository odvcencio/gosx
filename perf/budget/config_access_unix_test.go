//go:build unix

package budget

import (
	"os"
	"syscall"
	"testing"
)

func TestConfigSnapshotRejectsOwnershipChange(t *testing.T) {
	inputs, err := LoadDerivationInputs("testdata/budget.v2.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(inputs.BudgetPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"owner", "group"} {
		t.Run(field, func(t *testing.T) {
			info, err := os.Stat(inputs.BudgetPath())
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			// Mutate a fresh native FileInfo, keeping device/inode, size and mtime.
			// This does not need privileges or alter the source file's ownership.
			if field == "owner" {
				stat.Uid++
			} else {
				stat.Gid++
			}
			if inputs.BudgetFileMatches(info, data) {
				t.Fatal("changed ownership accepted as unchanged")
			}
		})
	}
}
