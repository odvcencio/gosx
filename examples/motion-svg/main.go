// Run: go run ./examples/motion-svg
package main

import (
	"log"
	"os"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/motion"
	"m31labs.dev/gosx/server"
)

func diagram(id string, expanded bool) gosx.Node {
	y, width, route := "105", "112", "M168 150 L312 150"
	if expanded {
		y, width, route = "76", "168", "M168 150 L240 150 L240 121 L312 121"
	}
	return gosx.El("svg", gosx.Attrs(gosx.Attr("id", id), gosx.Attr("viewBox", "0 0 600 300"), gosx.Attr("role", "img"), gosx.Attr("aria-label", "A request flowing between a client and a service")),
		gosx.El("path", gosx.Attrs(gosx.Attr("data-morph-id", "route"), gosx.Attr("d", route), gosx.Attr("fill", "none"), gosx.Attr("stroke", "#e5ae6b"), gosx.Attr("stroke-width", "3"))),
		block("client", "Client", "56", "105", "112", "#b6e0cf"),
		block("service", "Service", "312", y, width, "#e5ae6b"),
	)
}

func block(id, label, x, y, width, color string) gosx.Node {
	return gosx.El("g", gosx.Attrs(gosx.Attr("data-morph-id", id), gosx.Attr("transform", "translate("+x+" "+y+")")),
		gosx.El("rect", gosx.Attrs(gosx.Attr("width", width), gosx.Attr("height", "90"), gosx.Attr("rx", "18"), gosx.Attr("fill", color))),
		gosx.El("text", gosx.Attrs(gosx.Attr("x", "20"), gosx.Attr("y", "51"), gosx.Attr("fill", "#172721")), gosx.Text(label)),
	)
}

func page(ctx *server.Context) gosx.Node {
	p := motion.NewProgram("diagram").WithTransport(motion.TransportOptions{
		Duration: 2 * time.Second, Labels: map[string]time.Duration{"start": 0, "middle": time.Second, "end": 2 * time.Second},
	})
	progress := p.Map("progress", p.Playhead("clock"), 0, 2, 0, 1)
	p.BindSVGMorph(progress, "#before", "#after")
	controls := []any{gosx.Attrs(gosx.Attr("class", "controls"), gosx.Attr("role", "group"), gosx.Attr("aria-label", "Diagram playback"))}
	for _, button := range []struct {
		action      server.MotionTransportAction
		label, text string
	}{
		{server.MotionSeek, "start", "Before"}, {server.MotionSeek, "middle", "Halfway"}, {server.MotionSeek, "end", "After"},
		{server.MotionPlay, "", "Play"}, {server.MotionPause, "", "Pause"}, {server.MotionReverse, "", "Reverse"},
	} {
		controls = append(controls, server.MotionControl(server.MotionControlProps{Transport: p.ID, Action: button.action, Label: button.label}, gosx.Text(button.text)))
	}
	body := ctx.Runtime().MotionScope(p,
		gosx.El("main",
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "eyebrow")), gosx.Text("GoSX / SVG motion")),
			gosx.El("h1", gosx.Text("The shape of a system.")),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "intro")), gosx.Text("A service grows. Its identity stays. Seek any moment, then trace the change backward.")),
			gosx.El("div", gosx.Attrs(gosx.Attr("hidden", true), gosx.Attr("aria-hidden", "true")), diagram("before", false)),
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "canvas")), diagram("after", true)),
			gosx.El("div", controls...),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "note")), gosx.Text("Go-authored snapshots · One shared playhead · No application JavaScript"))),
	)
	return server.HTMLDocument(&server.DocumentContext{Title: "SVG motion — GoSX", Head: gosx.El("style", gosx.RawHTML(style)), Body: body})
}

func main() {
	app := server.New()
	app.Page("GET /", page)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(app.ListenAndServe("127.0.0.1:" + port))
}

const style = `
*{box-sizing:border-box}body{margin:0;background:#101b19;color:#e9efe9;font-family:system-ui,sans-serif}
main{width:min(880px,100%);margin:auto;padding:clamp(24px,6vw,72px) clamp(20px,5vw,56px)}
.eyebrow{color:#b6e0cf;letter-spacing:.16em;text-transform:uppercase;font-size:12px;margin:0 0 36px}
h1{font:clamp(36px,7vw,64px)/1.06 Georgia,serif;letter-spacing:-.04em;margin:0 0 20px;max-width:620px}
.intro{max-width:510px;font-size:18px;line-height:1.7;color:#aebeb6}.canvas{border:1px solid #344840;border-radius:22px;background:#162720;margin:36px 0 24px;overflow:hidden}
svg{display:block;width:100%;height:auto}svg text{font:16px system-ui,sans-serif}.controls{display:flex;flex-wrap:wrap;gap:9px}
button{min-height:46px;padding:10px 18px;background:#b6e0cf;color:#14251d;border:0;border-radius:24px;font:inherit;cursor:pointer}
button:focus-visible{outline:3px solid #e5ae6b;outline-offset:4px}button:disabled{opacity:.45;cursor:default}.note{font-size:12px;line-height:1.8;color:#91a69a;margin-top:24px}
@media(prefers-reduced-motion:reduce){*{scroll-behavior:auto}}
`
