//go:build !linux

package budget

import "os"

// The portable snapshot has no metadata-change timestamp on other platforms.
func sameBudgetFileChangeTime(a, b os.FileInfo) bool { return true }
