package budget

import (
	"os"
	"syscall"
)

// Linux ctime detects changes to extended attributes and ACLs, even when the
// content, permission bits and mtime have not changed.
func sameBudgetFileChangeTime(a, b os.FileInfo) bool {
	left, ok := a.Sys().(*syscall.Stat_t)
	right, otherOK := b.Sys().(*syscall.Stat_t)
	return ok && otherOK && left.Ctim == right.Ctim
}
