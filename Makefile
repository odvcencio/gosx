GO ?= go
GOFMT ?= gofmt
GO_WASM_EXEC ?= $(shell $(GO) env GOROOT)/lib/wasm/go_js_wasm_exec
NODE ?= node
GOFILES := $(shell find . -name '*.go' -not -path './dist/*' -not -path './build/*')

.PHONY: fmt fmt-check verify-fmt test test-race test-js test-wasm test-e2e build-cli build-runtime ci

fmt:
	$(GOFMT) -w $(GOFILES)
	$(GO) run ./cmd/gosx fmt .

fmt-check:
	@unformatted="$$( $(GOFMT) -l $(GOFILES) )"; \
	if [ -n "$$unformatted" ]; then \
		echo "unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@$(GO) run ./cmd/gosx fmt --check .

verify-fmt: fmt-check

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-js:
	$(NODE) ./client/js/build-bootstrap.mjs --check
	$(NODE) --test --test-force-exit ./client/js/*.test.js

test-wasm:
	GOOS=js GOARCH=wasm $(GO) test -exec="$(GO_WASM_EXEC)" ./client/wasm

test-e2e:
	$(NODE) --test e2e/gosx_docs_e2e.test.mjs

build-cli:
	$(GO) build ./cmd/gosx

build-runtime:
	mkdir -p build
	GOOS=js GOARCH=wasm $(GO) build -o build/gosx-runtime.wasm ./client/wasm

ci: fmt-check test test-race test-js test-wasm test-e2e build-cli build-runtime
