//go:build !unix

package budget

import "os"

// FileInfo does not expose POSIX ownership or link counts on these platforms.
func sameBudgetFileOwnership(a, b os.FileInfo) bool { return true }
