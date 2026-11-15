package docs

import (
	"testing"

	"m31labs.dev/gosx/scene/selenabuild"
)

// pulse.sel.json must match pulse.sel and the linked Selena compiler. Rebuild
// it with: gosx selena build examples/gosx-docs/app/test/motion-material
func TestPulseArtifactMatchesSource(t *testing.T) {
	if err := selenabuild.Check(".", selenabuild.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := motionMaterialFixture(); err != nil {
		t.Fatal(err)
	}
}
