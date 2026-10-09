//go:build unix

package budget

import (
	"os"
	"syscall"
)

func sameBudgetFileOwnership(a, b os.FileInfo) bool {
	left, ok := a.Sys().(*syscall.Stat_t)
	right, otherOK := b.Sys().(*syscall.Stat_t)
	return ok && otherOK && left.Uid == right.Uid && left.Gid == right.Gid && left.Nlink == right.Nlink
}
