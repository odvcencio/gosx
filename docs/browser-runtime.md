# GoSX in the browser

GoSX applications can be written in Go and GSX without setting up an
application-side JavaScript toolchain. Islands execute both JavaScript and
WebAssembly in the browser; other enhancements may use JavaScript without an
island VM. The browser host is framework code authored mainly in TypeScript;
the shared island VM is Go compiled to WASM.

The server renders the initial HTML and supplies the data and program assets
needed by each island. In the browser, the host loads and verifies the selected
WASM runtime, dispatches browser events to it, and applies its DOM patches. The
Go VM evaluates island expressions and state. The TypeScript host also owns
browser-specific features such as the WebGPU and WebGL scene renderers, where
the browser APIs themselves are JavaScript APIs. An application still owns
authorization and must not put secrets into island props.

The compiler, host, and runtime share a versioned contract. A build checks the
project's runtime source against the CLI and checks prebuilt runtime evidence
before staging it. At startup, the loader verifies the asset hash and checks
the runtime handshake, feature mask, and ABI identity before releasing
hydration. A failure is reported as a runtime issue and leaves the server
rendering as the fallback; it does not make an incompatible runtime safe to
execute. Use a CLI, project dependency, and prebuilt artifact from the same
compatible release. Local source changes require matching local runtime source.

Application authors use the shipped assets through `gosx dev` and `gosx build`.
Production uses TinyGo or a compatible verified prebuilt runtime; no Node, npm,
or JavaScript bundler setup is required in the application. Browser cost is
still real: inspect `dist/build.json`, `gosx size`, and route measurements for
the assets actually selected rather than assuming every page loads the same
runtime.

Framework maintainers change the authored sources in `client/runtime/host`,
`client/runtime/wasm`, `client/runtime/scene3d`, and `client/js/bootstrap-src`.
`make build-bootstrap` generates the runtime ABI source and the JavaScript
bundles and compression sidecars. `make test-js` checks generated artifacts,
runtime tests, and the designated TypeScript checks; `make test-wasm-islands`
and `make wasm-size-budget` cover the WASM side. The strict TypeScript checks
cover designated modules, not the entire browser runtime.

## Run a compiled island with `go run`

From a GoSX checkout, build the runtime once and start the counter example:

```sh
go run ./cmd/gosx build-runtime build
go run ./examples/counter
```

The runtime build requires TinyGo. The example compiles its island program,
mounts the JSON at `/gosx/islands/Counter.json`, and calls
`app.SetRuntimeRoot(repositoryRoot)` so the server can find `build/` and the
bootstrap scripts in `client/js/`. It registers the program URL with
`SetProgramAsset` and the runtime URL and its content hash with `SetRuntime`.
See `examples/counter/main.go` for the complete code. For multiple programs,
`PageRuntime.SetProgramDir` can infer their URLs by name instead.

`gosx dev` stages its own runtime and island programs and supports hot reload;
it does not require the standalone setup above. `gosx build` prepares assets
for deployment.

A mount whose pattern starts with `/gosx/` wins over the built-in runtime handler for matching requests, with no fallback when the mount returns 404. A broad `/gosx/` mount also takes `/gosx/runtime.wasm` and the bootstrap scripts. Mount exact paths or use a narrower prefix such as `/gosx/islands/`. POST requests to `/gosx/action/` retain their existing routing. Public files and rewrites under `/gosx/` remain behind the runtime handler.

An application that renders an island without a configured runtime root currently receives no diagnostic (#879).
