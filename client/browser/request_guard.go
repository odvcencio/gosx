package browser

// RequestPolicy configures exact URL paths to suppress across origins. Query
// strings do not affect matching. Unmatched calls preserve receiver/arguments.
type RequestPolicy struct{ BlockedPaths []string }
