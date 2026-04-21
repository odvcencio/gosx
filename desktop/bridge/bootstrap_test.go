package bridge

import (
	"strings"
	"testing"
)

func TestBootstrapScriptExposesDesktopAPI(t *testing.T) {
	script := BootstrapScript()
	if strings.TrimSpace(script) == "" {
		t.Fatal("BootstrapScript returned an empty script")
	}

	for _, want := range []string{
		"window",
		"gosxDesktop",
		"call: call",
		"emit: emit",
		"on: on",
		"chrome.webview.postMessage",
		"chrome.webview.addEventListener",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("BootstrapScript missing %q", want)
		}
	}
}

func TestBootstrapScriptHandlesBridgeOpcodes(t *testing.T) {
	script := BootstrapScript()
	for _, want := range []string{
		`send("req"`,
		`send("evt"`,
		`env.op === "res"`,
		`env.op === "err"`,
		`env.op === "frame"`,
		`env.op === "end"`,
		`env.op === "evt"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("BootstrapScript missing opcode handling %q", want)
		}
	}
}

func TestBootstrapScriptTypedErrorsAndFrames(t *testing.T) {
	script := BootstrapScript()
	for _, want := range []string{
		"GosxDesktopError",
		"err.code",
		"err.detail",
		"request.reject(makeError(env))",
		"options.onFrame",
		"request.onFrame(env.payload, env)",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("BootstrapScript missing %q", want)
		}
	}
}

func TestBootstrapScriptHandlesBuiltInReloadEvent(t *testing.T) {
	script := BootstrapScript()
	for _, want := range []string{
		"gosx.dev.reload",
		"root.location.reload()",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("BootstrapScript missing reload behavior %q", want)
		}
	}
}

func TestBootstrapScriptIsIdempotenceGuarded(t *testing.T) {
	script := BootstrapScript()
	for _, want := range []string{
		"__gosxDesktopBridge === true",
		"return;",
		"Object.freeze",
		`Object.defineProperty(root, "gosxDesktop"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("BootstrapScript missing idempotence guard %q", want)
		}
	}
}
