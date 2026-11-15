package livesim

import "m31labs.dev/gosx/hub"

// Hub is the package-level hub that main.go mounts at /demos/livesim/ws. It is
// declared on every platform so main.go compiles in a js/wasm build of ./...;
// page.server.go creates it and runs the simulation on it in server builds.
var Hub *hub.Hub
