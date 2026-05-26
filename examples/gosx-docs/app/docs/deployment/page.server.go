package docs

import (
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Deployment", "Build, export, and deploy GoSX applications as single binaries, static sites, or edge bundles.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Deployment",
				"description": "Build, export, and deploy GoSX applications as single binaries, static sites, or edge bundles.",
				"tags":        []string{"build", "deploy", "static", "ssr", "isr", "edge"},
				"toc": []map[string]string{
					{"href": "#build-modes", "label": "Build Modes"},
					{"href": "#static-export", "label": "Static Export"},
					{"href": "#github-pages", "label": "GitHub Pages"},
					{"href": "#server-deployment", "label": "Server Deployment"},
					{"href": "#isr", "label": "ISR"},
					{"href": "#edge-bundles", "label": "Edge Bundles"},
					{"href": "#docker", "label": "Docker"},
				},
				"sampleBuildModes": `# SSR binary (default)
gosx build --prod

# Static export
gosx export

# Edge bundle
gosx build --prod`,
				"sampleExport": `# Development-style static export
gosx export

# Production bundle with hashed runtime assets
gosx build --prod

# Output structure
dist/
  static/
    index.html
    docs/
      compiler/
        index.html
      deployment/
        index.html
    404.html
    gosx/
      assets/
      islands/
      css/
  export.json`,
				"sampleGitHubPages": `name: Deploy GoSX site to GitHub Pages

on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: false

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true
      - name: Install TinyGo
        run: |
          curl -fsSL -o tinygo.deb https://github.com/tinygo-org/tinygo/releases/download/v0.40.1/tinygo_0.40.1_amd64.deb
          sudo dpkg -i tinygo.deb
      - uses: actions/configure-pages@v5
      - name: Build static site
        run: go run github.com/odvcencio/gosx/cmd/gosx build --prod .
      - name: Disable Jekyll
        run: touch dist/static/.nojekyll
      - uses: actions/upload-pages-artifact@v4
        with:
          path: dist/static

  deploy:
    runs-on: ubuntu-latest
    needs: build
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - id: deployment
        uses: actions/deploy-pages@v4`,
				"sampleServerBuild": `go build -o gosx-app ./cmd/server

# Binary contains everything — templates, assets, WASM.
ls -lh gosx-app
# -rwxr-xr-x  1 user  staff  14M gosx-app

PORT=8080 ./gosx-app`,
				"sampleServerMain": `func main() {
	app := gosx.New(gosx.Config{
		Port:     os.Getenv("PORT"),
		// Optional: external ISR cache.
		ISRCache: redis.NewISRAdapter(redisClient),
	})
	app.Mount(modules.All())
	app.ListenAndServe()
}`,
				"sampleISR": `func init() {
	docs.RegisterDocsPage("Products", "Product catalogue.", route.FileModuleOptions{
		ISR: &route.ISROptions{
			// Revalidate every 60 seconds in the background.
			RevalidateSeconds: 60,
		},
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			products, err := db.ListProducts(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]any{"products": products}, nil
		},
	})
}`,
				"sampleEdge": `gosx build --prod --target edge --out ./edge-dist

# Output
edge-dist/
  handler.wasm    # 3.2 MB — all routes + templates
  manifest.json   # route table for the edge adapter`,
				"sampleDockerfile": `# Dockerfile.runtime
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN gosx build --prod && go build -o /bin/app ./cmd/server

FROM gcr.io/distroless/static
COPY --from=builder /bin/app /app
EXPOSE 8080
ENTRYPOINT ["/app"]`,
				"sampleDockerDeploy": `docker build -f Dockerfile.runtime -t harbor.example.com/myapp:v1.0.0 .
docker push harbor.example.com/myapp:v1.0.0

kubectl set image deployment/myapp app=harbor.example.com/myapp:v1.0.0`,
			}, nil
		},
	})
}
