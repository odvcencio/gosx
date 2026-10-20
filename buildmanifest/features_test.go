package buildmanifest

import "testing"

func TestRuntimeURLsIncludeFeatureChunks(t *testing.T) {
	m := &Manifest{}
	m.Runtime.Features = map[string]HashedAsset{"engine-bridge": {File: "bootstrap-feature-engine-bridge.abcd1234.js", Hash: "abcd1234"}}
	got := m.RuntimeURLs("/gosx/assets").Features["engine-bridge"]
	if want := "/gosx/assets/runtime/bootstrap-feature-engine-bridge.abcd1234.js"; got != want {
		t.Fatalf("Features[engine-bridge] = %q, want %q", got, want)
	}
	if (&Manifest{}).RuntimeURLs("/gosx/assets").Features != nil {
		t.Fatal("no feature chunks must yield a nil map")
	}
}
