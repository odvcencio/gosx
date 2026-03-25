# Changelog

## v0.1.0

- formalized the initial GoSX release line with a repo-level `gosx.Version`
- completed the zero-manual-wiring island path for compiled `.gsx` islands
- added build-manifest loading for hashed runtime and island assets
- hardened actions, hubs, server timeouts, HTML escaping, and build failure handling
- added repeatable repo tooling with `make test`, `make test-race`, `make test-wasm`, `make build-runtime`, and `make ci`
- added CI coverage for format checks, race tests, js/wasm runtime tests, CLI build, and WASM runtime build
- added js/wasm runtime tests that compile `.gsx` islands, hydrate them through `__gosx_hydrate`, dispatch via `__gosx_action`, and assert the client patch stream
