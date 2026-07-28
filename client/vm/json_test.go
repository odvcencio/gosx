package vm

import "testing"

// TestValueToAnySelfReferentialArrayDoesNotOverflow mirrors the
// String() / Eq() cycle-safety regressions in value_test.go for the
// JSON marshal path. ToAny recurses over Items exactly like
// String() did before the fix. So a self-referential array, built
// through OpIndexSet's in-place mutation (see lhs_set.go), would
// recurse until the goroutine stack overflowed. That is a fatal
// error, not a panic, so recover() could never catch it.
func TestValueToAnySelfReferentialArrayDoesNotOverflow(t *testing.T) {
	arr := ArrayVal([]Value{{}})
	arr.List()[0] = arr

	got := arr.ToAny()

	// The depth guard does not fire on the first recursion. It fires
	// only after maxValueRecursionDepth levels. So walk down the
	// nested []any{[]any{...}} chain the cycle produces, and confirm
	// the recursion is bounded. Within a small multiple of
	// maxValueRecursionDepth steps, the walk must reach the cycle
	// sentinel. An unguarded ToAny would instead keep producing
	// another []any forever, until the goroutine stack overflowed.
	steps := 0
	for steps <= maxValueRecursionDepth+2 {
		items, ok := got.([]any)
		if !ok {
			if s, ok := got.(string); ok && s == cycleSentinel {
				return
			}
			t.Fatalf("ToAny() cycle walk step %d = %#v (%T), want []any or the cycle sentinel %q", steps, got, got, cycleSentinel)
		}
		if len(items) != 1 {
			t.Fatalf("ToAny() cycle walk step %d returned %d items, want 1", steps, len(items))
		}
		got = items[0]
		steps++
	}
	t.Fatalf("ToAny() on a self-referential array never reached the cycle sentinel within %d steps; the depth guard did not fire", steps)
}
