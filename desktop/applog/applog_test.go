package applog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestOpenAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(Options{Dir: dir, Name: "defaults"})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	if log.Path() != filepath.Join(dir, "defaults.log") {
		t.Fatalf("Path() = %q", log.Path())
	}
	if _, err := log.Write([]byte(strings.Repeat("x", 5<<20))); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("y")); err != nil {
		t.Fatal(err)
	}
	if got := fileSize(t, filepath.Join(dir, "defaults.1.log")); got != 5<<20 {
		t.Fatalf("rotated file size = %d, want %d", got, 5<<20)
	}
	if got := fileSize(t, filepath.Join(dir, "defaults.log")); got != 1 {
		t.Fatalf("active file size = %d, want 1", got)
	}

	for i := 0; i < 4; i++ {
		if _, err := log.Write([]byte(strings.Repeat("z", 5<<20))); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 3; i++ {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("defaults.%d.log", i))); err != nil {
			t.Fatalf("default rotation %d: %v", i, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "defaults.4.log")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unexpected fourth rotated file, stat error = %v", err)
	}
}

func TestOpenRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name    string
		options Options
	}{
		{name: "empty dir", options: Options{Name: "app"}},
		{name: "empty name", options: Options{Dir: t.TempDir()}},
		{name: "slash", options: Options{Dir: t.TempDir(), Name: "sub/app"}},
		{name: "backslash", options: Options{Dir: t.TempDir(), Name: `sub\app`}},
		{name: "dot dot", options: Options{Dir: t.TempDir(), Name: "app..log"}},
		{name: "negative max bytes", options: Options{Dir: t.TempDir(), Name: "app", MaxBytes: -1}},
		{name: "negative keep", options: Options{Dir: t.TempDir(), Name: "app", Keep: -1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if log, err := Open(test.options); !errors.Is(err, ErrInvalidOptions) {
				if log != nil {
					log.Close()
				}
				t.Fatalf("Open() error = %v, want ErrInvalidOptions", err)
			}
		})
	}
}

func TestRotationKeepsNewestFiles(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(Options{Dir: dir, Name: "rotate", MaxBytes: 4, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	for _, value := range []string{"1111", "2222", "3333", "4444"} {
		if _, err := log.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	for path, want := range map[string]string{
		"rotate.1.log": "3333",
		"rotate.2.log": "2222",
		"rotate.log":   "4444",
	} {
		if got := readFile(t, filepath.Join(dir, path)); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "rotate.3.log")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unexpected third rotated file, stat error = %v", err)
	}
}

func TestOversizedWriteStaysWhole(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(Options{Dir: dir, Name: "large", MaxBytes: 4, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	if n, err := log.Write([]byte("oversized")); err != nil || n != len("oversized") {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	if got := readFile(t, filepath.Join(dir, "large.log")); got != "oversized" {
		t.Fatalf("active file = %q", got)
	}
	if _, err := log.Write([]byte("next")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "large.1.log")); got != "oversized" {
		t.Fatalf("rotated file = %q, want whole oversized write", got)
	}
}

func TestConcurrentWrites(t *testing.T) {
	const (
		writers = 8
		writes  = 100
		chunk   = 64
	)
	dir := t.TempDir()
	log, err := Open(Options{Dir: dir, Name: "concurrent", MaxBytes: 4096, Keep: 20})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writes; j++ {
				if n, err := log.Write([]byte(strings.Repeat("x", chunk))); err != nil || n != chunk {
					t.Errorf("Write() = %d, %v", n, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	want := int64(writers * writes * chunk)
	if total != want {
		t.Fatalf("total bytes = %d, want %d", total, want)
	}
}

func TestWriteAfterClose(t *testing.T) {
	log, err := Open(Options{Dir: t.TempDir(), Name: "closed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Write() error = %v, want os.ErrClosed", err)
	}
}

func TestReopenCountsExistingSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.log")
	if err := os.WriteFile(path, []byte("old!"), 0644); err != nil {
		t.Fatal(err)
	}
	log, err := Open(Options{Dir: dir, Name: "existing", MaxBytes: 4, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if _, err := log.Write([]byte("new!")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "existing.1.log")); got != "old!" {
		t.Fatalf("rotated existing content = %q, want old!", got)
	}
	if got := readFile(t, path); got != "new!" {
		t.Fatalf("active content = %q, want new!", got)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWriteReopensAfterFailedRotationReopen(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(Options{Dir: dir, Name: "app", MaxBytes: 64, Keep: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	// Simulate a rotation that closed the file and could not reopen it.
	l.mu.Lock()
	_ = l.file.Close()
	l.file = nil
	l.mu.Unlock()
	if _, err := l.Write([]byte("after failure\n")); err != nil {
		t.Fatalf("Write after failed reopen = %v, want a retry that succeeds", err)
	}
	data, err := os.ReadFile(l.Path())
	if err != nil || !strings.Contains(string(data), "after failure") {
		t.Fatalf("log = %q, %v", data, err)
	}
}
