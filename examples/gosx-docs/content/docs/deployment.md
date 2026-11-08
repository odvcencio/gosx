---
title: "Deployment"
description: "Build, export, and operate the staged GoSX deployment bundle."
mode: "light"
tags: ["build", "deploy", "static", "ssr", "isr", "edge", "offline"]
anchors: {"Build the deployable bundle": "build-output", "Static export": "static-export", "Prerender edge worker": "edge-output", "Server deployment": "server-deployment", "Incremental static regeneration": "isr", "Offline and Windows bundles": "offline-windows", "Container example": "docker"}
toc_labels: {"build-output": "Build output", "static-export": "Static export", "edge-output": "Edge output", "server-deployment": "Server deployment", "isr": "ISR", "offline-windows": "Offline & Windows", "docker": "Containers"}
---
## Build the deployable bundle

`gosx build` accepts an application directory. Development is the default; select production explicitly for hashed assets, a production server build, static prerendering, and edge/platform output.

```bash
# Fast development assets (the default)
gosx build --dev .

# Production assets, server, static export, and edge/platform metadata
gosx build --prod .

# Add an offline bundle
gosx build --prod --offline .

# Package a Windows release on a Windows target/host
gosx build --prod --msix .
```

Outputs are additive. The offline and Windows packaging flags extend the same production bundle; edge and platform files are part of the normal runnable production output rather than a separate target mode.

```text
dist/
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
  platform/            # deployment descriptors
```

## Static export

`gosx export .` builds the runnable application, requests each eligible non-parameterized file route, and writes HTML below `dist/static` plus route metadata in `dist/export.json`. Dynamic parameter routes are not invented during export, and a route scope can opt out with `{ "prerender": false }` in `route.config.json`.

```bash
gosx export .

# Publish this directory as the static site root:
dist/static/
```

The exporter rewrites page and asset references for nested static paths and copies only runtime assets referenced by exported documents. Treat `dist/static` as the static-host root and retain `dist/export.json` when another GoSX deployment layer needs route metadata.

## Prerender edge worker

A production build of a runnable app writes `dist/edge/worker.js` and `dist/platform/deployment.json`. The worker serves exported routes and static assets through an `ASSETS` binding, then proxies misses, dynamic routes, and mutations to `GOSX_ORIGIN` (or `ORIGIN`).

```bash
gosx build --prod .

# Deploy dist/static through the worker's ASSETS binding.
# Configure the dynamic fallback in the worker environment:
GOSX_ORIGIN=https://app.example.com
```

> [!NOTE]
> Not an edge WASM server
>
> The generated worker does not compile Go route handlers into a portable server-side WASM module. Keep an origin for anything absent from the prerendered route table.

## Server deployment

For a runnable `package main`, the build places the executable at `dist/server/app` and writes `dist/run.sh`. Deploy the whole bundle: file-routed apps can read staged `app/`, `content/`, and `public/` at runtime, while `build.json` maps hashed browser assets.

```bash
gosx build --prod .

cd dist
PORT=8080 ./run.sh

# Equivalent direct launch from the bundle root:
GOSX_APP_ROOT="$PWD" PORT=8080 ./server/app
```

## Incremental static regeneration

ISR serves pages represented in the production export manifest and refreshes stale entries in the background. Enable it on the server and give an exported route a public cache lifetime. Cache tags travel into the export metadata for explicit invalidation.

```json
{
  "cache": {
    "public": true,
    "maxAge": "60s",
    "staleWhileRevalidate": "5m"
  },
  "cacheTags": ["products"]
}
```

```go
app := server.New()
app.EnableISR()

// Optional shared store for multiple server instances:
app.SetISRStore(redis.NewISRStore(redisClient, redis.Options{
    Prefix: "shop:gosx",
}))
```

The default ISR store is process-local. A multi-instance deployment can install a shared `server.ISRStore` with `app.SetISRStore`; the Redis package provides `redis.NewISRStore`. This artifact store is distinct from the revalidation-version store.

## Offline and Windows bundles

`--offline` stages `dist/offline` with its own versioned manifest and the available static, runtime, app, and public inputs. The desktop host can open that directory through its `app://gosx` bundle transport.

```bash
gosx build --prod --offline .
gosx desktop --bundle dist/offline
```

`--msix` packages a runnable Windows build. It requires a Windows target or host and the Windows packaging tools; signing and AppInstaller generation are additional explicit options.

## Container example

Build in a toolchain image, then copy `dist/` as a unit into an image with a shell for `run.sh`. Choose a smaller base only after proving your own binary and system-library requirements.

```dockerfile
FROM golang:1.26-bookworm AS build
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
ENTRYPOINT ["/app/run.sh"]
```
