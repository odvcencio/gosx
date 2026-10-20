package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Deployment", "Build, export, and operate the staged GoSX deployment bundle.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Deployment",
				"description": "Build, export, and operate the staged GoSX deployment bundle.",
				"tags":        []string{"build", "deploy", "static", "ssr", "isr", "edge", "offline"},
				"buildInfo":   docsapp.SiteBuildInfo(),
				"toc": []map[string]string{
					{"href": "#build-output", "label": "Build output"},
					{"href": "#static-export", "label": "Static export"},
					{"href": "#edge-output", "label": "Edge output"},
					{"href": "#server-deployment", "label": "Server deployment"},
					{"href": "#compression", "label": "Response compression"},
					{"href": "#isr", "label": "ISR"},
					{"href": "#offline-windows", "label": "Offline & Windows"},
					{"href": "#docker", "label": "Containers"},
				},
				"sampleBuildModes":  docsapp.DocSample("deployment/sampleBuildModes.bash.sample"),
				"sampleOutput":      docsapp.DocSample("deployment/sampleOutput.text.sample"),
				"sampleExport":      docsapp.DocSample("deployment/sampleExport.bash.sample"),
				"sampleEdge":        docsapp.DocSample("deployment/sampleEdge.bash.sample"),
				"sampleServerRun":   docsapp.DocSample("deployment/sampleServerRun.bash.sample"),
				"sampleCompression": docsapp.DocSample("deployment/sampleCompression.go.sample"),
				"sampleISRConfig":   docsapp.DocSample("deployment/sampleISRConfig.json.sample"),
				"sampleISRApp":      docsapp.DocSample("deployment/sampleISRApp.go.sample"),
				"sampleOffline":     docsapp.DocSample("deployment/sampleOffline.bash.sample"),
				"sampleDockerfile":  docsapp.DocSample("deployment/sampleDockerfile.dockerfile.sample"),
			}, nil
		},
	})
}
