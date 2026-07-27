package gosx

import "m31labs.dev/gosx/internal/version"

// Version is the current GoSX library release.
const Version = version.Number

// MinGoVersion is the lowest Go toolchain release that can build GoSX, as it
// appears in a go directive. Code that generates a go.mod must declare at least
// this, because requiring m31labs.dev/gosx inherits the floor regardless.
const MinGoVersion = version.MinGo
