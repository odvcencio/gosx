package motion

// Policy carries per-evaluation behavioral overrides.
// ReducedMotion is accepted by Eval but its behavior is implemented in Task 1.6d.
type Policy struct {
	ReducedMotion bool
}
