GO ?= go
GOFMT ?= gofmt
GO_WASM_EXEC ?= $(shell $(GO) env GOROOT)/lib/wasm/go_js_wasm_exec
GOFILES := $(shell find . -name '*.go' -not -path './dist/*' -not -path './build/*')

.PHONY: fmt fmt-check test test-race test-wasm build-cli build-runtime ci

fmt:
	$(GOFMT) -w $(GOFILES)

fmt-check:
	@unformatted="$$( $(GOFMT) -l $(GOFILES) )"; \
	if [ -n "$$unformatted" ]; then \
		echo "unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-wasm:
	GOOS=js GOARCH=wasm $(GO) test -exec="$(GO_WASM_EXEC)" ./client/wasm

build-cli:
	$(GO) build ./cmd/gosx

build-runtime:
	mkdir -p build
	GOOS=js GOARCH=wasm $(GO) build -o build/gosx-runtime.wasm ./client/wasm

ci: fmt-check test test-race test-wasm build-cli build-runtime
