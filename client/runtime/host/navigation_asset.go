package host

import _ "embed"

// NavigationRuntime is the browser navigation host embedded by server.NavigationScript.
// The TypeScript source is deliberately embedded as-is: its type surface is JSDoc-only,
// so the browser executes the same source without a runtime transpilation step.
//
//go:embed navigation.ts
var NavigationRuntime string
