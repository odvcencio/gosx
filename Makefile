GO ?= go
GOFMT ?= gofmt
GO_WASM_EXEC ?= $(shell $(GO) env GOROOT)/lib/wasm/go_js_wasm_exec
NODE ?= node
TMPDIR ?= /tmp
GOFILES := $(shell find . -name '*.go' -not -path './dist/*' -not -path './build/*')

.PHONY: fmt fmt-check verify-fmt test test-race test-js test-wasm test-e2e test-desktop build-cli build-desktop-windows build-runtime ci

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
	$(GO) test ./e2e

test-desktop:
	$(GO) test ./desktop ./cmd/gosx -run 'Desktop|RunDesktop|NormalizeOptions|NewUnsupportedPlatform'
	GOOS=windows GOARCH=amd64 $(GO) test -c -o $(TMPDIR)/gosx-desktop-windows-amd64.test.exe ./desktop
	GOOS=windows GOARCH=arm64 $(GO) test -c -o $(TMPDIR)/gosx-desktop-windows-arm64.test.exe ./desktop
	GOOS=windows GOARCH=amd64 $(GO) test -c -o $(TMPDIR)/gosx-cmd-windows-amd64.test.exe ./cmd/gosx
	GOOS=windows GOARCH=arm64 $(GO) test -c -o $(TMPDIR)/gosx-cmd-windows-arm64.test.exe ./cmd/gosx

build-cli:
	$(GO) build ./cmd/gosx

build-desktop-windows:
	mkdir -p build
	GOOS=windows GOARCH=amd64 $(GO) build -o build/gosx-windows-amd64.exe ./cmd/gosx
	GOOS=windows GOARCH=arm64 $(GO) build -o build/gosx-windows-arm64.exe ./cmd/gosx

build-runtime:
	mkdir -p build
	GOOS=js GOARCH=wasm $(GO) build -o build/gosx-runtime.wasm ./client/wasm

ci: fmt-check test test-race test-js test-wasm test-e2e test-desktop build-cli build-desktop-windows build-runtime
