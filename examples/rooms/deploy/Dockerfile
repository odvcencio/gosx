# syntax=docker/dockerfile:1
# Build from the GoSX repository root:
#   docker buildx build --load -f examples/rooms/deploy/Dockerfile -t IMAGE .
# Go 1.26.6+ fixes interface method pointers entering the js/wasm GC barrier
# (golang/go#80499); the image ships a js/wasm client.
FROM golang:1.26.9-bookworm AS build
WORKDIR /src
ENV GOWORK=off CGO_ENABLED=0 GOMAXPROCS=2 GOFLAGS=-p=2
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags='-s -w' -o /out/server ./examples/rooms/server \
    && mkdir -p /out/static \
    && GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o /out/static/main.wasm ./examples/rooms/client \
    && cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" examples/rooms/static/index.html /out/static/ \
    && test -x /out/server && test -s /out/static/main.wasm

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/ ./
ENV ROOMS_ADDR=:8090 ROOMS_STATIC=/app/static
USER 65532:65532
EXPOSE 8090
ENTRYPOINT ["/app/server"]
