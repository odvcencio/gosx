// Run: go run ./examples/motion-shader
// All application behavior is Go and Selena. Playback uses GoSX constructs.
package main

import (
	_ "embed"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

//go:embed tide.sel
var shader []byte

//go:embed style.css
var style string

func main() {
	material, _, err := scene.CompileSelenaMaterial(shader, scene.SelenaMaterialOptions{})
	if err != nil {
		log.Fatal(err)
	}
	flat, err := (scene.ShaderSurface{Material: material, MeshID: "ribbon", Label: "Animated tide ribbons"}).EngineConfig()
	if err != nil {
		log.Fatal(err)
	}
	form, err := (scene.ShaderSurface{Material: material, Shape: scene.ShaderSphere, MeshID: "form", Label: "The same material on a sphere"}).EngineConfig()
	if err != nil {
		log.Fatal(err)
	}
	flat.MountID, form.MountID = "tide", "form"
	app := server.New()
	app.Page("GET /", func(ctx *server.Context) gosx.Node { return page(ctx, flat, form) })
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Motion shader example: http://localhost:%s", port)
	log.Fatal(app.ListenAndServe("127.0.0.1:" + port))
}

func page(ctx *server.Context, flat, form engine.Config) gosx.Node {
	program := motion.NewProgram("tide-story").WithTransport(motion.TransportOptions{
		Duration: 14 * time.Second, AutoPlay: true, Labels: map[string]time.Duration{"bloom": 5 * time.Second},
		Scenes: []string{"#tide", "#form"},
	})
	clock := program.Playhead("clock")
	key := func(at, value float64) motion.Key { return motion.Key{T: at, Value: motion.ScalarV(value)} }
	sample := func(id string, ease motion.Ease, keys ...motion.Key) motion.SignalRef {
		ref, err := program.SampleTrack(id, clock, motion.Track{Ease: ease, Keys: keys})
		if err != nil {
			log.Fatal(err)
		}
		return ref
	}
	soft := motion.Ease{Kind: motion.EaseInOutPow, Args: []float64{2}}
	gain := sample("gain", soft, key(0, .35), key(5, .95), key(9, .45), key(14, .6))
	warmth := sample("warmth", soft, key(0, 0), key(7, 1), key(14, .18))
	progress := sample("pose", soft, key(0, 0), key(5, .85), key(9, .25), key(14, 1))
	entrances := motion.ExpandStagger(motion.Track{
		Ease: motion.Ease{Kind: motion.EaseOutBack, Args: []float64{.8}},
		Keys: []motion.Key{key(0, 32), key(1.2, 0)},
	}, []int{0, 1, 2, 3}, motion.StaggerSpec{From: motion.FromFirst, Delay: .14})
	for i, selector := range []string{".hero-copy .eyebrow", ".headline-first", ".headline-second", ".hero-caption"} {
		id := fmt.Sprintf("entrance-%d", i)
		rise, err := program.SampleTrack(id, clock, entrances[i])
		if err != nil {
			log.Fatal(err)
		}
		program.BindStyle(rise, selector, "transform.y", "px")
		fade := program.Map(id+"-fade", rise, 32, 0, 0, 1)
		program.BindStyle(program.Clamp(id+"-visible", fade, 0, 1), selector, "opacity", "")
	}
	sway := sample("sway", soft, key(0, -.45), key(5, .5), key(9, -.25), key(14, .15))
	lift := sample("lift", motion.Ease{Kind: motion.EaseInOutBack, Args: []float64{.6}}, key(0, -.25), key(5, .25), key(9, -.15), key(14, 0))
	turn := sample("turn", soft, key(0, 0), key(5, math.Pi), key(9, 1.5*math.Pi), key(14, 2*math.Pi))
	tilt := sample("tilt", soft, key(0, .6), key(5, -.3), key(9, .4), key(14, .15))
	breath := sample("breath", soft, key(0, .82), key(5, 1.04), key(9, .92), key(14, 1))
	program.BindSceneNode(sway, "#form", "form", "position.x")
	program.BindSceneNode(lift, "#form", "form", "position.y")
	program.BindSceneNode(turn, "#form", "form", "rotation.y")
	program.BindSceneNode(tilt, "#form", "form", "rotation.x")
	for _, property := range []string{"scale.x", "scale.y", "scale.z"} {
		program.BindSceneNode(breath, "#form", "form", property)
	}
	if err := program.BindSceneTransition(progress, "#form", scene.CommandTransition{
		From: []scene.Command{
			scene.SetCameraCommand(scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 10), FOV: 50}),
		},
		To: []scene.Command{
			scene.SetCameraCommand(scene.PerspectiveCamera{Position: scene.Vec3(0, .3, 8.5), FOV: 45}),
		},
	}); err != nil {
		log.Fatal(err)
	}
	program.BindMaterialUniform(gain, "#tide", "ribbon", "gain")
	program.BindMaterialUniform(gain, "#form", "form", "gain")
	program.BindMaterialUniform(warmth, "#tide", "ribbon", "warmth")
	program.BindMaterialUniform(warmth, "#form", "form", "warmth")
	for i, window := range [][2]float64{{0, 5}, {5, 9}, {9, 14}} {
		id := fmt.Sprintf("chapter-%d", i)
		fill := sample(id, motion.Ease{}, key(window[0], 0), key(window[1], 100))
		program.BindStyle(fill, "."+id+" .chapter-fill", "width", "%")
	}
	control := func(action server.MotionTransportAction, text string) gosx.Node {
		return server.MotionControl(server.MotionControlProps{Transport: program.ID, Action: action}, gosx.Text(text))
	}
	body := ctx.Runtime().MotionScope(program, gosx.Attrs(gosx.Attr("class", "page")),
		gosx.El("header", gosx.El("a", gosx.Attrs(gosx.Attr("href", "https://gosx.m31labs.dev/docs/motion/")), gosx.Text("GoSX / Motion studies")), gosx.El("span", gosx.Text("01 — Tidal forms"))),
		gosx.El("main",
			gosx.El("section", gosx.Attrs(gosx.Attr("class", "hero")),
				ctx.Engine(flat, gosx.El("p", gosx.Text("A quiet sea of teal ribbons."))),
				gosx.El("div", gosx.Attrs(gosx.Attr("class", "hero-copy")),
					gosx.El("p", gosx.Attrs(gosx.Attr("class", "eyebrow")), gosx.Text("Go · GSX · Selena")),
					gosx.El("h1", gosx.El("span", gosx.Attrs(gosx.Attr("class", "headline-first")), gosx.Text("A little rhythm.")), gosx.El("span", gosx.Attrs(gosx.Attr("class", "headline-second")), gosx.Text("A world of motion."))),
					gosx.El("p", gosx.Attrs(gosx.Attr("class", "hero-caption")), gosx.Text("A slow breath of light. A turn toward the sun. Everything moves to the same moment.")),
				),
			),
			gosx.El("section", gosx.Attrs(gosx.Attr("class", "study")),
				gosx.El("div", gosx.Attrs(gosx.Attr("class", "form")), ctx.Engine(form, gosx.El("p", gosx.Text("The same ribbons, wrapped around a sphere.")))),
				gosx.El("div", gosx.Attrs(gosx.Attr("class", "study-copy")),
					gosx.El("p", gosx.Attrs(gosx.Attr("class", "eyebrow")), gosx.Text("Move through the moment")),
					gosx.El("h2", gosx.Text("Arrive. Bloom. Exhale.")),
					gosx.El("p", gosx.Text("The form sways, turns and breathes as teal ribbons warm to amber. Pause anywhere, or follow the same moment back.")),
					gosx.El("div", gosx.Attrs(gosx.Attr("class", "chapters"), gosx.Attr("aria-hidden", "true")),
						chapter("chapter-0", "01", "Arrive"), chapter("chapter-1", "02", "Bloom"), chapter("chapter-2", "03", "Exhale")),
					gosx.El("div", gosx.Attrs(gosx.Attr("class", "controls"), gosx.Attr("role", "group"), gosx.Attr("aria-label", "Motion playback")),
						control(server.MotionPlay, "Play"), control(server.MotionPause, "Pause"), control(server.MotionReverse, "Reverse"),
						server.MotionControl(server.MotionControlProps{Transport: program.ID, Action: server.MotionSeek, Label: "bloom"}, gosx.Text("The bloom · 5s")),
						control(server.MotionReplay, "Back to start")),
					gosx.El("p", gosx.Attrs(gosx.Attr("class", "note")), gosx.Text("Keyboard friendly. Reduced motion respected. No application JavaScript.")),
				),
			),
		),
		gosx.El("footer", gosx.Text("Two dimensions. Three dimensions. One framework.")),
	)
	return server.HTMLDocument(&server.DocumentContext{Title: "Tidal forms — GoSX motion", Head: gosx.El("style", gosx.RawHTML(style)), Body: body})
}

func chapter(class, number, name string) gosx.Node {
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", class)),
		gosx.El("span", gosx.Text(number+" / "+name)),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "chapter-track")), gosx.El("span", gosx.Attrs(gosx.Attr("class", "chapter-fill")))))
}
