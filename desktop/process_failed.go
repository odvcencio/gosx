package desktop

// ProcessFailedKind identifies the WebView2 process that exited or stopped
// responding. Values match COREWEBVIEW2_PROCESS_FAILED_KIND and can be
// compared with the ProcessFailed* constants.
type ProcessFailedKind string

const (
	ProcessFailedBrowserProcessExited       ProcessFailedKind = "browser-process-exited"
	ProcessFailedRenderProcessExited        ProcessFailedKind = "render-process-exited"
	ProcessFailedRenderProcessUnresponsive  ProcessFailedKind = "render-process-unresponsive"
	ProcessFailedFrameRenderProcessExited   ProcessFailedKind = "frame-render-process-exited"
	ProcessFailedUtilityProcessExited       ProcessFailedKind = "utility-process-exited"
	ProcessFailedSandboxHelperProcessExited ProcessFailedKind = "sandbox-helper-process-exited"
	ProcessFailedGPUProcessExited           ProcessFailedKind = "gpu-process-exited"
	ProcessFailedPPAPIPluginProcessExited   ProcessFailedKind = "ppapi-plugin-process-exited"
	ProcessFailedPPAPIBrokerProcessExited   ProcessFailedKind = "ppapi-broker-process-exited"
	ProcessFailedUnknownProcessExited       ProcessFailedKind = "unknown-process-exited"
)

func processFailedKindFromWebView2(value int32) ProcessFailedKind {
	switch value {
	case 0:
		return ProcessFailedBrowserProcessExited
	case 1:
		return ProcessFailedRenderProcessExited
	case 2:
		return ProcessFailedRenderProcessUnresponsive
	case 3:
		return ProcessFailedFrameRenderProcessExited
	case 4:
		return ProcessFailedUtilityProcessExited
	case 5:
		return ProcessFailedSandboxHelperProcessExited
	case 6:
		return ProcessFailedGPUProcessExited
	case 7:
		return ProcessFailedPPAPIPluginProcessExited
	case 8:
		return ProcessFailedPPAPIBrokerProcessExited
	case 9:
		return ProcessFailedUnknownProcessExited
	default:
		return ProcessFailedUnknownProcessExited
	}
}
