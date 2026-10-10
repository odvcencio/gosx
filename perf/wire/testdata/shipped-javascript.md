The shipped JavaScript corpus scans the actual committed browser outputs and Go-embedded browser sources through `ScanReferences(body, KindScript)`. Inline WebAuthn and emoji embeds also use `ScanReferences(body, KindDocument)`. The generated bundle list comes from `cmd/buildbootstrap/main.go`; embedded sources come from Go embed declarations. Navigation is checked against its serving embed and decoded gzip/Brotli sidecars. Go and TinyGo runtime shims use the installed SDK files.

Result at the #539 production-guard follow-up (2026-10-10): **0/44 complete, 44 incomplete**. The reference allowlist contains exactly one literal fetch: `/_gosx/emoji-codes.json`, at `server/emoji_complete.ts:17`. Every other file reports zero references. No event name, CSS class, attribute name or other non-URL string is reported.

Each row gives one concrete witness that requires incomplete coverage under the retained policy. Computed access, global objects used as values, enumeration/reflection, opaque timer callbacks and denied capability tokens are mandatory failures even in framework code. Denied tokens are receiver-independent, so ordinary string methods such as `replace` and metadata such as `code` can also trigger the conservative policy. Reducing the incomplete-file count would require changing those rules; this fix does not exempt framework code. Enhanced routes reaching these files therefore remain unknown.

The monolithic bootstrap also exceeds the existing 250,000-node scan limit and returns `invalid-input`. The test verifies that specific limit; it rejects scan errors in other files. Known references before a limit remain partial, with `Complete=false`. A missing TinyGo installation skips only the external TinyGo shim; this recorded run included both SDK shims.

The witness is from the scanner’s AST policy. If its pinned esbuild fallback formatted the input, the expression is shown in that formatted form. Minified token names are kept so the witness can be located in the shipped file. It is a sufficient reason for incompleteness, not an exhaustive list of every reason in the file.

| Shipped file | Complete | Non-potential references | Reason / witness |
| --- | --- | --- | --- |
| `auth/webauthn_runtime.ts` | false | None | denied capability token: replace |
| `client/js/bootstrap-controller-input.js` | false | None | denied capability token: host |
| `client/js/bootstrap-feature-controllers.js` | false | None | computed access: m[_] |
| `client/js/bootstrap-feature-engines.js` | false | None | computed access: le[it] |
| `client/js/bootstrap-feature-hubs.js` | false | None | computed access: ue[ge] |
| `client/js/bootstrap-feature-islands.js` | false | None | computed access: u[p] |
| `client/js/bootstrap-feature-scene3d-animation.js` | false | None | computed access: n[u] |
| `client/js/bootstrap-feature-scene3d-command.js` | false | None | global object used as a value: window |
| `client/js/bootstrap-feature-scene3d-compute.js` | false | None | computed access: Se[r] |
| `client/js/bootstrap-feature-scene3d-decompress.js` | false | None | computed access: e[a] |
| `client/js/bootstrap-feature-scene3d-gltf.js` | false | None | denied capability token: code |
| `client/js/bootstrap-feature-scene3d-hydrate.js` | false | None | enumeration/reflection: getPrototypeOf |
| `client/js/bootstrap-feature-scene3d-instance-stream.js` | false | None | global object used as a value: window |
| `client/js/bootstrap-feature-scene3d-ocean-query.js` | false | None | enumeration/reflection: assign |
| `client/js/bootstrap-feature-scene3d-particle-burst.js` | false | None | computed access: i[e] |
| `client/js/bootstrap-feature-scene3d-pipeline-recovery.js` | false | None | computed access: n[1] |
| `client/js/bootstrap-feature-scene3d-timeline.js` | false | None | computed access: t[0] |
| `client/js/bootstrap-feature-scene3d-vessel.js` | false | None | computed access: e[i] |
| `client/js/bootstrap-feature-scene3d-walk.js` | false | None | computed access: a[c] |
| `client/js/bootstrap-feature-scene3d-webgl.js` | false | None | denied capability token: profile |
| `client/js/bootstrap-feature-scene3d-webgpu.js` | false | None | computed access: n[h * 4 + x] |
| `client/js/bootstrap-feature-scene3d-zoom.js` | false | None | denied capability token: style |
| `client/js/bootstrap-feature-scene3d.js` | false | None | computed access: n.dataset[e] |
| `client/js/bootstrap-feature-textlayout.js` | false | None | enumeration/reflection: values |
| `client/js/bootstrap-lite.js` | false | None | computed access: Ot[e] |
| `client/js/bootstrap-runtime.js` | false | None | computed access: Ot[e] |
| `client/js/bootstrap.js` | false | None | AST node limit (250000); computed access: Sp[e] |
| `client/js/patch.js` | false | None | denied capability token: host |
| `client/js/relay.js` | false | None | denied capability token: host |
| `client/js/stripe-bridge.js` | false | None | denied capability token: host |
| `client/js/vendor/hls.min.js` | false | None | computed access: t2[r3] |
| `client/runtime/host/navigation-runtime.min.js` | false | None | denied capability token: host |
| `editor/assets/code-intelligence.ts` | false | None | opaque timer callback: setTimeout |
| `editor/assets/collaborative-editor.ts` | false | None | denied capability token: createElement |
| `editor/assets/mdpp-diagrams.ts` | false | None | denied capability token: replace |
| `editor/assets/native-editor.ts` | false | None | denied capability token: replace |
| `editor/intelligenceassets/assets/wasm_exec.js` | false | None | denied capability token: code |
| `engine/surface/runtime/bootstrap.ts` | false | None | denied capability token: click |
| `perf/instrument.js` | false | None | computed access: entries[i] |
| `server/devtools_lantern.js` | false | None | denied capability token: open |
| `server/emoji_complete.ts` | false | `/_gosx/emoji-codes.json` (`server/emoji_complete.ts:17`) | denied capability token: textContent |
| `server/youtube_audio.js` | false | None | computed access: m[1] |
| `toolchain/go/wasm_exec.js` | false | None | denied capability token: code |
| `toolchain/tinygo/wasm_exec.js` | false | None | global object used as a value: window |
