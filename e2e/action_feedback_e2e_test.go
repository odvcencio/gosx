//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"m31labs.dev/gosx"
)

func TestManagedFormFeedbackAfterRedirect(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	fixture := filepath.Join(root, "e2e", "testdata", "action-feedback")
	proof := newRuntimeBrowserProof(t, filepath.Join(fixture, "main.go"))
	defer writeRuntimeBrowserProof(t, "managed-form-feedback.json", &proof)
	baseline, err := os.ReadFile(filepath.Join(fixture, "baseline-observation.json"))
	if err != nil {
		t.Fatal(err)
	}
	proof.Baseline = baseline
	appDir := t.TempDir()
	copyFixtureTree(t, fixture, appDir)
	module := "module example.invalid/gosx-form-feedback\n\ngo 1.26.0\n\nrequire m31labs.dev/gosx v" + strings.TrimPrefix(gosx.Version, "v") + "\n\nreplace m31labs.dev/gosx => " + strconv.Quote(filepath.ToSlash(root)) + "\n"
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}
	app := startDevApp(t, appDir, fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t)))
	page := newBrowserPage(t, chrome, nil, 1100, 800, "", 120*time.Second)
	var statusMu sync.Mutex
	var statuses []int
	chromedp.ListenTarget(page.ctx, func(event any) {
		if response, ok := event.(*network.EventResponseReceived); ok && strings.Contains(response.Response.URL, "/__actions/save") {
			statusMu.Lock()
			statuses = append(statuses, int(response.Response.Status))
			statusMu.Unlock()
		}
	})
	page.eval(t, "navigator.userAgent", &proof.Browser)
	load := func() {
		t.Helper()
		if code := page.navigate(t, app.baseURL); code != http.StatusOK {
			t.Fatalf("fixture HTTP %d", code)
		}
		page.waitFor(t, `!!window.__gosx?.navigation?.submitAction && !!document.querySelector('[name="csrf_token"]').value`, 10*time.Second, "managed forms and CSRF token")
		page.eval(t, `window.__gosxFormProof = document.getElementById("save-form"); window.__gosxFormProofResults = 0; document.addEventListener("gosx:form:result", () => window.__gosxFormProofResults++)`, nil)
	}
	readState := func() map[string]any {
		t.Helper()
		var state map[string]any
		page.eval(t, `(() => { const form = document.getElementById("save-form"); return {state: form.getAttribute("data-gosx-form-state"), pending: form.getAttribute("data-gosx-pending"), sameForm: form === window.__gosxFormProof, message: form.querySelector(".form-status").textContent, error: form.querySelector(".form-error").textContent}; })()`, &state)
		return state
	}
	submit := func(value string) {
		t.Helper()
		if err := chromedp.Run(page.ctx,
			chromedp.Click(`#save-form input[name="title"]`, chromedp.ByQuery),
			chromedp.KeyEvent(kb.Control+"a"),
			chromedp.KeyEvent(kb.Backspace),
			chromedp.SendKeys(`#save-form input[name="title"]`, value, chromedp.ByQuery),
			chromedp.Click(`#save-form button[type="submit"]`, chromedp.ByQuery),
		); err != nil {
			var debug any
			page.eval(t, `(() => { const input = document.querySelector('#save-form input[name="title"]'); return {html: input.outerHTML, value: input.value, type: input.type}; })()`, &debug)
			t.Fatalf("%v (wanted %q, input %v)", err, value, debug)
		}
	}
	for _, rejected := range []struct {
		value  string
		status int
	}{{"", 422}, {"conflict", 409}} {
		load()
		submit(rejected.value)
		page.waitFor(t, `document.getElementById("save-form").getAttribute("data-gosx-form-state") === "error"`, 5*time.Second, "server rejection")
		before := readState()
		if before["error"] == "" || before["pending"] != nil {
			t.Fatalf("incomplete rejected form state: %v", before)
		}
		submit("Corrected title")
		page.waitFor(t, `location.search === "?saved=1" && window.__gosxFormProofResults === 2 && document.getElementById("save-form").getAttribute("data-gosx-pending") !== "true"`, 5*time.Second, "redirected correction settles")
		after := readState()
		proof.Steps = append(proof.Steps, devRuntimeStep{Input: fmt.Sprintf("%d rejection, correct input, click Save, follow 303 redirect", rejected.status), Expected: "retained form idle; previous field error cleared; no pending marker", Observed: map[string]any{"rejected": before, "corrected": after}})
		if after["state"] != "idle" || after["pending"] != nil || after["sameForm"] != true || after["error"] != "" {
			t.Fatalf("correction did not settle the retained form: %v", after)
		}
	}
	load()
	submit("destination")
	page.waitFor(t, `location.search === "?destination=error" && window.__gosxFormProofResults === 1 && document.getElementById("save-form").getAttribute("data-gosx-pending") !== "true"`, 5*time.Second, "destination redirect settles")
	destination := readState()
	proof.Steps = append(proof.Steps, devRuntimeStep{Input: "save and redirect to a destination with fresh error feedback", Expected: "destination error state preserved; no pending marker", Observed: destination})
	if destination["state"] != "error" || destination["sameForm"] != true || destination["pending"] != nil || destination["message"] != "Destination needs attention." {
		t.Fatalf("fresh destination feedback changed: %v", destination)
	}
	load()
	submit("inline")
	page.waitFor(t, `document.getElementById("save-form").getAttribute("data-gosx-form-state") === "success"`, 5*time.Second, "non-redirect success")
	proof.Steps = append(proof.Steps, devRuntimeStep{Input: "submit a successful action without a redirect", Expected: "success", Observed: readState()})
	statusMu.Lock()
	observedStatuses := append([]int(nil), statuses...)
	statusMu.Unlock()
	wantStatuses := []int{422, 303, 409, 303, 303, 200}
	if fmt.Sprint(observedStatuses) != fmt.Sprint(wantStatuses) {
		t.Fatalf("action responses %v, want %v", observedStatuses, wantStatuses)
	}
	proof.Steps = append(proof.Steps, devRuntimeStep{Input: "actual action response sequence", Expected: wantStatuses, Observed: observedStatuses})
}
