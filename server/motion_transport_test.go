package server

import (
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/motion"
	"m31labs.dev/gosx/scene"
)

func TestMotionTransportRequiresOnlyItsOptionalFeature(t *testing.T) {
	app := New()
	app.Page("GET /motion", func(ctx *Context) gosx.Node {
		program := motion.NewProgram("story").WithTransport(motion.TransportOptions{Duration: 2 * time.Second})
		return ctx.Runtime().MotionScope(program, gosx.El("h1", gosx.Text("Readable without animation")))
	})
	app.Page("GET /plain", func(ctx *Context) gosx.Node {
		return ctx.Runtime().MotionScope(motion.NewProgram("plain"), gosx.Text("Ordinary motion"))
	})
	app.Page("GET /track", func(ctx *Context) gosx.Node {
		program := motion.NewProgram("track")
		if _, err := program.SampleTrack("rise", program.Hover("hover", ".title"), motion.Track{Keys: []motion.Key{
			{T: 0, Value: motion.ScalarV(24)}, {T: 1, Value: motion.ScalarV(0)},
		}}); err != nil {
			t.Fatal(err)
		}
		return ctx.Runtime().MotionScope(program, gosx.Text("Readable track fallback"))
	})
	app.Page("GET /svg", func(ctx *Context) gosx.Node {
		program := motion.NewProgram("diagram")
		program.BindSVGMorph(program.Hover("progress", "#diagram"), "#before", "#after")
		return ctx.Runtime().MotionScope(program, gosx.Text("Readable SVG fallback"))
	})
	app.Page("GET /scene", func(ctx *Context) gosx.Node {
		program := motion.NewProgram("scene")
		if err := program.BindSceneTransition(program.Hover("progress", "#form"), "#form", scene.CommandTransition{}); err != nil {
			t.Fatal(err)
		}
		return ctx.Runtime().MotionScope(program, gosx.Text("Readable scene fallback"))
	})
	app.Page("GET /bindings", func(ctx *Context) gosx.Node {
		program := motion.NewProgram("bindings")
		value := program.Hover("hover", ".title")
		program.BindSceneNode(value, "#form", "form", "position.x")
		program.BindMaterialUniform(value, "#form", "form", "warmth")
		return ctx.Runtime().MotionScope(program, gosx.Text("Readable scene binding fallback"))
	})
	contract := renderDocumentContract(t, app.Build(), "/motion")
	if !strings.Contains(contract, `"bootstrapFeatureMotionTransportPath":"/gosx/bootstrap-feature-motion-transport.js"`) {
		t.Fatalf("missing optional transport chunk: %s", contract)
	}
	if strings.Contains(renderDocumentContract(t, app.Build(), "/plain"), "MotionTransportPath") {
		t.Fatal("ordinary motion must not load transport code")
	}
	track := renderDocumentContract(t, app.Build(), "/track")
	if !strings.Contains(track, `"bootstrapFeatureMotionTrackPath":"/gosx/bootstrap-feature-motion-track.js"`) || strings.Contains(track, "MotionTransportPath") || strings.Contains(track, "MotionScenePath") {
		t.Fatalf("scalar tracks must require only their own optional feature: %s", track)
	}
	if strings.Contains(contract, "MotionTrackPath") || strings.Contains(renderDocumentContract(t, app.Build(), "/plain"), "MotionTrackPath") {
		t.Fatal("existing pages must not load scalar track code")
	}
	bindings := renderDocumentContract(t, app.Build(), "/bindings")
	if !strings.Contains(bindings, "bootstrapFeatureMotionTrackPath") || strings.Contains(bindings, "MotionTransportPath") {
		t.Fatal("ordinary scene node and material bindings need their optional adapters, without a transport")
	}
	svg := renderDocumentContract(t, app.Build(), "/svg")
	if !strings.Contains(svg, `"bootstrapFeatureMotionSvgPath":"/gosx/bootstrap-feature-motion-svg.js"`) || strings.Contains(svg, "MotionTransportPath") {
		t.Fatalf("SVG morphing must require only its own optional feature: %s", svg)
	}
	pose := renderDocumentContract(t, app.Build(), "/scene")
	if !strings.Contains(pose, `"bootstrapFeatureMotionScenePath":"/gosx/bootstrap-feature-motion-scene.js"`) || strings.Contains(pose, "MotionTransportPath") || strings.Contains(pose, "MotionSvgPath") {
		t.Fatalf("scene transitions must require only their own optional feature: %s", pose)
	}
}

func TestMotionControlIsSemanticAndSafeBeforeEnhancement(t *testing.T) {
	html := gosx.RenderHTML(MotionControl(MotionControlProps{Transport: "story", Action: MotionSeek, Label: "arrival"}, gosx.Text("Arrival")))
	for _, want := range []string{`<button`, `type="button"`, `disabled`, `data-gosx-transport="story"`, `data-gosx-transport-action="seek"`, `data-gosx-transport-label="arrival"`, `Arrival</button>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q: %s", want, html)
		}
	}
}
