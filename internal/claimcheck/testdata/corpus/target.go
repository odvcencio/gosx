// Package target is the file the fixture claims describe. The go tool ignores
// everything under testdata, so this file never compiles with the package.
package target

// Alpha exists exactly once.
func Alpha() {}

// Beta and Beta2 share a name prefix, so a count of that prefix is 2.
func Beta() {}

func Beta2() {}
