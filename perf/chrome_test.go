package perf

import (
	"os"
	"testing"
)

// TestFindChromeFromEnv sets CHROME_PATH to the test binary (guaranteed executable)
// and verifies FindChrome returns it directly.
func TestFindChromeFromEnv(t *testing.T) {
	// os.Executable returns the path of the running test binary.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	t.Setenv("CHROME_PATH", exe)

	got, err := FindChrome()
	if err != nil {
		t.Fatalf("FindChrome() error = %v", err)
	}
	if got != exe {
		t.Errorf("FindChrome() = %q, want %q", got, exe)
	}
}

// TestFindChromeNoEnvFallback unsets CHROME_PATH and relies on PATH or platform
// defaults. If Chrome is not available on this machine the test is skipped.
func TestFindChromeNoEnvFallback(t *testing.T) {
	t.Setenv("CHROME_PATH", "")

	got, err := FindChrome()
	if err != nil {
		t.Skipf("Chrome not found on this machine (skipping): %v", err)
	}
	if got == "" {
		t.Error("FindChrome() returned empty path without error")
	}
	t.Logf("Chrome found at: %s", got)
}

// TestFindChromeInvalidPath sets CHROME_PATH to a non-existent path and
// verifies FindChrome returns a non-nil error.
func TestFindChromeInvalidPath(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nonexistent/chrome")

	_, err := FindChrome()
	if err == nil {
		t.Fatal("FindChrome() expected error for non-existent CHROME_PATH, got nil")
	}
}
