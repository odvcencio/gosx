package desktop

import "testing"

func TestDesktopWebView2ProductionSettingsPolicy(t *testing.T) {
	production := settingsPolicy(false)
	if production.browserAcceleratorKeys || production.zoomControl || production.statusBar {
		t.Fatalf("production settings policy = %+v, want all three controls disabled", production)
	}

	debug := settingsPolicy(true)
	if !debug.browserAcceleratorKeys || !debug.zoomControl || !debug.statusBar {
		t.Fatalf("debug settings policy = %+v, want all three controls enabled", debug)
	}
}
