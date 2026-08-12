package docs

import (
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Deployment", "Build, export, and operate the staged GoSX deployment bundle.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Deployment",
				"description": "Build, export, and operate the staged GoSX deployment bundle.",
				"tags":        []string{"build", "deploy", "static", "ssr", "isr", "edge", "offline"},
				"toc": []map[string]string{
					{"href": "#build-output", "label": "Build output"},
					{"href": "#static-export", "label": "Static export"},
					{"href": "#edge-output", "label": "Edge output"},
					{"href": "#server-deployment", "label": "Server deployment"},
					{"href": "#isr", "label": "ISR"},
					{"href": "#offline-windows", "label": "Offline & Windows"},
					{"href": "#docker", "label": "Containers"},
				},
				"sampleBuildModes": `# Fast development assets (the default)
gosx build --dev .

# Production assets, server, static export, and edge/platform metadata
gosx build --prod .

# Add an offline bundle
gosx build --prod --offline .

# Package a Windows release on a Windows target/host
gosx build --prod --msix .`,
				"sampleOutput": `dist/
  assets/              # content-hashed browser assets
  app/                 # staged file routes
  content/             # staged content collections, when present
  public/              # staged public files, when present
  server/app           # runnable Go server, when package main
  run.sh               # sets GOSX_APP_ROOT and starts server/app
  build.json           # asset manifest
  static/              # production prerender output
  export.json          # exported route metadata
  edge/worker.js       # static-first worker with origin fallback
  platform/            # deployment descriptors`,
				"sampleExport": `gosx export .

# Publish this directory as the static site root:
dist/static/`,
				"sampleEdge": `gosx build --prod .

# Deploy dist/static through the worker's ASSETS binding.
# Configure the dynamic fallback in the worker environment:
GOSX_ORIGIN=https://app.example.com`,
				"sampleServerRun": `gosx build --prod .

cd dist
PORT=8080 ./run.sh

# Equivalent direct launch from the bundle root:
GOSX_APP_ROOT="$PWD" PORT=8080 ./server/app`,
				"sampleISRConfig": `{
  "cache": {
    "public": true,
    "maxAge": "60s",
    "staleWhileRevalidate": "5m"
  },
  "cacheTags": ["products"]
}`,
				"sampleISRApp": `app := server.New()
app.EnableISR()

// Optional shared store for multiple server instances:
app.SetISRStore(redis.NewISRStore(redisClient, redis.Options{
    Prefix: "shop:gosx",
}))`,
				"sampleOffline": `gosx build --prod --offline .
gosx desktop --bundle dist/offline`,
				"sampleDockerfile": `FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go run m31labs.dev/gosx/cmd/gosx build --prod .

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /src/dist/ ./
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/app/run.sh"]`,
			}, nil
		},
	})
}
