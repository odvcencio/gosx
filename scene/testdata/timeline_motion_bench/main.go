// Exports real Go-authored motion graphs and independently sampled native poses
// for client/js/timeline-motion.bench.mjs. Not part of an application runtime.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"m31labs.dev/gosx/motion"
	"m31labs.dev/gosx/scene"
)

type sample struct {
	Seconds float64 `json:"seconds"`
	Value   float64 `json:"value"`
}

type fixture struct {
	N       int             `json:"n"`
	Program json.RawMessage `json:"program"`
	Samples []sample        `json:"samples"`
}

func main() {
	var fixtures []fixture
	for _, n := range []int{1, 2, 8, 16, 64, 256, 1024} {
		plan := scene.NewTimeline("sequence")
		for i := 0; i < n; i++ {
			plan.At(time.Duration(i)*time.Second, scene.Tween{
				Node: "piece", Property: "x", From: float64(i), To: float64(i + 1), Duration: time.Second,
			})
		}
		program := motion.NewProgram(fmt.Sprintf("sequence-%d", n)).WithTransport(motion.TransportOptions{Duration: plan.Duration()})
		if err := plan.BindMotion(program, program.Playhead("clock"), "#scene"); err != nil {
			panic(err)
		}
		wire, err := program.Marshal()
		if err != nil {
			panic(err)
		}
		f := fixture{N: n, Program: wire}
		for _, at := range []float64{0, float64(n)/2 + .125, float64(n)/2 + .25, float64(n) - .001, float64(n), .25} {
			commands, err := plan.Sample(at)
			if err != nil {
				panic(err)
			}
			f.Samples = append(f.Samples, sample{at, commands[0].Data.(map[string]float64)["x"]})
		}
		fixtures = append(fixtures, f)
	}
	if err := json.NewEncoder(os.Stdout).Encode(fixtures); err != nil {
		panic(err)
	}
}
