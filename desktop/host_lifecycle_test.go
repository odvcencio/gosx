package desktop

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func lifecycleOptions() HostLifecycleOptions {
	return HostLifecycleOptions{Origin: "https://example.test", ReadyPath: "/", ReadyMessageType: "document-ready", ReportDOMReady: true, AllowBlankLoadingPage: true, ServiceName: "host", Startup: StartupData{Strings: map[string]string{"testMode": "renderer"}}, DocumentMarker: DocumentMarker{Name: "desktop", Value: "true"}, Telemetry: &HostTelemetryConfig{Endpoint: "/events?token=secret", Enabled: false}, ProcessFailureTest: &HostProcessFailureTest{Kind: ProcessFailedRenderProcessExited, MessageType: "process-failed", SentFlag: "failureSent"}, FailurePage: HostFailurePage{HTML: `<head><title>Server stopped</title></head><body><button id="retry">Retry</button></body>`, MessageID: "message", DetailsID: "details", RetryID: "retry", StatusID: "status", Marker: DocumentMarker{Name: "failed", Value: "1"}, DefaultError: "Server stopped", EmptyDetails: "No logs", StartingText: "Starting", RetryError: "Retry failed"}}
}
func TestHostLifecycleBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable")
	}
	script, err := HostLifecycleBootstrap(lifecycleOptions())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"script": script})
	cmd := exec.Command(node, "testdata/host_lifecycle.cjs")
	cmd.Stdin = strings.NewReader(string(data))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lifecycle: %v\n%s", err, output)
	}
}
func TestHostLifecycleValidationAndEscaping(t *testing.T) {
	for _, origin := range []string{"https://example.test/path", "https://user:secret@example.test", "javascript:bad", "https://example.test?token=secret"} {
		o := lifecycleOptions()
		o.Origin = origin
		if _, err := HostLifecycleBootstrap(o); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid origin error: %v", err)
		}
	}
	o := lifecycleOptions()
	o.PollInterval = -time.Second
	if _, err := HostLifecycleBootstrap(o); err == nil {
		t.Fatal("negative interval accepted")
	}
	o = lifecycleOptions()
	o.FailurePage.HTML = "</script>\u2028\u2029"
	o.Startup.Strings["unsafe-key"] = "</script>\u2028\u2029"
	script, err := HostLifecycleBootstrap(o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "</script>") || strings.ContainsAny(script, "\u2028\u2029") {
		t.Fatal("unsafe script delimiters")
	}
	if !strings.Contains(script, `\u003c/script\u003e\u2028\u2029`) {
		t.Fatal("JSON escaping missing")
	}
}
