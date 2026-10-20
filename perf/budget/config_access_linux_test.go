package budget

import (
	"os"
	"syscall"
	"testing"
)

func TestConfigSnapshotRejectsMetadataChange(t *testing.T) {
	inputs, err := LoadDerivationInputs("testdata/budget.v2.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(inputs.BudgetPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ctime", "links"} {
		t.Run(field, func(t *testing.T) {
			info, err := os.Stat(inputs.BudgetPath())
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			if field == "ctime" {
				stat.Ctim.Sec++
			} else {
				stat.Nlink++
			}
			if inputs.BudgetFileMatches(info, data) {
				t.Fatal("changed metadata accepted as unchanged")
			}
		})
	}
}
