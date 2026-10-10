//go:build !js || !wasm

package browser_test

import (
	"testing"

	"m31labs.dev/gosx/client/browser"
)

func TestInstantiateTemplateNativeStub(t *testing.T) {
	for _, doc := range []browser.Document{{}, browser.CurrentDocument()} {
		if doc.InstantiateTemplate("row-template").Valid() {
			t.Fatal("native document returned a browser element")
		}
	}
}
