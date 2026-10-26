# Inside the turbofan

An orbitable, procedural two-spool turbofan cutaway with a steady thermodynamic
cycle workbench, authored in Go and GSX. No external model, texture, custom
shader, or application JavaScript is required.

![The running turbofan cutaway](preview.png)

From the repository root:

```sh
GOWORK=off go run ./cmd/gosx build --prod examples/turbofan
PORT=8080 examples/turbofan/dist/run.sh
```

Open <http://localhost:8080>. A development build also works; replace `--prod`
with `--dev`. The `build.goWASM` configuration compiles the cycle engine and
stages it with content hashes, Brotli/gzip compression and the matching Go
loader alongside the framework runtime assets. After a build,
`GOWORK=off go run ./examples/turbofan` runs the source application too.

Drag to orbit and scroll or pinch to zoom. Camera and casing buttons update the existing scene locally, with no page
navigation or application data requests. Casing changes preserve the camera and
rotor clock. Pause freezes the current pose; resume continues from that pose.
Browser Back/Forward restores camera and casing choices.
The framework also honors the browser's reduced-motion preference.

The cycle workbench solves both streams, burner energy, two shaft power
balances and convergent nozzles with choking and pressure thrust. Six sliders
change flight conditions and design inputs. Results, station temperatures,
pressures and flows update locally in the dedicated Go/WASM engine without
requests or navigation. Export a point to CSV with inputs, assumptions and
unrounded results. Sea-level and cruise presets change flight conditions;
Reset all restores the design. Infeasible combinations clear the results.

This is a generic **design-point** calculator for cycle trends and energy
accounting. Constant gas properties and prescribed corrected airflow and
efficiencies are explicit assumptions. Nozzle areas resize at each point.
There is no real-engine calibration, component-map matching, surge prediction,
CFD or transient shaft dynamics. The rotor animation is illustrative.
See [equations, analytic benchmark and limits](MODEL.md).

![The calculated cycle workbench](cycle-preview.png)

The model includes a partial nacelle and intake lip, a spinner and 24 fan
blades, seven compressor rotor rows with six stationary stator rows, an
annular combustor with injectors, one high-pressure and three low-pressure
turbine rows, concentric shafts, an exhaust duct, and a tail cone. The fan
and low-pressure turbine share `low-spool`; the compressor and high-pressure
turbine share `high-spool`. Their angular velocities are 0.48 and 0.82 rad/s
for observation, not operating RPM.

Lathe profiles form the casings. Sampled, twisted airfoil surfaces are combined
into one indexed `BufferGeometry` per blade row: 498 blades in 18 row meshes,
about 48,000 triangles overall. This preserves custom airfoils without relying
on the primitive-only `InstancedMesh` path. Stable IDs and retained `Group.Spin`
boundaries keep the entire rotor assemblies on their shared shaft axis.
Both casing halves stay resident in 88 meshes: the cutaway draws 78 and
the exterior draws 76. Switching casing only changes visibility. SceneIR is
about 3.0 MB before HTTP compression. The scene caps rendering at 30 FPS,
1.5 DPR, and 921,600 backing pixels.

The geometry does not represent a particular manufacturer's engine or the
cycle solver's calculated nozzle areas. The front half of the nacelle and
core casing is omitted in cutaway mode. Stage proportions and motion are
chosen for clarity.

Validation:

```sh
GOWORK=off go test ./scene/... ./examples/turbofan/...
GOWORK=off go vet ./examples/turbofan/...
GOWORK=off go run ./cmd/gosx check examples/turbofan/app/page.gsx
GOWORK=off go run ./cmd/gosx size examples/turbofan/dist/build.json
```

With Playwright installed, run the browser checks against the running build:

```sh
SCENE3D_TOOLS_DIR=/path/containing/node_modules \
SCENE3D_BROWSER=/path/to/chromium \
node examples/turbofan/verify.mjs http://127.0.0.1:8080
```

The harness checks native/WASM numerical agreement, keyboard/slider/touch
input, CSV contents, infeasible-state recovery, a server-calculated fallback,
visible motion, pause/resume at the current pose, casing and camera controls
without navigation or data fetches, document/canvas identity, Back/Forward,
pointer orbit, wheel zoom, query fallbacks, mobile fit, reduced motion, and
WebGPU selection or an explicit WebGL fallback. It writes the documentation
previews and local `evidence/` captures with a source-hashed JSON receipt.
It is test tooling and is never served as application code.

The retained [browser receipt](verification.json) identifies the tested source
by SHA-256; [mobile capture](mobile-preview.png). On this Linux software GPU,
WebGPU lost its device and recovered visibly through WebGL2. Native WebGPU
visual parity and hardware GPU frame rates remain unverified. The TinyGo
production build succeeded without the optional `wasm-opt` optimization.

For a software-rendered browser, the existing documentation Scene3D budget
provides delivery and load checks:

```sh
GOWORK=off go run ./cmd/gosx perf --coverage --budget perf/budgets/default.json \
  --budget-profile docs-scene3d-ci http://127.0.0.1:8080
```

The lathe generator regression test also covers the surface gap found while
building this example: each sampled quad must contain two non-overlapping
triangles sharing a diagonal, with every boundary edge present once.
