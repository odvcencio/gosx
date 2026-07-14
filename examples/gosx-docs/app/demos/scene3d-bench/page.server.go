package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Scene3D Bench",
		"Live frame-time instrumentation for the Scene3D renderer — histogram, p50/p95/max, GPU info, and seven stress workloads.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				workload := ctx.Query("workload")
				return map[string]any{
					"scene":    BenchScene(workload),
					"workload": workloadLabel(workload),
				}, nil
			},
		},
	)
}

// workloadLabel returns a human-readable name for the selected workload,
// defaulting to "mixed" when the query param is empty or unknown. Kept in
// sync with the BenchScene dispatch table in program.go.
func workloadLabel(workload string) string {
	switch workload {
	case "static":
		return "static"
	case "pbr-heavy":
		return "pbr-heavy"
	case "thick-lines":
		return "thick-lines"
	case "particles":
		return "particles"
	case "mesh-swarm":
		return "mesh-swarm"
	case "particles-storm":
		return "particles-storm"
	case "mixed", "":
		return "mixed"
	default:
		return "mixed"
	}
}
