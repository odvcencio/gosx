package docs

import (
	"github.com/odvcencio/gosx/route"
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
							"headline": "One language. Full stack.",
							"body":     "Server rendering, reactive islands, 3D scenes, real-time collaboration \u2014 all in Go. No second language required.",
						},
						{
							"headline": "Zero JavaScript toolchain.",
							"body":     "No webpack. No npm. No bundler. One go build produces a deployable binary with everything included.",
						},
						{
							"headline": "Accessible by design.",
							"body":     "WCAG 2.2 AA from the ground up. Server-measured text, reduced-motion support, semantic HTML, keyboard navigation.",
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
							"title": "File Routes & SSR",
							"body":  "Pages are files. Layouts nest. Everything renders on the server first.",
							"mode":  "light",
							"href":  "/docs/routing",
						},
						{
							"title": "Islands & Signals",
							"body":  "Reactive DOM regions powered by a Go expression VM. Shared signals sync across islands.",
							"mode":  "dark",
							"href":  "/docs/islands",
						},
						{
							"title": "3D Engine",
							"body":  "PBR renderer with WebGPU and WebGL2. Declare scenes in Go structs, not JavaScript.",
							"mode":  "dark",
							"href":  "/docs/scene3d",
						},
						{
							"title": "Forms & Actions",
							"body":  "HTML form posts with server validation, CSRF protection, and flash messages.",
							"mode":  "light",
							"href":  "/docs/forms",
						},
						{
							"title": "Auth",
							"body":  "Magic links, passkeys, and OAuth. Sessions and CSRF built in.",
							"mode":  "light",
							"href":  "/docs/auth",
						},
						{
							"title": "Real-time & CRDT",
							"body":  "WebSocket hubs with conflict-free replicated data types. Sync state across clients.",
							"mode":  "dark",
							"href":  "/docs/hubs",
						},
						{
							"title": "GPU Compute",
							"body":  "100K particles at 60fps via WGSL compute shaders. CPU fallback for WebGL2.",
							"mode":  "dark",
							"href":  "/docs/engines",
						},
						{
							"title": "Build & Deploy",
							"body":  "One binary. Static export, SSR, or edge. ISR with background revalidation.",
							"mode":  "light",
							"href":  "/docs/deployment",
						},
					},
				}, nil
			},
		},
	)
}
