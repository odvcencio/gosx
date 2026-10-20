// Package evalparity is a differential test harness for GoSX's expression
// language. GoSX evaluates a `{expr}` hole through three independent code
// paths that must agree:
//
//  1. Transpile: transpile.Transpile emits real Go source, compiled by the
//     Go compiler (m31labs.dev/gosx/transpile). See transpile_run.go.
//  2. Route: the file router interprets the compiled IR per request with
//     go/parser + reflect (m31labs.dev/gosx/route). This is what
//     router.AddDir actually serves. See route_run.go.
//  3. VM: the same IR, lowered by ir.LowerIsland into island bytecode and
//     walked by the client VM (m31labs.dev/gosx/client/vm) — the same code
//     that runs in the browser under WASM, callable directly here because
//     the VM package is plain Go. See vm_run.go.
//
// cases_table.go holds a table of small, one-expression components.
// source.go renders each case to byte-identical GoSX source for all three
// backends (so a mismatch can only come from evaluation, never from
// different input markup). harness_test.go's TestExpressionParity runs
// every case through every backend that claims to support it and compares
// the rendered text.
//
// # Triage contract
//
// A case's outcome on a backend falls into exactly one of three buckets,
// and the table (Case, in case.go) must say which one before the test can
// pass:
//
//   - Supported and agreeing: the backend's output equals Case.Want. This
//     is the default — a case with empty Unsupported/Diverges entries for
//     a backend must produce Want on that backend.
//   - Unsupported: the backend cannot run the case at all (a compile or
//     lower error). Recorded in Case.Unsupported[backend] with a one-line
//     reason. This is a language-boundary fact (e.g. GSX's ternary syntax
//     is not valid Go, so transpile can never run it), not a bug to fix.
//   - Diverges: the backend runs without error but produces something
//     other than Want. Recorded in Case.Diverges[backend] (the pinned,
//     actual wrong output) and Case.DivergesReason[backend] (why it is
//     accepted rather than fixed).
//
// Finding a real, small, clearly-correct bug (see route/exprlower.go's
// int/int QUO fix and client/vm/value.go's truth() fix, both landed
// alongside this package) is preferred over recording a Diverges entry.
// Diverges is for a mismatch that is not a small, targeted fix — usually
// because the two backends have genuinely different designs (route's
// reflect interpreter is dynamically typed; transpile's output is
// statically typed real Go) and reconciling them is a bigger, separate
// change.
//
// Any outcome the table does not predict — an unmarked backend erroring,
// an Unsupported backend running anyway, or a Diverges backend's pinned
// text changing — fails TestExpressionParity loudly. Nothing here skips
// silently.
//
// # Generated support matrix
//
// matrix.go renders cases into docs/expression-support-matrix.md.
// TestSupportMatrixUpToDate (matrix_test.go) fails if the checked-in file
// does not match; regenerate it with:
//
//	go test ./internal/evalparity/... -run TestSupportMatrixUpToDate -update
package evalparity
