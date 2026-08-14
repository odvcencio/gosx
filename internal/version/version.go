package version

// Current is the canonical GoSX release tag.
const Current = "v0.40.1"

// Number is Current without the leading tag prefix. Keep this constant in sync
// with Current so packages that historically expose bare semver remain stable.
const Number = "0.40.1"

// MinGo is the lowest Go toolchain release that can build GoSX. It must equal
// the go directive in the repository go.mod.
//
// A scaffolded application requires m31labs.dev/gosx, so it inherits this floor
// whether or not its own go.mod admits it. When the two disagree and the
// scaffold declares the lower one, `go run .` fails in a fresh project with
// "module ... requires go >= X", which is the first command a new user runs.
// That is how this drifted before: go.mod moved to 1.26 and the scaffold kept
// emitting 1.25.1.
//
// TestMinGoMatchesGoModDirective reads go.mod and fails if they part company.
const MinGo = "1.26"
