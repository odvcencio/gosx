GO ?= go
GOFMT ?= gofmt
GO_WASM_EXEC ?= $(shell $(GO) env GOROOT)/lib/wasm/go_js_wasm_exec
NODE ?= node
GZIP ?= gzip
PERL ?= perl
DANMUJI ?= danmuji
CANOPY ?= canopy
CANOPY_CACHE ?= .canopy/index.json
CANOPY_TIMEOUT ?= 120s
CANOPY_GOMAXPROCS ?= 2
CANOPY_GOMEMLIMIT ?= 1536MiB
CANOPY_MAX_VMEM_KB ?= 4194304
TMPDIR ?= /tmp
PERF_URLS ?= http://localhost:8080/
PERF_BUDGET ?= perf/budgets/default.json
PERF_OUT ?= build/perf-report.json
PERF_FLAGS ?= --mobile pixel7 --throttle 4 --coverage
FUZZTIME ?= 5s
FUZZ_TIMEOUT ?= 45s
FUZZ_PARALLEL ?= 2
GOFILES := $(shell find . -name '*.go' -not -path './dist/*' -not -path './build/*')
DMJFILES := $(shell find . -name '*.dmj' -not -path './dist/*' -not -path './build/*')
DMJGOFILES := $(patsubst %.dmj,%_danmuji_test.go,$(DMJFILES))

.PHONY: fmt fmt-check verify-fmt verify-danmuji canopy-index canopy-stats canopy-clean test test-race test-fuzz-smoke test-js test-wasm test-wasm-islands wasm-size-budget test-e2e test-desktop test-desktop-macos perf-budget perf-budget-ci build-cli build-desktop-windows build-desktop-macos build-runtime ci

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

verify-danmuji:
	@command -v $(DANMUJI) >/dev/null 2>&1 || { echo "danmuji not found; install with: go install github.com/odvcencio/danmuji/cmd/danmuji@v0.3.2"; exit 1; }
	@before="$$(mktemp)"; after="$$(mktemp)"; \
	trap 'rm -f "$$before" "$$after"' EXIT; \
	for f in $(DMJGOFILES); do \
		if [ -f "$$f" ]; then sha256sum "$$f"; else echo "MISSING  $$f"; fi; \
	done | sort > "$$before"; \
	echo "$(DANMUJI) build ."; \
	$(DANMUJI) build .; \
	echo "$(PERL) -0pi -e 's{//line \\Q$(CURDIR)/\\E}{//line }g' $(DMJGOFILES)"; \
	$(PERL) -0pi -e 's{//line \Q$(CURDIR)/\E}{//line }g' $(DMJGOFILES); \
	echo "$(GOFMT) -w $(DMJGOFILES)"; \
	$(GOFMT) -w $(DMJGOFILES); \
	for f in $(DMJGOFILES); do \
		if [ -f "$$f" ]; then sha256sum "$$f"; else echo "MISSING  $$f"; fi; \
	done | sort > "$$after"; \
	if ! diff -u "$$before" "$$after"; then \
		echo "danmuji generated files are stale; rebuild with: make verify-danmuji"; \
		exit 1; \
	fi; \
	if [ "$$CI" = "true" ]; then \
		untracked="$$(git status --porcelain -- $(DMJGOFILES) | awk '/^\?\?/ {print}')"; \
		if [ -n "$$untracked" ]; then \
			echo "danmuji generated files are missing from git:"; \
			echo "$$untracked"; \
			exit 1; \
		fi; \
	fi

canopy-index:
	mkdir -p $(dir $(CANOPY_CACHE))
	CANOPY=$(CANOPY) CANOPY_TIMEOUT=$(CANOPY_TIMEOUT) CANOPY_MAX_VMEM_KB=$(CANOPY_MAX_VMEM_KB) \
		CANOPY_GOMAXPROCS=$(CANOPY_GOMAXPROCS) CANOPY_GOMEMLIMIT=$(CANOPY_GOMEMLIMIT) \
		./scripts/canopy-safe.sh index build . --out $(CANOPY_CACHE)

canopy-stats:
	@if [ ! -f "$(CANOPY_CACHE)" ]; then \
		echo "$(CANOPY_CACHE) is missing; run: make canopy-index"; \
		exit 1; \
	fi
	CANOPY=$(CANOPY) CANOPY_TIMEOUT=$(CANOPY_TIMEOUT) CANOPY_MAX_VMEM_KB=$(CANOPY_MAX_VMEM_KB) \
		CANOPY_GOMAXPROCS=$(CANOPY_GOMAXPROCS) CANOPY_GOMEMLIMIT=$(CANOPY_GOMEMLIMIT) \
		./scripts/canopy-safe.sh index stats --cache $(CANOPY_CACHE)

canopy-clean:
	rm -rf .canopy

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-fuzz-smoke:
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./session -run '^$$' -fuzz FuzzDanmujiDecodeSessionCookieNeverPanics -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./crdt -run '^$$' -fuzz FuzzDanmujiLoadDocumentNeverPanics -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./physics -run '^$$' -fuzz FuzzDanmujiRaycastHandlesBoundedNumericInputs -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./route -run '^$$' -fuzz FuzzDanmujiRouterHandlesArbitraryEscapedPaths -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)

test-js:
	$(NODE) ./client/js/build-bootstrap.mjs --check
	$(NODE) --test ./client/js/*.test.js

test-wasm:
	GOOS=js GOARCH=wasm $(GO) test -exec="$(GO_WASM_EXEC)" ./client/wasm

test-wasm-islands:
	GOOS=js GOARCH=wasm $(GO) test -tags='gosx_tiny_runtime gosx_tiny_islands_only' -exec="$(GO_WASM_EXEC)" ./client/wasm

# wasm-size-budget builds both client/wasm flavors and asserts they stay within
# the budget. Override WASM_FULL_BUDGET_KB / WASM_TINY_BUDGET_KB to raise the
# bar for a planned-growth slice (require an ADR for any >10% bump).
wasm-size-budget:
	$(SHELL) ./scripts/check-wasm-size.sh

test-e2e:
	$(NODE) --test e2e/gosx_docs_e2e.test.mjs
	$(GO) test ./e2e

test-desktop:
	$(GO) test ./desktop ./cmd/gosx -run 'Desktop|RunDesktop|NormalizeOptions|NewUnsupportedPlatform'
	GOOS=windows GOARCH=amd64 $(GO) test -c -o $(TMPDIR)/gosx-desktop-windows-amd64.test.exe ./desktop
	GOOS=windows GOARCH=arm64 $(GO) test -c -o $(TMPDIR)/gosx-desktop-windows-arm64.test.exe ./desktop
	GOOS=windows GOARCH=amd64 $(GO) test -c -o $(TMPDIR)/gosx-cmd-windows-amd64.test.exe ./cmd/gosx
	GOOS=windows GOARCH=arm64 $(GO) test -c -o $(TMPDIR)/gosx-cmd-windows-arm64.test.exe ./cmd/gosx

test-desktop-macos:
	mkdir -p build/desktop-test
	GOOS=darwin GOARCH=amd64 $(GO) test -c -o build/desktop-test/desktop-darwin-amd64.test ./desktop
	GOOS=darwin GOARCH=arm64 $(GO) test -c -o build/desktop-test/desktop-darwin-arm64.test ./desktop
	GOOS=darwin GOARCH=amd64 $(GO) test -c -o build/desktop-test/gosx-darwin-amd64.test ./cmd/gosx
	GOOS=darwin GOARCH=arm64 $(GO) test -c -o build/desktop-test/gosx-darwin-arm64.test ./cmd/gosx

perf-budget:
	mkdir -p $(dir $(PERF_OUT))
	$(GO) run ./cmd/gosx perf $(PERF_FLAGS) --budget $(PERF_BUDGET) --json $(PERF_URLS) > $(PERF_OUT)

perf-budget-ci:
	$(SHELL) ./scripts/perf-budget-ci.sh

build-cli:
	$(GO) build ./cmd/gosx

build-desktop-windows:
	mkdir -p build
	GOOS=windows GOARCH=amd64 $(GO) build -o build/gosx-windows-amd64.exe ./cmd/gosx
	GOOS=windows GOARCH=arm64 $(GO) build -o build/gosx-windows-arm64.exe ./cmd/gosx

build-desktop-macos:
	mkdir -p build
	GOOS=darwin GOARCH=amd64 $(GO) build -o build/gosx-darwin-amd64 ./cmd/gosx
	GOOS=darwin GOARCH=arm64 $(GO) build -o build/gosx-darwin-arm64 ./cmd/gosx

build-runtime:
	$(GO) run ./cmd/gosx build-runtime build

ci: fmt-check verify-danmuji test test-race test-fuzz-smoke test-js test-wasm test-wasm-islands wasm-size-budget test-e2e perf-budget-ci test-desktop test-desktop-macos build-cli build-desktop-windows build-desktop-macos build-runtime
