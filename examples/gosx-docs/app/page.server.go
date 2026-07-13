package docs

import (
	"m31labs.dev/gosx/route"
)

func init() {
	RegisterStaticDocsPage(
		"GoSX",
		"Go-native web platform. One language, full stack, zero JavaScript toolchain.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"heroScene": HeroScene(),
					"pitchStatements": []map[string]string{
						{
							"num":      "01",
							"headline": "One language. Full stack.",
							"body":     "Server rendering, reactive islands, 3D scenes, real-time collaboration \u2014 all in Go. No second language required.",
						},
						{
							"num":      "02",
							"headline": "Zero JavaScript toolchain.",
							"body":     "No webpack. No npm. No bundler. One go build produces a deployable binary with everything included.",
						},
						{
							"num":      "03",
							"headline": "Accessible by design.",
							"body":     "Semantic landmarks, named controls, reduced-motion support, server-measured text, and CI checks for duplicate IDs and ARIA references.",
						},
					},
					"proofPoints": []map[string]string{
						{"value": "5", "label": "External dependencies"},
						{"value": "79K", "label": "Lines of Go"},
						{"value": "0", "label": "CGo required"},
						{"value": "2 weeks", "label": "From zero to here"},
					},
					"showcases": []map[string]any{
						{
							"num":   "01",
							"title": "File Routes & SSR",
							"body":  "Pages are files. Layouts nest. Everything renders on the server first.",
							"href":  "/docs/routing",
						},
						{
							"num":   "02",
							"title": "Islands & Signals",
							"body":  "Reactive DOM regions powered by a Go expression VM. Shared signals sync across islands.",
							"href":  "/docs/islands",
						},
						{
							"num":   "03",
							"title": "3D Engine",
							"body":  "PBR renderer with WebGPU and WebGL2. Declare scenes in Go structs, not JavaScript.",
							"href":  "/docs/scene3d",
						},
						{
							"num":   "04",
							"title": "Forms & Actions",
							"body":  "HTML form posts with server validation, CSRF protection, and flash messages.",
							"href":  "/docs/forms",
						},
						{
							"num":   "05",
							"title": "Auth",
							"body":  "Magic links, passkeys, and OAuth. Sessions and CSRF built in.",
							"href":  "/docs/auth",
						},
						{
							"num":   "06",
							"title": "Real-time & CRDT",
							"body":  "WebSocket hubs with conflict-free replicated data types. Sync state across clients.",
							"href":  "/docs/hubs",
						},
						{
							"num":   "07",
							"title": "GPU Compute",
							"body":  "100K particles at 60fps via WGSL compute shaders. CPU fallback for WebGL2.",
							"href":  "/docs/engines",
						},
						{
							"num":   "08",
							"title": "Build & Deploy",
							"body":  "One binary. Static export, SSR, or edge. ISR with background revalidation.",
							"href":  "/docs/deployment",
						},
					},
				}, nil
			},
		},
	)
}
