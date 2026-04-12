//go:build browser

package perf

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordGIF(t *testing.T) {
	d, err := New(WithHeadless(true), WithTimeout(15*time.Second))
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer d.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body style="background:blue"><h1 style="color:white">Recording Test</h1></body></html>`)
	}))
	defer srv.Close()

	// Navigate first to ensure a target exists for the screencast.
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	rec, err := StartRecording(d)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	// Give screencast time to capture frames.
	time.Sleep(2 * time.Second)

	outDir := t.TempDir()
	gifPath := filepath.Join(outDir, "test.gif")

	if err := rec.Stop(d, gifPath); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	info, err := os.Stat(gifPath)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("output file is empty")
	}
	t.Logf("recorded %s (%d bytes)", gifPath, info.Size())
}
