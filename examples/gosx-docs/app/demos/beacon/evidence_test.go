package docs

import (
	"testing"

	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/preview"
)

func TestBlackglassCoastProducesHeadlessRenderEvidence(t *testing.T) {
	props := BlackglassBeaconProgram()
	for _, viewport := range []struct {
		name          string
		width, height int
	}{
		{name: "desktop", width: 1280, height: 720},
		{name: "mobile", width: 390, height: 844},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			session := harness.New(props, preview.Options{
				Width: viewport.width, Height: viewport.height, Background: props.Background, MaxSegments: 24,
			})
			if _, err := session.Render(0); err != nil {
				t.Fatalf("render coast: %v", err)
			}
			report := session.Report()
			if len(report.Events) == 0 || report.Events[0].Frame == nil {
				t.Fatalf("coast produced no frame evidence: %+v", report.Events)
			}
			frame := report.Events[0].Frame
			if frame.Width != viewport.width || frame.Height != viewport.height {
				t.Fatalf("frame size = %dx%d, want %dx%d", frame.Width, frame.Height, viewport.width, viewport.height)
			}
			if frame.Coverage < 0.04 || frame.VisibleBounds == nil {
				t.Fatalf("coast frame is blank or under-framed: %+v", frame)
			}
			if frame.UniqueColors < 32 || frame.LuminanceVariance <= 0 || frame.EdgeEnergy <= 0 {
				t.Fatalf("coast frame lacks production visual evidence: %+v", frame)
			}
			if frame.DeviceLost {
				t.Fatal("headless renderer reported device loss")
			}
			if err := session.Validate(); err != nil {
				t.Fatalf("validate coast harness: %v", err)
			}
		})
	}
}
