package desktop

import "testing"

func TestDesktopProcessFailedKindMapping(t *testing.T) {
	want := []ProcessFailedKind{
		ProcessFailedBrowserProcessExited,
		ProcessFailedRenderProcessExited,
		ProcessFailedRenderProcessUnresponsive,
		ProcessFailedFrameRenderProcessExited,
		ProcessFailedUtilityProcessExited,
		ProcessFailedSandboxHelperProcessExited,
		ProcessFailedGPUProcessExited,
		ProcessFailedPPAPIPluginProcessExited,
		ProcessFailedPPAPIBrokerProcessExited,
		ProcessFailedUnknownProcessExited,
	}
	for value, kind := range want {
		if got := processFailedKindFromWebView2(int32(value)); got != kind {
			t.Errorf("kind(%d) = %q, want %q", value, got, kind)
		}
	}
	if got := processFailedKindFromWebView2(100); got != ProcessFailedUnknownProcessExited {
		t.Fatalf("unknown kind = %q, want %q", got, ProcessFailedUnknownProcessExited)
	}
}
