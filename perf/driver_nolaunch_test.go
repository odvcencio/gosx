package perf

import (
	"testing"
)

func TestDriverNoChromeError(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	_, err := New()
	if err == nil {
		t.Fatal("expected error when Chrome not found")
	}
}
