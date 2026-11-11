# GoSX Modules

This repository contains independently versioned Go modules:

- `m31labs.dev/gosx`: the core framework.
- `m31labs.dev/gosx/editor`: the optional browser editor shell and assets.
- `m31labs.dev/gosx/netplay/webrtc`: the WebRTC transport for netplay. It
  keeps the Pion WebRTC dependencies out of the core module.
- `m31labs.dev/gosx/game/teamai/arbiterrules`: compiles Arbiter `.arb` rule
  files into team AI rule bundles at build time. It keeps the Arbiter parser
  out of the core module and out of js/wasm builds.
- `m31labs.dev/gosx/cmd/buildbootstrap`: the maintainer tool that builds the
  client bootstrap bundles (`go run ./cmd/buildbootstrap`). It has its own
  module so its bundler dependencies (esbuild, minifiers) stay out of the core
  module.
- `build/` holds a `go.mod` that is a boundary marker, not an application
  module. It keeps generated build output out of `go test ./...` and
  `go list ./...` patterns. Keep it.

Markdown++ rendering is the canonical content-source layer for the core
`content` package through `github.com/odvcencio/mdpp`. The framework owns the
collection-loading contract: mdpp handles parsing/rendering, typed frontmatter,
diagnostics, and renderer options, while applications can still import mdpp
directly for lower-level renderer control.
