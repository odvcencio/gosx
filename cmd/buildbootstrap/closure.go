package main

// closure.go proves that each chunk is closed: every identifier a chunk reads
// is either declared inside that chunk or is a real browser global.
//
// A chunk is one script. The browser evaluates it on its own, so an identifier
// that no source file in the chunk declares throws ReferenceError the first
// time the chunk reaches it. A split that leaves a symbol behind therefore
// ships a latent crash, and the size gates cannot see it.
//
// The parser is github.com/tdewolff/parse/v2/js, which the minifier already
// pulls in. It builds real scopes, so it reports free identifiers only, and it
// never confuses a comment or a string for code. The repo keeps a second
// implementation on acorn at cmd/buildbootstrap/freevars.mjs for cross-checks.
//
// Two classes of report are noise, not bugs:
//
//  1. A name the code tests with `typeof X === "function"` before it reads it.
//     That is the deliberate optional-symbol pattern. The check treats a name
//     as optional when the chunk applies `typeof` to it at least once.
//  2. A real browser global. The allowlist below carries those.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
)

// browserGlobals lists the identifiers a browser supplies. The bundles target
// ES2020 in a DOM window with WebGL2, WebGPU and Web Audio.
var browserGlobals = map[string]bool{}

func init() {
	names := strings.Fields(`
	globalThis window self document navigator console location history screen
	Math JSON Object Array String Number Boolean Symbol BigInt Function RegExp
	Date Error EvalError RangeError ReferenceError SyntaxError TypeError
	URIError AggregateError Promise Proxy Reflect Map Set WeakMap WeakSet
	WeakRef FinalizationRegistry ArrayBuffer SharedArrayBuffer DataView Atomics
	Int8Array Uint8Array Uint8ClampedArray Int16Array Uint16Array Int32Array
	Uint32Array Float16Array Float32Array Float64Array BigInt64Array
	BigUint64Array Infinity NaN undefined isNaN isFinite parseInt parseFloat
	decodeURI decodeURIComponent encodeURI encodeURIComponent escape unescape
	eval Intl setTimeout clearTimeout setInterval clearInterval setImmediate
	queueMicrotask structuredClone requestAnimationFrame cancelAnimationFrame
	requestIdleCallback cancelIdleCallback performance fetch Headers Request
	Response AbortController AbortSignal Blob File FileReader FormData URL
	URLSearchParams TextEncoder TextDecoder atob btoa crypto localStorage
	sessionStorage indexedDB caches matchMedia getComputedStyle alert confirm
	prompt open close scrollTo scrollBy getSelection Image Audio Option Event
	CustomEvent ErrorEvent MessageEvent CloseEvent PointerEvent KeyboardEvent
	MouseEvent WheelEvent TouchEvent InputEvent FocusEvent DragEvent
	MessageChannel MessagePort BroadcastChannel EventTarget Node Element
	HTMLElement HTMLCanvasElement HTMLImageElement HTMLVideoElement
	HTMLMediaElement HTMLScriptElement HTMLInputElement HTMLTemplateElement
	CanvasRenderingContext2D OffscreenCanvas OffscreenCanvasRenderingContext2D
	ImageBitmap createImageBitmap ImageData Path2D DOMMatrix DOMPoint
	DOMPointReadOnly DOMRect DOMRectReadOnly DOMException MutationObserver
	ResizeObserver IntersectionObserver PerformanceObserver ReportingObserver
	WebSocket EventSource Worker SharedWorker ServiceWorker XMLHttpRequest
	DOMParser XMLSerializer NodeFilter NodeIterator TreeWalker Range Selection
	CSS CSSStyleSheet FontFace ResizeObserverEntry ResizeObserverSize
	ReadableStream WritableStream TransformStream CompressionStream
	DecompressionStream WebGLRenderingContext WebGL2RenderingContext WebGLBuffer
	WebGLProgram WebGLShader WebGLTexture WebGLFramebuffer WebGLRenderbuffer
	WebGLUniformLocation WebGLVertexArrayObject WebGLQuery WebGLSync
	WebGLSampler WebGLTransformFeedback GPU GPUAdapter GPUDevice GPUBuffer
	GPUTexture GPUSampler GPUShaderModule GPUBufferUsage GPUTextureUsage
	GPUShaderStage GPUMapMode GPUColorWrite GPUOutOfMemoryError
	GPUValidationError GPUInternalError GPUCanvasContext GPUQueue
	GPUCommandEncoder GPUPipelineError AudioContext webkitAudioContext
	OfflineAudioContext AudioBuffer GainNode OscillatorNode BiquadFilterNode
	DynamicsCompressorNode StereoPannerNode AudioBufferSourceNode PannerNode
	ConstantSourceNode AnalyserNode MediaSource SourceBuffer ManagedMediaSource
	VTTCue TextTrack TextTrackCue Gamepad GamepadButton WakeLock
	ResizeObserverBoxOptions IntersectionObserverEntry WebAssembly
	arguments this
	`)
	for _, n := range names {
		browserGlobals[n] = true
	}
}

// typeofGuarded walks a chunk and returns every name the chunk feeds to the
// `typeof` operator. `typeof X` never throws for an undeclared X, so those
// names are optional by construction.
type typeofGuarded struct {
	names map[string]bool
}

func (t *typeofGuarded) Enter(n js.INode) js.IVisitor {
	if unary, ok := n.(*js.UnaryExpr); ok && unary.Op == js.TypeofToken {
		if v, ok := unary.X.(*js.Var); ok {
			t.names[string(v.Name())] = true
		}
	}
	return t
}

func (t *typeofGuarded) Exit(js.INode) {}

// chunkFreeIdentifiers parses one chunk and returns the names it reads but
// never declares, minus the browser globals and the typeof-guarded names.
func chunkFreeIdentifiers(dir string, entry output) ([]string, error) {
	bodies := make([]string, 0, len(entry.sources))
	for _, src := range entry.sources {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(src.rel)))
		if err != nil {
			return nil, err
		}
		bodies = append(bodies, normalizeNewlines(string(data)))
	}
	chunkSource, err := transpileTypedChunk(entry, bodies)
	if err != nil {
		return nil, err
	}
	ast, err := js.Parse(parse.NewInputString(chunkSource), js.Options{})
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", entry.name, err)
	}

	guarded := &typeofGuarded{names: map[string]bool{}}
	js.Walk(guarded, ast)

	var free []string
	for _, v := range ast.BlockStmt.Scope.Undeclared {
		name := string(v.Name())
		if browserGlobals[name] || guarded.names[name] {
			continue
		}
		free = append(free, name)
	}
	sort.Strings(free)
	return free, nil
}

// verifyChunkClosure fails when any chunk reads an identifier that neither the
// chunk nor the browser supplies.
func verifyChunkClosure(dir string) error {
	var report []string
	for _, entry := range outputs {
		free, err := chunkFreeIdentifiers(dir, entry)
		if err != nil {
			return err
		}
		if len(free) > 0 {
			report = append(report, fmt.Sprintf("%s: %s", entry.name, strings.Join(free, ", ")))
		}
	}
	if len(report) > 0 {
		return fmt.Errorf("chunk closure check failed; these chunks read identifiers nothing declares:\n  %s\n"+
			"Move the declaration into the chunk, bridge it through a window.__gosx_* handoff, "+
			"or guard the read with `typeof X === \"function\"`",
			strings.Join(report, "\n  "))
	}
	return nil
}
