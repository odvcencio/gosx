//go:build browser

package ouroboros

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRemoteBrowserEnvironmentSmoke(t *testing.T) {
	wsURL := strings.TrimSpace(os.Getenv("CHROME_WS_URL"))
	if wsURL == "" {
		t.Skip("CHROME_WS_URL is required for remote browser smoke")
	}
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")

	env, err := CollectBrowserEnvironment(t.Context(), BrowserBaselineOptions{
		Timeout:        10 * time.Second,
		Headless:       true,
		Environment:    "headless-logic",
		ViewportWidth:  1280,
		ViewportHeight: 720,
		DPR:            1,
	})
	if err != nil {
		t.Fatalf("CollectBrowserEnvironment: %v", err)
	}
	if got := env.Browser["connectionMode"]; got != "remote-cdp" {
		t.Fatalf("connectionMode = %#v, want remote-cdp", got)
	}
	if got := env.Browser["product"]; got == nil || got == "" {
		t.Fatalf("missing Browser.getVersion product: %#v", env.Browser)
	}
	if got := env.Browser["userAgent"]; got == nil || got == "" {
		t.Fatalf("missing Browser.getVersion userAgent: %#v", env.Browser)
	}
	if _, ok := env.GPU["probeError"]; ok {
		t.Fatalf("GPU probe failed: %#v", env.GPU["probeError"])
	}
	if _, ok := env.Browser["remoteEndpoint"]; ok {
		t.Fatalf("remoteEndpoint display value must not be recorded: %#v", env.Browser["remoteEndpoint"])
	}
	if hash, _ := env.Browser["remoteEndpointSHA256"].(string); !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("remote endpoint hash missing: %q", hash)
	}
}
