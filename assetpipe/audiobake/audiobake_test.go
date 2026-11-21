package audiobake

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFreshNeedsTheSameSumAndEveryOutput(t *testing.T) {
	dir := t.TempDir()
	stamp, out := filepath.Join(dir, "a.sha256"), filepath.Join(dir, "a.opus")
	sum := Sum([]byte("score"))
	if Fresh(stamp, sum, out) {
		t.Fatal("fresh with no stamp")
	}
	if err := WriteStamp(stamp, sum); err != nil {
		t.Fatal(err)
	}
	if Fresh(stamp, sum, out) {
		t.Fatal("fresh with the output missing")
	}
	_ = os.WriteFile(out, []byte("x"), 0o644)
	if !Fresh(stamp, sum, out) || Fresh(stamp, Sum([]byte("changed")), out) {
		t.Fatal("Fresh does not follow the stamp")
	}
}

func TestCopyTreeCopiesOnlyTheChosenFiles(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	for _, p := range []string{"s00/fight.opus", "s01/fight.opus", "s00/fight.sha256"} {
		_ = os.MkdirAll(filepath.Join(src, filepath.Dir(p)), 0o755)
		_ = os.WriteFile(filepath.Join(src, p), []byte(p), 0o644)
	}
	if err := CopyTree(src, dst, ".opus", func(rel string) bool { return filepath.Dir(rel) == "s00" }); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "s00/fight.opus")); err != nil || string(b) != "s00/fight.opus" {
		t.Fatalf("copied %q, %v", b, err)
	}
	for _, p := range []string{"s01/fight.opus", "s00/fight.sha256"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err == nil {
			t.Fatalf("%s was copied", p)
		}
	}
	if err := CopyTree(filepath.Join(src, "missing"), dst, ".opus", nil); err != nil {
		t.Fatal(err)
	}
}
