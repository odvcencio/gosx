package server

import "m31labs.dev/gosx"

// MotionControlProps describes a semantic button for a MotionScope transport.
// Label is the named position for a seek action; children supply button text.
type MotionControlProps struct {
	Transport string
	Action    MotionTransportAction
	Label     string
}

type MotionTransportAction string

const (
	MotionPlay    MotionTransportAction = "play"
	MotionPause   MotionTransportAction = "pause"
	MotionReverse MotionTransportAction = "reverse"
	MotionReplay  MotionTransportAction = "replay"
	MotionSeek    MotionTransportAction = "seek"
)

// MotionControl uses GoSX's delegated motion runtime. Authors need no browser
// script or island for standard playback controls. Without enhancement it is
// a disabled button and the scope's semantic HTML remains readable.
func MotionControl(props MotionControlProps, children ...any) gosx.Node {
	attrs := gosx.Attrs(
		gosx.Attr("type", "button"), gosx.Attr("disabled", true),
		gosx.Attr("data-gosx-transport", props.Transport),
		gosx.Attr("data-gosx-transport-action", string(props.Action)),
	)
	if props.Label != "" {
		attrs = append(attrs, gosx.Attr("data-gosx-transport-label", props.Label))
	}
	return gosx.El("button", append([]any{attrs}, children...)...)
}
