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
		"app: appAPI",
		"window: windowAPI",
		"dialog: dialogAPI",
		"clipboard: clipboardAPI",
		"shell: shellAPI",
		"notification: notificationAPI",
		"chrome.webview.postMessage",
		"chrome.webview.addEventListener",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("BootstrapScript missing %q", want)
		}
	}
}

func TestBootstrapScriptExposesNativeConvenienceMethods(t *testing.T) {
	script := BootstrapScript()
	for _, want := range []string{
		`call("gosx.desktop.app.info"`,
		`call("gosx.desktop.app.close"`,
		`call("gosx.desktop.window.minimize"`,
		`call("gosx.desktop.window.maximize"`,
		`call("gosx.desktop.window.restore"`,
		`call("gosx.desktop.window.focus"`,
		`call("gosx.desktop.window.setTitle"`,
		`call("gosx.desktop.window.setFullscreen"`,
		`call("gosx.desktop.window.setMinSize"`,
		`call("gosx.desktop.window.setMaxSize"`,
		`call("gosx.desktop.dialog.openFile"`,
		`call("gosx.desktop.dialog.saveFile"`,
		`call("gosx.desktop.clipboard.readText"`,
		`call("gosx.desktop.clipboard.writeText"`,
		`call("gosx.desktop.shell.openExternal"`,
		`call("gosx.desktop.notification.show"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("BootstrapScript missing native method helper %q", want)
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
