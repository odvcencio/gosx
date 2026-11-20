package hosttest

import "m31labs.dev/gosx/game/host"

// The wire types live in package host (game/host/padtest_protocol.go), because
// the browser adapter inside host must share them and host cannot import this
// package: hosttest imports host for GamepadState. They are re-exported here
// so a test driver needs one import.
type (
	// Message is every message of the protocol; see host.PadMessage.
	Message = host.PadMessage
	// Pad is one pad on the wire.
	Pad = host.PadWire
)

// Message types, At values and rejection reasons.
const (
	ProtocolVersion = host.PadTestProtocolVersion
	Path            = host.PadTestPath
	QueryField      = host.PadTestQueryField
	MaxMessage      = host.PadTestMaxMessage

	MsgHello    = host.PadMsgHello
	MsgReady    = host.PadMsgReady
	MsgSet      = host.PadMsgSet
	MsgApplied  = host.PadMsgApplied
	MsgRejected = host.PadMsgRejected
	MsgWatch    = host.PadMsgWatch
	MsgProgress = host.PadMsgProgress

	AtNext  = host.PadAtNext
	AtFrame = host.PadAtFrame

	RejectLate     = host.PadRejectLate
	RejectMissed   = host.PadRejectMissed
	RejectSequence = host.PadRejectSequence
	RejectInvalid  = host.PadRejectInvalid
	RejectLost     = host.PadRejectLost
)
