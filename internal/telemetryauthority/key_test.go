package telemetryauthority

import "testing"

func TestOpaqueAuthority(t *testing.T) {
	if (Key{}).Valid() || (Key{proof: &token{marker: 1}}).Valid() {
		t.Fatal("forged capability")
	}
	if !New().Valid() {
		t.Fatal("framework capability rejected")
	}
}
