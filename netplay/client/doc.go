// Package client is the browser (Go WASM) side of GoSX netplay transport.
//
// Dial returns a netplay.Transport that prefers a WebRTC data channel and
// falls back to hub WebSocket binary frames when WebRTC does not connect. The
// app writes no JavaScript: the page asks for the "netplay" runtime feature
// (island Renderer.RequireFeature) and the GoSX runtime supplies the browser
// binding.
package client
