package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/examples/gosx-docs/app/demos/checkers"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/preview"
)

func main() {
	out := flag.String("out", "checkers-native-preview.png", "output PNG path")
	width := flag.Int("width", 960, "image width")
	height := flag.Int("height", 600, "image height")
	material := flag.String("material", "carved-wood", "board material family")
	telemetry := flag.String("telemetry", "", "output harness JSON (default: PNG name with .telemetry.json)")
	flag.Parse()

	props := checkers.ShowcaseSceneWithMaterial(*material)
	session := harness.New(props, preview.Options{
		Width: *width, Height: *height, Time: 0, DisableShadows: true, DisablePostFX: true, MaxSegments: 32,
	})
	result, err := session.Render(0)
	if err != nil {
		fatal(err)
	}
	center := session.Trace("center socket", scene.Ray{Origin: scene.Vec3(0, 5, 0), Direction: scene.Vec3(0, -1, 0)}, scene.PickableOnly())
	if center.Closest == nil || center.Closest.ID != "checkers-sockets" || center.Closest.InstanceIndex == nil {
		fatal(fmt.Errorf("center picking probe missed an exact socket: %#v", center.Closest))
	}
	corner := session.Trace("round-board corner rejection", scene.Ray{Origin: scene.Vec3(4.2, 5, 4.2), Direction: scene.Vec3(0, -1, 0)}, scene.PickableOnly())
	if corner.Closest != nil {
		fatal(fmt.Errorf("corner rejection probe produced false hit: %#v", corner.Closest))
	}
	if err := session.Validate(); err != nil {
		fatal(err)
	}
	if parent := filepath.Dir(*out); parent != "." {
		if err := os.MkdirAll(parent, 0755); err != nil {
			fatal(err)
		}
	}
	file, err := os.Create(*out)
	if err != nil {
		fatal(err)
	}
	if err := preview.WritePNG(file, result); err != nil {
		_ = file.Close()
		fatal(err)
	}
	if err := file.Close(); err != nil {
		fatal(err)
	}
	reportPath := *telemetry
	if reportPath == "" {
		ext := filepath.Ext(*out)
		reportPath = strings.TrimSuffix(*out, ext) + ".telemetry.json"
	}
	report, err := os.Create(reportPath)
	if err != nil {
		fatal(err)
	}
	if err := session.WriteJSON(report); err != nil {
		_ = report.Close()
		fatal(err)
	}
	if err := report.Close(); err != nil {
		fatal(err)
	}
	fmt.Printf("rendered %s with %s (%d batches, %d materials)\n", *out, reportPath, len(result.Bundle.InstancedMeshes), len(result.Bundle.Materials))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "checkers preview:", err)
	os.Exit(1)
}
