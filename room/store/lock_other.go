//go:build !unix

package store

import "os"

// tryLock is a no-op where advisory file locks are unavailable; run a single
// process per directory.
func tryLock(*os.File) error { return nil }
