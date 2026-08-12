package host

import _ "embed"

// NavigationRuntime is the browser navigation host embedded by
// server.NavigationScript. The compatibility adapter precedes the JSDoc-only
// TypeScript authority so navigation publishes legacy names through the same
// facade-owned boundary as the bootstrap bundles.
//
//go:embed compatibility.ts
var compatibilityRuntime string

//go:embed navigation.ts
var navigationRuntime string

var NavigationRuntime = compatibilityRuntime + "\n" + navigationRuntime
