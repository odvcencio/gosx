package docs

import (
	"testing"

	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/preview"
)

func TestBlackglassCoastProducesHeadlessRenderEvidence(t *testing.T) {
	props := BlackglassBeaconProgram()
	session := harness.New(props, preview.Options{
		Width: 960, Height: 540, Background: props.Background, DisablePostFX: true, MaxSegments: 24,
	})
	if _, err := session.Render(0); err != nil {
		t.Fatalf("render coast: %v", err)
	}
	report := session.Report()
	if len(report.Events) == 0 || report.Events[0].Frame == nil || report.Events[0].Frame.Coverage < 0.04 {
		t.Fatalf("coast frame evidence is blank or under-framed: %+v", report.Events)
	}
	if err := session.Validate(); err != nil {
		t.Fatalf("validate coast harness: %v", err)
	}
}
