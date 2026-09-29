package desktop

// PermissionKind names a browser permission a page asks for.
type PermissionKind string

const (
	PermissionUnknown           PermissionKind = "unknown"
	PermissionMicrophone        PermissionKind = "microphone"
	PermissionCamera            PermissionKind = "camera"
	PermissionGeolocation       PermissionKind = "geolocation"
	PermissionNotifications     PermissionKind = "notifications"
	PermissionOtherSensors      PermissionKind = "other-sensors"
	PermissionClipboardRead     PermissionKind = "clipboard-read"
	PermissionMultipleDownloads PermissionKind = "multiple-downloads"
	PermissionFileReadWrite     PermissionKind = "file-read-write"
	PermissionAutoplay          PermissionKind = "autoplay"
	PermissionLocalFonts        PermissionKind = "local-fonts"
	// PermissionMIDISysex is how WebView2 reports Web MIDI requests. With
	// the 153 runtime, requestMIDIAccess({sysex: false}) also arrives as
	// this kind, so apps that use Web MIDI answer it.
	PermissionMIDISysex         PermissionKind = "midi-sysex"
	PermissionWindowManagement  PermissionKind = "window-management"
	PermissionPersistentStorage PermissionKind = "persistent-storage"
)

// PermissionDecision answers a permission request.
type PermissionDecision string

const (
	// PermissionAsk leaves the decision to the browser, which shows its
	// own prompt (the default without a handler).
	PermissionAsk PermissionDecision = ""
	// PermissionAllow grants the request without a prompt.
	PermissionAllow PermissionDecision = "allow"
	// PermissionDeny refuses the request without a prompt.
	PermissionDeny PermissionDecision = "deny"
)

// PermissionRequest describes one permission request from page content.
type PermissionRequest struct {
	Kind PermissionKind
	// URI is the page or frame origin that asked.
	URI string
	// UserInitiated reports whether a user gesture started the request.
	UserInitiated bool
}

// webView2PermissionKinds maps COREWEBVIEW2_PERMISSION_KIND values
// (WebView2 SDK 1.0.4191.47) to PermissionKind.
var webView2PermissionKinds = [...]PermissionKind{
	PermissionUnknown,
	PermissionMicrophone,
	PermissionCamera,
	PermissionGeolocation,
	PermissionNotifications,
	PermissionOtherSensors,
	PermissionClipboardRead,
	PermissionMultipleDownloads,
	PermissionFileReadWrite,
	PermissionAutoplay,
	PermissionLocalFonts,
	PermissionMIDISysex,
	PermissionWindowManagement,
	PermissionPersistentStorage,
}

func permissionKindFromWebView2(value int32) PermissionKind {
	if value < 0 || int(value) >= len(webView2PermissionKinds) {
		return PermissionUnknown
	}
	return webView2PermissionKinds[value]
}

// webView2PermissionState maps a decision to COREWEBVIEW2_PERMISSION_STATE.
// ok is false for PermissionAsk and unknown decisions, which leave the
// request's state unchanged.
func webView2PermissionState(decision PermissionDecision) (state int32, ok bool) {
	switch decision {
	case PermissionAllow:
		return 1, true
	case PermissionDeny:
		return 2, true
	}
	return 0, false
}
