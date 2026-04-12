//go:build browser

package perf

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClickUpdatesDOM verifies that Click works on a page with a button
// that updates text.
func TestClickUpdatesDOM(t *testing.T) {
	d, err := New(WithHeadless(true), WithTimeout(10*time.Second))
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	defer d.Close()

	page := `<html><body>
	<span id="count">0</span>
	<button id="inc" onclick="document.getElementById('count').textContent = parseInt(document.getElementById('count').textContent) + 1">+</button>
	</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	if err := Click(d, "#inc"); err != nil {
		t.Fatalf("Click: %v", err)
	}

	var text string
	if err := d.Evaluate(`document.getElementById("count").textContent`, &text); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if text != "1" {
		t.Fatalf("expected count '1' after click, got %q", text)
	}
}
