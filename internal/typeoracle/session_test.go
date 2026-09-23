package typeoracle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// newTestModule builds a standalone temp Go module with a stub
// "m31labs.dev/gosx" package (just enough of the runtime API transpile's
// projection calls) replaced in locally, so `go list -export` here resolves
// a tiny, fast dependency graph instead of gosx's own full module tree —
// the same shape strictcheck/check_test.go's newTestModule uses, kept as
// its own local copy since that one is unexported from a different
// package.
func newTestModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "gosxstub")
	mustMkdir(t, stub)
	mustWrite(t, filepath.Join(stub, "go.mod"), "module m31labs.dev/gosx\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(stub, "node.go"), `package gosx
type Node struct{}
type AttrValue struct{}
type AttrList []AttrValue
func El(string, ...any) Node { return Node{} }
func Text(string) Node { return Node{} }
func Expr(any) Node { return Node{} }
func RawHTML(string) Node { return Node{} }
func Fragment(...Node) Node { return Node{} }
func Attr(string, any) AttrValue { return AttrValue{} }
func Attrs(...AttrValue) any { return nil }
func Props(values ...AttrValue) AttrList { return values }
func Spread(any) AttrValue { return AttrValue{} }
func If(cond bool, child Node) Node { return Node{} }
func Map[T any](items []T, fn func(T, int) Node) Node { return Node{} }
`)
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.test/app\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => "+filepath.ToSlash(stub)+"\n")
	return dir
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

// testOptions forces GOWORK off: a checkout nested under an ancestor
// directory that itself has a go.work (this worktree's own repository
// layout, for one) would otherwise have every `go list` call in this test
// silently resolved against the wrong module — see the package doc's
// Importer choice section neighbor, strictcheck/check.go's identical
// GOWORK field, for the same rationale.
func testOptions() Options {
	return Options{GOWORK: "off"}
}

func loadTestSession(t *testing.T, dir string) *Session {
	t.Helper()
	session, err := LoadWithOptions(context.Background(), dir, testOptions())
	if err != nil {
		t.Fatalf("LoadWithOptions: %v", err)
	}
	return session
}
