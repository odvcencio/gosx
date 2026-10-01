// Command instance-stream-go-writer-fixture writes real
// scene.InstanceStreamFrame.Encode() output as base64 JSON on stdout, so the
// JS decoder (client/runtime/scene3d/instance-stream.ts) can be tested
// against bytes the real Go encoder produced instead of a JS-side
// reimplementation of the wire format that could share the same bug.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"m31labs.dev/gosx/scene"
)

type fixture struct {
	Name  string `json:"name"`
	Bytes string `json:"bytes"`
}

func main() {
	frames := []struct {
		name  string
		frame scene.InstanceStreamFrame
	}{
		{
			// A 12-byte id: already 4-byte aligned, so the encoder adds no id
			// padding. Two instances of InstanceStreamTransform (16 floats each).
			name: "aligned-id-transform",
			frame: scene.InstanceStreamFrame{
				BatchID:  "crowd-actors",
				Revision: 3,
				Kind:     scene.InstanceStreamTransform,
				Count:    2,
				Data:     sequentialFloats(2 * scene.InstanceStreamTransform.Stride()),
			},
		},
		{
			// A 2-byte id: NOT 4-byte aligned, so the JS decoder must skip the
			// encoder's 2 zero pad bytes to find the payload.
			name: "padded-id-transform-color",
			frame: scene.InstanceStreamFrame{
				BatchID:  "ab",
				Revision: 9001,
				Kind:     scene.InstanceStreamTransformColor,
				Count:    1,
				Data:     sequentialFloats(scene.InstanceStreamTransformColor.Stride()),
			},
		},
		{
			// Zero instances: a valid, empty frame (a batch that streams but
			// currently has nothing to draw).
			name: "zero-count-skinned-pose",
			frame: scene.InstanceStreamFrame{
				BatchID:  "crowd",
				Revision: 1,
				Kind:     scene.InstanceStreamSkinnedPose,
				Count:    0,
				Data:     nil,
			},
		},
	}

	out := make([]fixture, 0, len(frames))
	for _, f := range frames {
		encoded, err := f.frame.Encode()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		out = append(out, fixture{Name: f.name, Bytes: base64.StdEncoding.EncodeToString(encoded)})
	}

	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func sequentialFloats(n int) []float32 {
	data := make([]float32, n)
	for i := range data {
		data[i] = float32(i) * 0.5
	}
	return data
}
