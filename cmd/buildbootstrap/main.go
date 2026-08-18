// Command buildbootstrap builds the client bootstrap bundles from
// client/js/bootstrap-src. It is the pure-Go replacement for the retired
// Node build script (client/js/build-bootstrap.mjs): same source
// concatenation order, same bundle set, same .gz/.br sidecars, same --check
// mode, same output paths — with no npm, no node_modules, and no JS toolchain.
//
// Minification defaults to esbuild's native Go library (esbuild is written in
// Go; the npm package was only a wrapper around it), which keeps the minified
// bundles byte-identical to what the retired Node pipeline produced and keeps
// composed source maps. A pure tdewolff/minify backend is available via
// -minifier=tdewolff for A/B comparison; as of the migration it produces
// smaller raw/brotli output on the large bundles but breaches three committed
// size gates (see docs in the repo history), so it is not the default.
//
// Usage:
//
//	go run ./cmd/buildbootstrap            # rebuild all bundles
//	go run ./cmd/buildbootstrap --check    # exit 1 if committed bundles are stale
//
// The build also writes client/js/bootstrap-src/chunks.json, the machine-
// readable copy of the chunk manifest below. JS tests read that file instead of
// keeping a second hand-written copy of the file lists. --check treats a stale
// chunks.json the same way it treats a stale bundle.
//
// bootstrap-src/15-scene-ir-schema-strict.ts is NOT in any bundle. It is a
// development tool: it validates a SceneIR document the server already
// produced, it publishes window.__gosx_validate_scene_ir_strict, and nothing
// in the runtime, the server, the tests or the examples reads that global. It
// cost every Scene3D page 52_389 source bytes. Load the file directly when a
// tool needs it.
//
// chunks.json is the machine-readable bundle manifest this tool writes, so a
// file that joins a bundle appears there. The claim below fails the moment the
// strict validator is bundled again, and it names this paragraph as the thing
// to correct.
//
//	gosx:claim lacks client/js/bootstrap-src/chunks.json `15-scene-ir-schema-strict`
//	gosx:claim has client/js/bootstrap-src/15-scene-ir-schema-strict.ts `__gosx_validate_scene_ir_strict`
//
// Set GOSX_BUNDLE_DEBUG=1 to append sourceMappingURL trailers to the bundles.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/klauspost/compress/gzip"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/js"
)

// Shared source paths. Every chunk lists whole files. The build used to cut
// chunk boundaries out of one large file with literal comment banners and
// function-declaration strings as markers, so a rename or a re-indent changed
// what shipped. The former 30-tail.js is now the 30a..30k file set and the
// runtime-utils head of 10-runtime-scene-core.js is now
// 10-runtime-scene-utils.ts. Each seam is a file, so a chunk manifest names
// files only.
const (
	textLayoutEngineFile     = "bootstrap-src/01-textlayout-engine.ts"
	runtimeSceneUtilsFile    = "bootstrap-src/10-runtime-scene-utils.ts"
	runtimeSceneCoreFile     = "bootstrap-src/10-runtime-scene-core.js"
	runtimePrimitivesFile    = "bootstrap-src/10-runtime-primitives.ts"
	tailEventDelegationFile  = "../runtime/host/events.ts"
	tailEngineMountingFile   = "bootstrap-src/30b-tail-engine-mounting.ts"
	tailHubConnectionsFile   = "../runtime/host/hubs.ts"
	tailHubFightInputFile    = "bootstrap-src/30c1-tail-hub-fight-input.ts"
	tailArcadeAudioFile      = "bootstrap-src/30c2-tail-arcade-audio.ts"
	tailIslandDisposeFile    = "../runtime/host/disposal.ts"
	tailEngineDisposeFile    = "../runtime/host/engine-disposal.ts"
	tailHubDisconnectFile    = "../runtime/host/hub-disposal.ts"
	tailPageDisposeFile      = "../runtime/host/page-disposal.ts"
	tailCapabilityProbeFile  = "bootstrap-src/30h-tail-capability-probe.ts"
	tailHydrationFile        = "../runtime/host/hydration.ts"
	tailRuntimeReadyFile     = "bootstrap-src/30j-tail-runtime-ready.ts"
	tailInitFile             = "bootstrap-src/30k-tail-init.js"
	videoSyncFallbackFile    = "bootstrap-src/28-video-sync-fallback.ts"
	scene3DCommandBridgeFile = "../runtime/scene3d/command-bridge.ts"
	controllersFile          = "../runtime/host/controllers.ts"
	hostCompatibilityFile    = "../runtime/host/compatibility.ts"
	runtimeContractFile      = "../runtime/generated/runtime-abi.ts"
	runtimeABISupportFile    = "../runtime/wasm/abi.ts"
	runtimeMailboxFile       = "../runtime/wasm/mailbox.ts"
	runtimeLoaderFile        = "../runtime/wasm/loader.ts"
)

type source struct {
	rel   string // path relative to client/js, forward slashes
	label string // sourcemap label, always the path
}

func sourceFile(rel string) source {
	return source{rel: rel, label: rel}
}

type output struct {
	name    string // bundle file name relative to client/js
	sources []source
}

var outputs = []output{
	{
		name: "bootstrap.js",
		sources: []source{
			sourceFile("bootstrap-src/00-textlayout.js"),
			// The monolith keeps the text-layout engine inline. 01a/01b wrap it
			// in its own IIFE, exactly like the lazily fetched
			// bootstrap-feature-textlayout.js chunk, so one engine code path
			// serves both. The engine reads its shared helpers from
			// window.__gosx_runtime_api, which 00-textlayout.js assigns last.
			sourceFile("bootstrap-src/01a-textlayout-inline-prefix.js"),
			sourceFile(textLayoutEngineFile),
			sourceFile("bootstrap-src/01b-textlayout-inline-suffix.js"),
			sourceFile("bootstrap-src/04-telemetry.ts"),
			sourceFile("bootstrap-src/05-document-env.ts"),
			sourceFile(hostCompatibilityFile),
			sourceFile(runtimeContractFile),
			sourceFile(runtimeABISupportFile),
			sourceFile(runtimeMailboxFile),
			sourceFile(runtimeLoaderFile),
			sourceFile("../runtime/host/actions.ts"),
			sourceFile(scene3DCommandBridgeFile),
			sourceFile("../runtime/host/regions.ts"),
			sourceFile(controllersFile),
			sourceFile("../runtime/host/facade.ts"),
			sourceFile("../runtime/host/stream.ts"),
			sourceFile("../runtime/host/dom.ts"),
			sourceFile("bootstrap-src/26-runtime-blocks.ts"),
			sourceFile(runtimePrimitivesFile),
			sourceFile(runtimeSceneUtilsFile),
			sourceFile(runtimeSceneCoreFile),
			sourceFile("bootstrap-src/11-scene-math.ts"),
			sourceFile("bootstrap-src/11-scene-base64.ts"),
			sourceFile("bootstrap-src/11a-scene-decompress.ts"),
			sourceFile("bootstrap-src/11b-scene-points-generate.ts"),
			sourceFile("bootstrap-src/12-scene-geometry.ts"),
			sourceFile("bootstrap-src/13-scene-material.ts"),
			sourceFile("bootstrap-src/14-scene-lighting.ts"),
			sourceFile("bootstrap-src/15-scene-ir-schema.ts"),
			sourceFile("bootstrap-src/15-scene-draw-plan.ts"),
			sourceFile("bootstrap-src/15b-scene-planner.ts"),
			sourceFile("bootstrap-src/15c-scene-backend-registry.ts"),
			sourceFile("bootstrap-src/15a-scene-postfx-shared.ts"),
			sourceFile("../runtime/scene3d/dom-regions.ts"),
			sourceFile("bootstrap-src/15a1-scene-texture-budget.ts"),
			sourceFile("bootstrap-src/16b-scene-hdr.ts"),
			// 16c holds the backend-agnostic PBR helpers 16-scene-webgl.js used
			// to own. The monolith keeps 16-scene-webgl.js inline right after
			// it, so both files ship here and neither declares a name twice.
			sourceFile("bootstrap-src/16c-scene-shared-pbr.ts"),
			// 16e holds the legacy vertex-colour WebGL renderer that
			// 10-runtime-scene-core.js used to carry. Only a WebGL page runs it,
			// so it ships beside 16-scene-webgl.js in the WebGL chunk and here.
			sourceFile("bootstrap-src/16e-scene-webgl-legacy.ts"),
			sourceFile("../runtime/scene3d/webgl.ts"),
			// 16z provides _externalProbe and window.__gosx_scene3d_webgpu_probe,
			// which 16a-scene-webgpu.js references at runtime. Without it the
			// legacy monolithic bootstrap.js throws ReferenceError the first
			// time the scene3d mount path touches the webgpu probe, which in
			// turn aborts GoSXScene3D engine registration and kills 38 tests
			// in the client/js runtime suite that rely on scene3d mount.
			sourceFile("bootstrap-src/16z-scene-webgpu-probe.ts"),
			// 16a1 holds the Selena uniform packer at module scope. 16a calls it
			// from wgpuCreatePostProcessor, which sits outside the renderer
			// closure, so the packer cannot live inside that closure. It ships
			// right after 16a because that placement compresses best.
			sourceFile("../runtime/scene3d/webgpu.ts"),
			sourceFile("bootstrap-src/16a1-scene-webgpu-selena-uniforms.ts"),
			sourceFile("../runtime/scene3d/compute.ts"),
			sourceFile("bootstrap-src/17-scene-input.ts"),
			sourceFile("bootstrap-src/18-scene-canvas.ts"),
			// 19a-scene-ktx2.ts holds the browser KTX2 reader and the block
			// uploader. It must load BEFORE 19-scene-gltf.js, which reads
			// sceneKTX2UploadPathReady before it swaps an image URI for a block
			// variant.
			sourceFile("bootstrap-src/19a-scene-ktx2.ts"),
			sourceFile("../runtime/scene3d/gltf.ts"),
			sourceFile("../runtime/scene3d/animation.ts"),
			sourceFile("bootstrap-src/19b-scene-control-forms.ts"),
			// 16d publishes the base symbols the lazy WebGL chunk reads through
			// window.__gosx_scene3d_api. The monolith does not need the bridge,
			// but it costs a few hundred bytes and keeps one source order.
			sourceFile("bootstrap-src/16d-scene-webgl-bridge.ts"),
			sourceFile("../runtime/scene3d/mount-backend.ts"),
			sourceFile("../runtime/scene3d/mount-webgl.ts"),
			sourceFile("../runtime/scene3d/mount-quality.ts"),
			sourceFile("../runtime/scene3d/overlays.ts"),
			sourceFile("../runtime/scene3d/mount-viewport.ts"),
			sourceFile("../runtime/scene3d/overlay-dom.ts"),
			sourceFile("../runtime/scene3d/mount-controls.ts"),
			sourceFile("../runtime/scene3d/mount-telemetry.ts"),
			sourceFile("../runtime/scene3d/mount.ts"),
			// 28 installs window.__gosx_video_sync_js_create — the pure-JS drift
			// engine the video factory (in 30b) uses on the brain-absent path. It
			// must load before the tail.
			sourceFile(videoSyncFallbackFile),
			// The 30a..30k set replaces the former single 30-tail.js. The
			// monolith ships every part, in the original order.
			sourceFile(tailEventDelegationFile),
			sourceFile(tailEngineMountingFile),
			sourceFile(tailHubConnectionsFile),
			sourceFile(tailHubFightInputFile),
			sourceFile(tailArcadeAudioFile),
			sourceFile(tailIslandDisposeFile),
			sourceFile(tailEngineDisposeFile),
			sourceFile(tailHubDisconnectFile),
			sourceFile(tailPageDisposeFile),
			sourceFile(tailCapabilityProbeFile),
			sourceFile(tailHydrationFile),
			sourceFile(tailRuntimeReadyFile),
			sourceFile(tailInitFile),
		},
	},
	{
		name: "bootstrap-lite.js",
		sources: []source{
			sourceFile("bootstrap-src/00-textlayout.js"),
			sourceFile("bootstrap-src/04-telemetry.ts"),
			sourceFile("bootstrap-src/05-document-env.ts"),
			sourceFile(hostCompatibilityFile),
			sourceFile("../runtime/host/actions.ts"),
			sourceFile("../runtime/host/regions.ts"),
			sourceFile("../runtime/host/facade.ts"),
			sourceFile("../runtime/host/stream.ts"),
			sourceFile("../runtime/host/dom.ts"),
			sourceFile("bootstrap-src/26-runtime-blocks.ts"),
			sourceFile("bootstrap-src/25-lite-tail.js"),
		},
	},
	{
		name: "bootstrap-runtime.js",
		sources: []source{
			sourceFile("bootstrap-src/00-textlayout.js"),
			sourceFile("bootstrap-src/04-telemetry.ts"),
			sourceFile("bootstrap-src/05-document-env.ts"),
			sourceFile(hostCompatibilityFile),
			sourceFile(runtimeContractFile),
			sourceFile(runtimeABISupportFile),
			sourceFile(runtimeMailboxFile),
			sourceFile(runtimeLoaderFile),
			sourceFile("../runtime/host/actions.ts"),
			sourceFile("../runtime/host/regions.ts"),
			sourceFile("../runtime/host/facade.ts"),
			sourceFile("../runtime/host/stream.ts"),
			sourceFile("../runtime/host/dom.ts"),
			sourceFile("bootstrap-src/26-runtime-blocks.ts"),
			sourceFile(runtimeSceneUtilsFile),
			sourceFile(runtimePrimitivesFile),
			sourceFile("bootstrap-src/26-runtime-tail.js"),
		},
	},
	{
		name: "bootstrap-feature-islands.js",
		sources: []source{
			sourceFile("bootstrap-src/26a-feature-islands-prefix.js"),
			sourceFile(hostCompatibilityFile),
			sourceFile(tailEventDelegationFile),
			sourceFile(tailIslandDisposeFile),
			// 30h holds entryRequiresAsyncWebGPUProbe. The hydration path calls
			// it, so both the islands chunk and the engines chunk carry it. The
			// marker-based build duplicated it silently, because the hydration
			// extract range contained the probe extract range.
			sourceFile(tailCapabilityProbeFile),
			sourceFile(tailHydrationFile),
			sourceFile("bootstrap-src/26a-feature-islands-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-engines.js",
		sources: []source{
			sourceFile("bootstrap-src/26b-feature-engines-prefix.js"),
			sourceFile(hostCompatibilityFile),
			// 26b1 installs window.__gosx_paint_canvas_bundle — the standalone 2D
			// painter the canvas2d surface-kind render loop (in 26b-prefix's
			// _startCanvasSurfaceRAF) calls each frame. Self-contained IIFE; load
			// order is immaterial since the loop resolves the global at rAF time.
			sourceFile("bootstrap-src/26b1-canvas2d-painter.ts"),
			// 26b2 installs window.__gosx_canvas_board_labels_sync — the DOM label
			// overlay that positions real HTML <span> elements over the WebGPU/canvas
			// board so text stays in the DOM (subpixel rendering, future editability).
			// Self-contained IIFE; the slice-4 RAF loop calls sync each frame.
			sourceFile("bootstrap-src/26b2-canvas-board-labels.ts"),
			// 28 installs window.__gosx_video_sync_js_create, the pure-JS drift
			// engine the video factory uses when the WASM brain is absent. The
			// engines feature carries the video factory, so it must carry the
			// fallback engine too.
			sourceFile(videoSyncFallbackFile),
			sourceFile(tailCapabilityProbeFile),
			sourceFile(tailEngineMountingFile),
			sourceFile(tailEngineDisposeFile),
			sourceFile("bootstrap-src/26b-feature-engines-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-hubs.js",
		sources: []source{
			sourceFile("bootstrap-src/26c-feature-hubs-prefix.js"),
			sourceFile(hostCompatibilityFile),
			sourceFile(tailHubConnectionsFile),
			// 30c1 is one application's fighting-game controller set. It ships
			// only because hydrate.HubInputConfig.Mode routes to it. Drop this
			// one path when that typed Go API retires.
			sourceFile(tailHubFightInputFile),
			sourceFile(tailArcadeAudioFile),
			sourceFile(tailHubDisconnectFile),
			sourceFile("bootstrap-src/26c-feature-hubs-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-controllers.js",
		sources: []source{
			sourceFile("bootstrap-src/26h-feature-controllers-prefix.js"),
			sourceFile(hostCompatibilityFile),
			sourceFile(controllersFile),
			sourceFile("bootstrap-src/26h-feature-controllers-suffix.js"),
		},
	},
	{
		// Text-layout engine chunk. bootstrap-lite.js and bootstrap-runtime.js
		// carried this engine on every page, even a page with no text block:
		// 42_738 of 131_137 minified bytes in lite (32.6%) and 42_751 of
		// 157_086 in runtime (27.2%). The selective runtime now fetches it when
		// the document holds a data-gosx-text-layout element, or when the
		// manifest mounts a Scene3D engine, which lays out labels.
		name: "bootstrap-feature-textlayout.js",
		sources: []source{
			sourceFile("bootstrap-src/26i-feature-textlayout-prefix.js"),
			sourceFile(textLayoutEngineFile),
			sourceFile("bootstrap-src/26i-feature-textlayout-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-scene3d.js",
		sources: []source{
			sourceFile("bootstrap-src/26d-feature-scene3d-prefix.js"),
			sourceFile(scene3DCommandBridgeFile),
			sourceFile(runtimePrimitivesFile),
			// 10-runtime-scene-utils.ts is NOT here any more. This chunk
			// carried a full copy of it while bootstrap-runtime.js carried
			// another, so a Chromium Scene3D page downloaded the same helpers
			// twice. 26d-feature-scene3d-prefix.js now bridges the eight names
			// this chunk reads from window.__gosx_runtime_api.
			sourceFile(runtimeSceneCoreFile),
			sourceFile("bootstrap-src/11-scene-math.ts"),
			// 11-scene-base64.ts stays eager. 20-scene-mount.js decodes a motion
			// program with it on pages that carry no compressed array at all.
			sourceFile("bootstrap-src/11-scene-base64.ts"),
			// 11a-scene-decompress.ts and 11b-scene-points-generate.ts are NOT
			// here any more — they moved to
			// bootstrap-feature-scene3d-decompress.js. A scene with plain float
			// arrays and no generator descriptor runs neither, and used to pay
			// 8_514 raw / 3_164 gzip / 2_602 brotli minified bytes for both.
			// createSceneState awaits the chunk before it decodes.
			sourceFile("bootstrap-src/12-scene-geometry.ts"),
			sourceFile("bootstrap-src/13-scene-material.ts"),
			sourceFile("bootstrap-src/14-scene-lighting.ts"),
			sourceFile("bootstrap-src/15-scene-ir-schema.ts"),
			sourceFile("bootstrap-src/15-scene-draw-plan.ts"),
			sourceFile("bootstrap-src/15b-scene-planner.ts"),
			sourceFile("bootstrap-src/15c-scene-backend-registry.ts"),
			// 15a keeps only the scalars the scene core and both renderers read.
			// The texture-unit table and the IBL budget moved to
			// 15a1-scene-texture-budget.ts, which ships in the WebGL chunk.
			// 16b-scene-hdr.ts moved there too: sceneParseRadianceHDR has one
			// caller in the tree, and it is 16-scene-webgl.js.
			sourceFile("bootstrap-src/15a-scene-postfx-shared.ts"),
			sourceFile("../runtime/scene3d/dom-regions.ts"),
			// 16b-scene-compute.js is NOT here any more — it moved to
			// bootstrap-feature-scene3d-compute.js. A scene with one cube and one
			// directional light runs no particle simulation, no CPU particle
			// fallback and no GPU instanced cull, and it used to pay 30_189 raw /
			// 8_772 gzip / 7_409 brotli minified bytes for all three. The mount
			// fetches the chunk when the scene declares compute particles or an
			// instanced mesh, and the renderers read the symbols through
			// window.__gosx_scene3d_api at call time.
			// 16-scene-webgl.js is NOT here any more — it moved to
			// bootstrap-feature-scene3d-webgl.js. A WebGPU-capable browser
			// downloaded both GPU backends and drew with one of them, so the
			// WebGL renderer cost a Chromium Scene3D page 160_835 minified
			// bytes it never executed. 20-scene-mount.js fetches the chunk
			// when the selection order puts WebGL first, and again when a
			// WebGPU device loss walks the fallback ladder down to WebGL.
			//
			// 16c keeps the backend-agnostic part eager: the camera matrices,
			// the shadow bounds, the instanced geometry generators, the light
			// and environment hashes, and the draw-pass classifiers. 15b, the
			// scene core and the WebGPU chunk all read those.
			//
			// 16b-scene-compute.js stays in this chunk because WebGL uses its
			// CPU particle-system path. This chunk is the ONLY copy of it: the
			// webgpu chunk carried a second copy until v0.35.8, which cost a
			// Chromium Scene3D page 27_651 duplicate minified bytes. 16z holds
			// the tiny stub + adapter probe.
			sourceFile("bootstrap-src/16c-scene-shared-pbr.ts"),
			sourceFile("bootstrap-src/16z-scene-webgpu-probe.ts"),
			sourceFile("bootstrap-src/17-scene-input.ts"),
			sourceFile("bootstrap-src/18-scene-canvas.ts"),
			// 19-scene-gltf.js is NOT here — it moved to
			// bootstrap-feature-scene3d-gltf.js so pages that don't load .glb/
			// .gltf model assets (galaxies, particle systems, CSS-driven 3D
			// scenes — the majority of Scene3D consumers) don't pay the ~30KB
			// parse cost. 20-scene-mount.js lazy-fetches the chunk on first
			// model request via ensureGLTFFeatureLoaded().
			//
			// 19a-scene-animation.js is NOT here either — it moved to
			// bootstrap-feature-scene3d-animation.js. Pages that don't use
			// keyframe animations or skeletal clips skip ~16KB of bone math
			// and quaternion slerp. Consumers that DO need the mixer can
			// lazy-load it via window.__gosx_scene3d_animation_api.
			sourceFile("bootstrap-src/19b-scene-control-forms.ts"),
			// 16d must come after every module it reads, and before the mount
			// code that may fetch the WebGL chunk.
			sourceFile("bootstrap-src/16d-scene-webgl-bridge.ts"),
			sourceFile("../runtime/scene3d/mount-backend.ts"),
			sourceFile("../runtime/scene3d/mount-webgl.ts"),
			sourceFile("../runtime/scene3d/mount-quality.ts"),
			sourceFile("../runtime/scene3d/overlays.ts"),
			sourceFile("../runtime/scene3d/mount-viewport.ts"),
			sourceFile("../runtime/scene3d/overlay-dom.ts"),
			sourceFile("../runtime/scene3d/mount-controls.ts"),
			sourceFile("../runtime/scene3d/mount-telemetry.ts"),
			sourceFile("../runtime/scene3d/mount.ts"),
			sourceFile("bootstrap-src/26d-feature-scene3d-suffix.js"),
		},
	},
	{
		// WebGL2 renderer chunk. A WebGPU-capable browser never fetches it.
		// 20-scene-mount.js loads it when backendSelectionOrder puts WebGL
		// first, and when the WebGPU fallback ladder steps down to WebGL after
		// a device loss. See 26j-feature-scene3d-webgl-prefix.js for the
		// bridged symbols.
		name: "bootstrap-feature-scene3d-webgl.js",
		sources: []source{
			sourceFile("bootstrap-src/26j-feature-scene3d-webgl-prefix.js"),
			// 15a1 and 16b-scene-hdr.ts follow the renderer that reads them.
			// 16-scene-webgl.js is the only caller of sceneAllocateTextureUnits,
			// sceneResolveIBLRenderTargetMode and sceneParseRadianceHDR, so a
			// WebGPU page stops paying for a WebGL2 sampler table, an IBL budget
			// solver and a Radiance HDR decoder it can never reach.
			sourceFile("bootstrap-src/15a1-scene-texture-budget.ts"),
			sourceFile("bootstrap-src/16b-scene-hdr.ts"),
			// 16e is the legacy vertex-colour renderer. It left
			// 10-runtime-scene-core.js, where every Scene3D page paid for it,
			// and joined the renderer it backs up. createSceneWebGLResult in
			// 20b calls it only after the PBR factory declines, and that
			// factory ships in this same chunk, so a WebGPU page can never
			// reach either one.
			sourceFile("bootstrap-src/16e-scene-webgl-legacy.ts"),
			sourceFile("../runtime/scene3d/webgl.ts"),
			sourceFile("bootstrap-src/26j-feature-scene3d-webgl-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-scene3d-command.js",
		sources: []source{
			sourceFile("../runtime/scene3d/command-runtime.ts"),
		},
	},
	{
		name: "bootstrap-feature-scene3d-webgpu.js",
		sources: []source{
			sourceFile("bootstrap-src/26e-feature-scene3d-webgpu-prefix.js"),
			// 16b-scene-compute.js is NOT here any more. It used to ship in
			// this chunk AND in bootstrap-feature-scene3d.js, so a Chromium
			// Scene3D page downloaded the same 27_651 minified bytes twice.
			// It now ships once, in the base scene3d chunk, and this bridge
			// hands 16a the two symbols it reads lexically.
			sourceFile("bootstrap-src/26e1-feature-scene3d-webgpu-compute-bridge.ts"),
			sourceFile("../runtime/scene3d/webgpu.ts"),
			sourceFile("bootstrap-src/16a1-scene-webgpu-selena-uniforms.ts"),
			sourceFile("bootstrap-src/26e-feature-scene3d-webgpu-suffix.js"),
		},
	},
	{
		// Compute chunk: the GPU particle simulation, the CPU particle
		// fallback, the particle force registry and the GPU instanced-cull
		// system. Both renderers read it, and neither can carry it, so it is
		// its own chunk rather than a passenger on one backend.
		name: "bootstrap-feature-scene3d-compute.js",
		sources: []source{
			sourceFile("bootstrap-src/26k-feature-scene3d-compute-prefix.js"),
			sourceFile("../runtime/scene3d/compute.ts"),
			sourceFile("bootstrap-src/26k-feature-scene3d-compute-suffix.js"),
		},
	},
	{
		// Decompress chunk: the quantized-array decoder, the progressive and
		// level-of-detail ladders, and the procedural point generators. The two
		// files call each other, so they share one chunk.
		name: "bootstrap-feature-scene3d-decompress.js",
		sources: []source{
			sourceFile("bootstrap-src/26l-feature-scene3d-decompress-prefix.js"),
			sourceFile("bootstrap-src/11a-scene-decompress.ts"),
			sourceFile("bootstrap-src/11b-scene-points-generate.ts"),
			sourceFile("bootstrap-src/26l-feature-scene3d-decompress-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-scene3d-gltf.js",
		sources: []source{
			sourceFile("bootstrap-src/26f-feature-scene3d-gltf-prefix.js"),
			// 19a holds the KTX2 reader and the block uploader. It must load
			// before 19-scene-gltf.js, which reads sceneKTX2UploadPathReady
			// before it swaps an image URI for a block variant. It ships in
			// this chunk only: the base scene3d chunk has no lexical reader of
			// it, and a second copy would be a second download.
			sourceFile("bootstrap-src/19a-scene-ktx2.ts"),
			sourceFile("../runtime/scene3d/gltf.ts"),
			sourceFile("bootstrap-src/26f-feature-scene3d-gltf-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-scene3d-animation.js",
		sources: []source{
			sourceFile("bootstrap-src/26g-feature-scene3d-animation-prefix.js"),
			sourceFile("../runtime/scene3d/animation.ts"),
			sourceFile("bootstrap-src/26g-feature-scene3d-animation-suffix.js"),
		},
	},
	{
		// Standalone host authorities are part of the build graph now, but their
		// committed JS/map/compressed artifacts remain a deliberate Stack 07
		// cutover. Until then --check reports them stale.
		name: "patch.js",
		sources: []source{
			sourceFile(hostCompatibilityFile),
			sourceFile("../runtime/host/patch.ts"),
		},
	},
	{
		name: "relay.js",
		sources: []source{
			sourceFile(hostCompatibilityFile),
			sourceFile("../runtime/host/relay.ts"),
		},
	},
	{
		name: "stripe-bridge.js",
		sources: []source{
			sourceFile(hostCompatibilityFile),
			sourceFile("../runtime/host/stripe-bridge.ts"),
		},
	},
}

// inlineAssets lists artifacts this tool prepares for direct Go embedding
// rather than for a fetched <script src> bundle. server.NavigationScript
// (server/navigation.go) inlines the navigation runtime straight into every
// page's <head> so it runs before any other script fetches — it must stay
// out of the outputs bundle graph above, which is why navigation.ts is a
// named exemption in TestEveryRuntimeTypeScriptAuthorityIsInTheBuildGraph.
//
// An inline asset gets the same concatenate/erase-types/minify treatment as
// a bundle (buildBundle), but the build writes only the minified .min.js
// file next to its .ts sources: no .map, no .gz/.br sidecars, and it never
// joins chunks.json, because nothing ever fetches it over the network —
// client/runtime/host/navigation_asset.go go:embeds it straight into the Go
// binary that server.NavigationScript then writes inline into the page.
var inlineAssets = []output{
	{
		name: "../runtime/host/navigation-runtime.min.js",
		sources: []source{
			sourceFile(hostCompatibilityFile),
			sourceFile("../runtime/host/navigation.ts"),
		},
	},
}

const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// normalizeNewlines converts \r\n and bare \r to \n, matching the mjs
// implementation's `String(source).replace(/\r\n?/g, "\n")`.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

type compacted struct {
	code    string
	lineMap []int
}

func compactSource(body string) compacted {
	lines := strings.Split(normalizeNewlines(body), "\n")
	var out []string
	var lineMap []int
	lastBlank := false

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		normalized := strings.TrimRight(line, " \t")
		if trimmed == "" {
			if lastBlank {
				continue
			}
			lastBlank = true
			out = append(out, "")
			lineMap = append(lineMap, index)
			continue
		}

		lastBlank = false
		out = append(out, normalized)
		lineMap = append(lineMap, index)
	}

	for len(out) > 0 && out[0] == "" {
		out = out[1:]
		lineMap = lineMap[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
		lineMap = lineMap[:len(lineMap)-1]
	}
	code := ""
	if len(out) > 0 {
		code = strings.Join(out, "\n") + "\n"
	}
	return compacted{code: code, lineMap: lineMap}
}

func base64VLQEncode(value int) string {
	current := value << 1
	if value < 0 {
		current = ((-value) << 1) | 1
	}
	var encoded strings.Builder
	for {
		digit := current & 31
		current >>= 5
		if current > 0 {
			digit |= 32
		}
		encoded.WriteByte(base64Chars[digit])
		if current <= 0 {
			break
		}
	}
	return encoded.String()
}

type mappingLine struct {
	source       int
	originalLine int
	column       int
}

func encodeMappings(lines []*mappingLine) string {
	segments := make([]string, 0, len(lines))
	previousSource := 0
	previousOriginalLine := 0
	previousOriginalColumn := 0

	for _, line := range lines {
		if line == nil {
			segments = append(segments, "")
			continue
		}
		segments = append(segments,
			base64VLQEncode(0)+
				base64VLQEncode(line.source-previousSource)+
				base64VLQEncode(line.originalLine-previousOriginalLine)+
				base64VLQEncode(line.column-previousOriginalColumn))
		previousSource = line.source
		previousOriginalLine = line.originalLine
		previousOriginalColumn = line.column
	}

	return strings.Join(segments, ";")
}

type sidecar struct {
	path  string
	bytes []byte // nil when compression does not beat the raw payload
}

func gzipCompress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func brotliCompress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := brotli.NewWriterOptions(&buf, brotli.WriterOptions{Quality: brotli.BestCompression})
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressedSidecars(filePath, code string) ([]sidecar, error) {
	raw := []byte(code)
	if len(raw) == 0 {
		return nil, nil
	}
	gz, err := gzipCompress(raw)
	if err != nil {
		return nil, fmt.Errorf("gzip %s: %w", filePath, err)
	}
	br, err := brotliCompress(raw)
	if err != nil {
		return nil, fmt.Errorf("brotli %s: %w", filePath, err)
	}
	sidecars := []sidecar{
		{path: filePath + ".gz", bytes: gz},
		{path: filePath + ".br", bytes: br},
	}
	for i := range sidecars {
		if len(sidecars[i].bytes) >= len(raw) {
			sidecars[i].bytes = nil
		}
	}
	return sidecars, nil
}

func writeCompressedSidecars(filePath, code string) error {
	sidecars, err := compressedSidecars(filePath, code)
	if err != nil {
		return err
	}
	for _, sc := range sidecars {
		if sc.bytes != nil {
			if err := os.WriteFile(sc.path, sc.bytes, 0o644); err != nil {
				return err
			}
			continue
		}
		if _, statErr := os.Stat(sc.path); statErr == nil {
			if err := os.Remove(sc.path); err != nil {
				return err
			}
		}
	}
	return nil
}

func sidecarsMatch(filePath, code string) (bool, error) {
	sidecars, err := compressedSidecars(filePath, code)
	if err != nil {
		return false, err
	}
	for _, sc := range sidecars {
		current, readErr := os.ReadFile(sc.path)
		exists := readErr == nil
		if sc.bytes == nil {
			if exists {
				return false, nil
			}
			continue
		}
		if !exists || !bytes.Equal(current, sc.bytes) {
			return false, nil
		}
	}
	return true, nil
}

type builtBundle struct {
	code string
	m    string
}

// jsQuote encodes a string exactly like JavaScript's JSON.stringify: it
// escapes `"`, `\`, and control characters (with the \b \t \n \f \r
// shorthands), and leaves everything else — including U+2028/U+2029 and
// non-ASCII — as raw UTF-8. encoding/json cannot be used here because it
// unconditionally escapes U+2028/U+2029 and encodes \b and \f as \u escapes,
// which would break byte-for-byte parity with the retired Node pipeline.
func jsQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func jsQuoteArray(values []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsQuote(v))
	}
	b.WriteByte(']')
	return b.String()
}

// compactedMapJSON serializes the pre-minification source map with the same
// key order and formatting the mjs pipeline used (JSON.stringify of
// {version, file, sources, sourcesContent, names, mappings}).
func compactedMapJSON(file string, sources, sourcesContent []string, mappings string) string {
	var b strings.Builder
	b.WriteString(`{"version":3,"file":`)
	b.WriteString(jsQuote(file))
	b.WriteString(`,"sources":`)
	b.WriteString(jsQuoteArray(sources))
	b.WriteString(`,"sourcesContent":`)
	b.WriteString(jsQuoteArray(sourcesContent))
	b.WriteString(`,"names":[],"mappings":`)
	b.WriteString(jsQuote(mappings))
	b.WriteByte('}')
	return b.String()
}

func buildCompactedBundle(dir string, entry output) (builtBundle, error) {
	type section struct {
		label     string
		raw       string
		compacted compacted
	}
	sections := make([]section, 0, len(entry.sources))
	for _, src := range entry.sources {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(src.rel)))
		if err != nil {
			return builtBundle{}, err
		}

		language, err := languageForSource(src)
		if err != nil {
			return builtBundle{}, err
		}

		raw := normalizeNewlines(string(data))
		bodyForCompaction := raw
		var lineOrigins []int
		if language == sourceTypeScript {
			// Validate against the original file before the chunk swallows a
			// syntax error into one offset in the concatenated bundle: a
			// tree-sitter diagnostic here still names src.rel and the exact
			// line and column the author sees in the editor.
			if err := validateTypedSource(src, data); err != nil {
				return builtBundle{}, err
			}

			// Erase this source's own types with its own esbuild loader, never
			// the whole chunk's. A .js source beside a .ts source must never
			// reach the TypeScript parser: reparsing `a < b > (c)` as a
			// generic-argument call silently drops the comparison against b.
			erased, mappings, err := transpileSource(src, raw)
			if err != nil {
				return builtBundle{}, err
			}
			origins, err := firstOriginalLinePerGeneratedLine(mappings)
			if err != nil {
				return builtBundle{}, fmt.Errorf("decode transpile map for %s: %w", src.rel, err)
			}
			bodyForCompaction = erased
			lineOrigins = origins
		}

		compactedSection := compactSource(bodyForCompaction)
		if lineOrigins != nil {
			// compactedSection.lineMap holds rows in the erased text. Type
			// erasure can remove whole lines (an erased interface, for
			// example), so translate each row through the real esbuild map
			// back to the original .ts row before the section's line map
			// joins the hand-rolled, line-only compacted map below. A row
			// esbuild names no source position for (an inserted blank line)
			// keeps the closest earlier mapped row.
			lastKnown := 0
			for i, erasedLine := range compactedSection.lineMap {
				if erasedLine >= 0 && erasedLine < len(lineOrigins) && lineOrigins[erasedLine] >= 0 {
					lastKnown = lineOrigins[erasedLine]
				}
				compactedSection.lineMap[i] = lastKnown
			}
		}

		sections = append(sections, section{
			label:     src.label,
			raw:       raw,
			compacted: compactedSection,
		})
	}

	var code strings.Builder
	var lines []*mappingLine
	for index, sec := range sections {
		if index > 0 && code.Len() != 0 {
			code.WriteByte('\n')
			lines = append(lines, nil)
		}
		code.WriteString(sec.compacted.code)
		for _, originalLine := range sec.compacted.lineMap {
			lines = append(lines, &mappingLine{source: index, originalLine: originalLine})
		}
	}

	joined := code.String()
	if !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}

	sources := make([]string, 0, len(sections))
	sourcesContent := make([]string, 0, len(sections))
	for _, sec := range sections {
		sources = append(sources, sec.label)
		sourcesContent = append(sourcesContent, sec.raw)
	}
	m := compactedMapJSON(entry.name, sources, sourcesContent, encodeMappings(lines))
	return builtBundle{code: joined, m: m}, nil
}

// minifyESBuild minifies with esbuild's Go library, composing the compacted
// source map into the final map exactly like the retired mjs pipeline: the
// compacted map rides in as an inline sourceMappingURL data URL, and esbuild
// emits the composed external map.
//
// built.code is already plain JavaScript: buildCompactedBundle erases every
// typed source's types on its own, before the chunk is assembled, so this
// step always reads with the JavaScript loader. It never re-promotes the
// whole chunk to the TypeScript parser, which used to reparse a neighboring
// .js source's `a < b > (c)` as a generic-argument call and silently drop
// the comparison against b.
func minifyESBuild(entry output, built builtBundle) (builtBundle, error) {
	dataURL := "data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(built.m))
	input := built.code + "\n//# sourceMappingURL=" + dataURL
	result := esbuild.Transform(input, esbuild.TransformOptions{
		Charset:           esbuild.CharsetUTF8,
		LegalComments:     esbuild.LegalCommentsNone,
		Loader:            esbuild.LoaderJS,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Sourcefile:        entry.name,
		Sourcemap:         esbuild.SourceMapExternal,
		Target:            esbuild.ES2020,
	})
	if len(result.Errors) > 0 {
		return builtBundle{}, fmt.Errorf("esbuild %s: %s", entry.name, result.Errors[0].Text)
	}
	m, err := normalizeESBuildMap(result.Map, entry.name)
	if err != nil {
		return builtBundle{}, fmt.Errorf("normalize map for %s: %w", entry.name, err)
	}
	return builtBundle{code: string(result.Code), m: m}, nil
}

// esbuildMap mirrors the JSON shape of esbuild's external source map output.
type esbuildMap struct {
	Version        int      `json:"version"`
	Sources        []string `json:"sources"`
	SourcesContent []string `json:"sourcesContent"`
	Mappings       string   `json:"mappings"`
	Names          []string `json:"names"`
}

// normalizeESBuildMap reproduces the mjs normalizeGeneratedMap: parse
// esbuild's (pretty-printed) map, set .file, and re-serialize compactly with
// JSON.stringify semantics and esbuild's key order (version, sources,
// sourcesContent, mappings, names) plus file appended last.
func normalizeESBuildMap(raw []byte, fileName string) (string, error) {
	var parsed esbuildMap
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`{"version":`)
	b.WriteString(strconv.Itoa(parsed.Version))
	b.WriteString(`,"sources":`)
	b.WriteString(jsQuoteArray(parsed.Sources))
	b.WriteString(`,"sourcesContent":`)
	b.WriteString(jsQuoteArray(parsed.SourcesContent))
	b.WriteString(`,"mappings":`)
	b.WriteString(jsQuote(parsed.Mappings))
	b.WriteString(`,"names":`)
	b.WriteString(jsQuoteArray(parsed.Names))
	b.WriteString(`,"file":`)
	b.WriteString(jsQuote(fileName))
	b.WriteByte('}')
	return b.String(), nil
}

// minifyTdewolff minifies with the pure-Go tdewolff/minify JS minifier. It
// cannot compose source maps, so callers keep the compacted (pre-minify) map.
func minifyTdewolff(code string) (string, error) {
	minifier := &js.Minifier{Version: 2020}
	var out bytes.Buffer
	if err := minifier.Minify(minify.New(), &out, strings.NewReader(code), nil); err != nil {
		return "", err
	}
	return out.String(), nil
}

func normalizeGeneratedCode(code, mapName string, debugSourcemaps bool) string {
	next := normalizeNewlines(code)
	if !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	if debugSourcemaps {
		next += "//# sourceMappingURL=" + mapName + "\n"
	}
	return next
}

func buildBundle(dir string, entry output, minifier string, debugSourcemaps bool) (builtBundle, error) {
	built, err := buildCompactedBundle(dir, entry)
	if err != nil {
		return builtBundle{}, err
	}
	switch minifier {
	case "esbuild":
		minified, err := minifyESBuild(entry, built)
		if err != nil {
			return builtBundle{}, err
		}
		return builtBundle{
			code: normalizeGeneratedCode(minified.code, entry.name+".map", debugSourcemaps),
			m:    minified.m,
		}, nil
	case "tdewolff":
		// built.code is already plain JavaScript: buildCompactedBundle erases
		// every typed source's types on its own before it assembles the
		// chunk, so tdewolff never needs a second, whole-chunk erasure pass.
		minified, err := minifyTdewolff(built.code)
		if err != nil {
			return builtBundle{}, fmt.Errorf("minify %s: %w", entry.name, err)
		}
		return builtBundle{
			code: normalizeGeneratedCode(minified, entry.name+".map", debugSourcemaps),
			// tdewolff cannot compose source maps, so the shipped map still
			// stops before minification: it is the pre-minify compacted map,
			// which now accounts for type erasure (buildCompactedBundle
			// erases each typed source before it builds this map), but not
			// for tdewolff's own line and column shifts. The esbuild path
			// remains the release default for that reason.
			m: built.m,
		}, nil
	default:
		return builtBundle{}, fmt.Errorf("unknown minifier %q (want esbuild or tdewolff)", minifier)
	}
}

// buildInlineAsset builds one inlineAssets entry down to its minified code
// only. Unlike buildBundle for a client/js/ bundle, an inline asset never
// carries a sourceMappingURL trailer (debugSourcemaps is always false):
// nothing ever fetches the artifact at a URL, so a dangling comment
// referencing a .map file this build never writes would only confuse a
// reader working from the served page's inline <script> tag.
func buildInlineAsset(dir string, entry output, minifier string) (string, error) {
	built, err := buildBundle(dir, entry, minifier, false)
	if err != nil {
		return "", err
	}
	return built.code, nil
}

// findClientJS resolves the client/js directory: an explicit -dir wins,
// otherwise walk up from the working directory looking for
// client/js/bootstrap-src (so the tool works from the repo root or any
// subdirectory).
func findClientJS(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(filepath.Join(explicit, "bootstrap-src")); err != nil {
			return "", fmt.Errorf("-dir %q does not contain bootstrap-src: %w", explicit, err)
		}
		return explicit, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for probe := cwd; ; probe = filepath.Dir(probe) {
		candidate := filepath.Join(probe, "client", "js")
		if _, err := os.Stat(filepath.Join(candidate, "bootstrap-src")); err == nil {
			return candidate, nil
		}
		if filepath.Dir(probe) == probe {
			return "", errors.New("could not locate client/js/bootstrap-src; run from the gosx repo or pass -dir")
		}
	}
}

// chunksManifestRel is the machine-readable copy of the outputs table. JS
// tests and the free-variable closure check read it, so nothing keeps a second
// hand-written copy of a chunk file list.
const chunksManifestRel = "bootstrap-src/chunks.json"

// chunksManifestJSON renders the outputs table as stable, indented JSON. The
// key order follows the outputs table, so a manifest edit shows a small diff.
func chunksManifestJSON() string {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("  \"comment\": \"Generated by cmd/buildbootstrap. Do not edit by hand.\",\n")
	b.WriteString("  \"chunks\": [\n")
	for i, entry := range outputs {
		b.WriteString("    {\n      \"name\": ")
		b.WriteString(jsQuote(entry.name))
		b.WriteString(",\n      \"sources\": [\n")
		for j, src := range entry.sources {
			b.WriteString("        ")
			b.WriteString(jsQuote(src.rel))
			if j < len(entry.sources)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString("      ]\n    }")
		if i < len(outputs)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ]\n}\n")
	return b.String()
}

func run() error {
	dirFlag := flag.String("dir", "", "path to client/js (default: auto-detect from working directory)")
	check := flag.Bool("check", false, "verify committed bundles are up to date; exit 1 when stale")
	closureOnly := flag.Bool("closure", false, "run only the chunk closure check and exit")
	minifier := flag.String("minifier", "esbuild", "JS minifier backend: esbuild (default, byte-stable) or tdewolff (A/B comparison)")
	flag.Parse()

	dir, err := findClientJS(*dirFlag)
	if err != nil {
		return err
	}

	// Prove every chunk is closed before doing anything else. A chunk that
	// reads an identifier nothing declares throws ReferenceError in the
	// browser, and no size gate can see that.
	if err := verifyChunkClosure(dir); err != nil {
		return err
	}
	if *closureOnly {
		return nil
	}
	debugSourcemaps := os.Getenv("GOSX_BUNDLE_DEBUG") == "1"

	manifestPath := filepath.Join(dir, filepath.FromSlash(chunksManifestRel))
	manifest := chunksManifestJSON()

	if *check {
		var stale []string
		recordStale := func(name string) {
			stale = append(stale, name)
		}
		if current, _ := os.ReadFile(manifestPath); string(current) != manifest {
			recordStale(chunksManifestRel)
		}
		for _, entry := range outputs {
			next, err := buildBundle(dir, entry, *minifier, debugSourcemaps)
			if err != nil {
				return err
			}
			bundlePath := filepath.Join(dir, entry.name)
			currentCode, _ := os.ReadFile(bundlePath)
			currentMap, _ := os.ReadFile(bundlePath + ".map")
			match, err := sidecarsMatch(bundlePath, next.code)
			if err != nil {
				return err
			}
			if string(currentCode) != next.code || string(currentMap) != next.m || !match {
				recordStale(entry.name)
			}
		}
		for _, entry := range inlineAssets {
			next, err := buildInlineAsset(dir, entry, *minifier)
			if err != nil {
				return err
			}
			assetPath := filepath.Join(dir, entry.name)
			current, _ := os.ReadFile(assetPath)
			if string(current) != next {
				recordStale(entry.name)
			}
		}
		if len(stale) > 0 {
			return fmt.Errorf("bootstrap runtime assets are out of date (%s). Run `go run ./cmd/buildbootstrap`", strings.Join(stale, ", "))
		}
		return nil
	}

	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		return err
	}
	for _, entry := range outputs {
		built, err := buildBundle(dir, entry, *minifier, debugSourcemaps)
		if err != nil {
			return err
		}
		bundlePath := filepath.Join(dir, entry.name)
		if err := os.WriteFile(bundlePath, []byte(built.code), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(bundlePath+".map", []byte(built.m), 0o644); err != nil {
			return err
		}
		if err := writeCompressedSidecars(bundlePath, built.code); err != nil {
			return err
		}
	}
	for _, entry := range inlineAssets {
		code, err := buildInlineAsset(dir, entry, *minifier)
		if err != nil {
			return err
		}
		assetPath := filepath.Join(dir, entry.name)
		if err := os.WriteFile(assetPath, []byte(code), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
