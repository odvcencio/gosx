package perf

import "m31labs.dev/gosx/browsertest"

// FindChrome returns the path to a Chrome or Chromium executable.
// Resolution order:
//  1. CHROME_PATH env var (if set and executable)
//  2. PATH lookup for well-known binary names
//  3. Platform-specific default install paths
//
// It is browsertest.FindChrome.
func FindChrome() (string, error) { return browsertest.FindChrome() }
