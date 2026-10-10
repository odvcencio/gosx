package server

import (
	"os/exec"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestTelemetryConfigHeadConsentNonceAndEscaping(t *testing.T) {
	state := NewPageState()
	state.SetNonce(`nonce"<`)
	ctx := &Context{PageState: *state}
	rendered := gosx.RenderHTML(ctx.TelemetryConfigHead(BrowserTelemetryConfig{Enabled: false, Endpoint: "</script>\u2028\u2029"}))
	for _, value := range []string{`"enabled":false`, `nonce="nonce&#34;&lt;"`, `\u003c/script\u003e\u2028\u2029`} {
		if !strings.Contains(rendered, value) {
			t.Fatalf("missing %q in %s", value, rendered)
		}
	}
	if strings.Count(rendered, "</script>") != 1 {
		t.Fatal("endpoint broke out of script")
	}
	var nilContext *Context
	if html := gosx.RenderHTML(nilContext.TelemetryConfigHead(BrowserTelemetryConfig{Enabled: false})); !strings.Contains(html, `"enabled":false`) {
		t.Fatal("nil context lost consent")
	}
}

func TestTelemetryOptOutRunsBeforeRuntimeStartup(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable")
	}
	html := gosx.RenderHTML((&Context{}).TelemetryConfigHead(BrowserTelemetryConfig{Enabled: false}))
	script := strings.TrimSuffix(strings.TrimPrefix(html, "<script>"), "</script>")
	// A startup failure immediately after configuration must observe opt-out.
	fixture := `const vm=require('node:vm'),fs=require('node:fs'),assert=require('node:assert/strict');const window={__gosx_telemetry_config:{endpoint:'/existing',enabled:true}};vm.runInNewContext(fs.readFileSync(0,'utf8'),{window});assert.equal(window.__gosx_telemetry_config.enabled,false);assert.equal(window.__gosx_telemetry_config.endpoint,'/existing');`
	cmd := exec.Command(node, "-e", fixture)
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("early consent: %v\n%s", err, output)
	}
}
