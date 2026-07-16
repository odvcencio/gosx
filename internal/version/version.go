package version

// Current is the canonical GoSX release tag.
const Current = "v0.31.18"

// Number is Current without the leading tag prefix. Keep this constant in sync
// with Current so packages that historically expose bare semver remain stable.
const Number = "0.31.18"
