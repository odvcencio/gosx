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
