package desktop

type webView2SettingsPolicy struct {
	browserAcceleratorKeys bool
	zoomControl            bool
	statusBar              bool
}

func settingsPolicy(debug bool) webView2SettingsPolicy {
	return webView2SettingsPolicy{
		browserAcceleratorKeys: debug,
		zoomControl:            debug,
		statusBar:              debug,
	}
}
