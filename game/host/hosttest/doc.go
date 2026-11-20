// Package hosttest drives a page's gamepads from a native Go test.
//
// A test starts a Server, opens the page at Server.PageURL, and then plugs
// pads, presses buttons and waits on frames through the Server's methods. The
// page runs the game unchanged: it reads pads through a host.GamepadSource
// that the Server feeds, so the game's own deadzones, button edges and
// bindings run as they do with a real pad.
//
// # Contract
//
// The Server listens on 127.0.0.1 only, on a port the system picks, and makes
// a new random token for each run. PageURL adds one query field to a page URL:
// gosx-padtest=<port>.<token>, where the token is 32 lowercase hex characters.
// Other query fields and the fragment are kept. The page derives the
// WebSocket address (ws://127.0.0.1:<port>/gosx-padtest) itself and never
// accepts an address from the URL. The token is the only authentication: a
// file page has an opaque origin, so the Origin header proves nothing.
//
// One page may connect. It sends hello with the token within the handshake
// timeout (5 seconds unless Options changes it). A second page, a wrong token
// and a message above 64 KiB are refused. When the page's connection ends the
// Server shuts down; a Server serves one page for one run.
//
// The page applies pad changes at engine-frame boundaries. A change received
// between frames waits for the next frame to begin, so every read inside one
// frame sees the same pads. The page applies at most one change per frame: a
// press and its release land on two frames, so a game that polls each frame
// sees both edges. Every change is a complete snapshot of the connected pads,
// so a change to two pads is one change. Slots keep their index: unplugging
// slot 0 leaves slot 1 as index 1.
//
// Each method that changes pads waits for the page's receipt. The page sends a
// receipt at the end of the first frame that read the pads after the change
// landed. A socket receive is never a receipt. Mutating methods run one at a
// time. A Receipt holds the change's sequence number, the frame that carried
// the receipt, and the counts of drawn frames and frames with a pad read.
//
// SetAt applies a change exactly at an engine frame. If that frame has begun
// the call fails with *LateFrameError, which names the current frame; the
// change is never applied a frame late. If the frame passes with no pad read,
// the call returns the receipt and ErrFrameNotSampled.
//
// To reconnect a pad with the same ID, call Unplug and then Plug. A game that
// tracks pads by ID cannot tell a same-ID replacement from the original unless
// it polls the slot empty in between, and the Unplug receipt proves it did.
//
// WaitFrames counts frames from the moment the page receives the request, so a
// stale Frames value cannot skew it. WaitSampled takes an absolute threshold
// (the since receipt's Sampled count plus n), so a receipt that was already
// in flight cannot cost it a wake-up. Progress asks the page for its counters
// and waits for the answer. Every wait ends with the caller's context. A page
// that draws no frames, such as a hidden tab, makes a wait time out; the error
// names the last frame, the sent and applied sequence numbers and the state of
// the connection. When the page's connection ends, every pending operation
// fails with ErrPageGone.
//
// If a context ends before a change is acknowledged, the change may still
// land later. The Server then keeps the pad state it sent as the state to edit
// next. A rejected change is reverted.
//
// # Testing without a browser
//
// The page side of the protocol is plain Go in package host
// (host.PadTestSource and host.PadTestSession), so this package's own tests
// run a Go page over a real loopback WebSocket with a hand-stepped frame
// loop, and feed its frames through actions.PadPoller. See fakepage_test.go.
//
// The wire types live in package host and are aliased in protocol.go: the
// browser adapter inside host cannot import this package, because this
// package imports host.
package hosttest
