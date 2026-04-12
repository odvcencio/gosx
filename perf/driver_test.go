//go:build browser

package perf

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDriverLaunchAndClose(t *testing.T) {
	d, err := New(WithHeadless(true), WithTimeout(10*time.Second))
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer d.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Test Page</title></head><body><h1>hello</h1></body></html>`)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	var title string
	if err := d.Evaluate(`document.title`, &title); err != nil {
		t.Fatalf("Evaluate title: %v", err)
	}
	if title != "Test Page" {
		t.Fatalf("expected title 'Test Page', got %q", title)
	}

	var text string
	if err := d.Evaluate(`document.querySelector("h1").textContent`, &text); err != nil {
		t.Fatalf("Evaluate h1: %v", err)
	}
	if text != "hello" {
		t.Fatalf("expected 'hello', got %q", text)
	}
}
