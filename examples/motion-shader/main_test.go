package main

import (
	"testing"

	"m31labs.dev/gosx/scene/selenabuild"
)

// tide.sel.json must match tide.sel and the linked Selena compiler. Rebuild it
// with: gosx selena build examples/motion-shader
func TestTideArtifactMatchesSource(t *testing.T) {
	if err := selenabuild.Check(".", selenabuild.Options{}); err != nil {
		t.Fatal(err)
	}
}
