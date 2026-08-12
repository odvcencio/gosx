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
WATER_EVIDENCE_URL ?= http://127.0.0.1:3000/demos/water
WATER_EVIDENCE_OUT ?= build/water-profile-evidence
WATER_EVIDENCE_BUDGET ?= perf/budgets/water-profile-evidence.json
WATER_EVIDENCE_FLAGS ?=
# Run each fuzz smoke target a fixed number of times. This keeps
# continuous-integration runs deterministic and avoids shutdown races.
FUZZTIME ?= 2000x
FUZZ_TIMEOUT ?= 45s
FUZZ_PARALLEL ?= 2
GOFILES := $(shell find . -name '*.go' -not -path './dist/*' -not -path './build/*')
DMJFILES := $(shell find . -name '*.dmj' -not -path './dist/*' -not -path './build/*')
DMJGOFILES := $(patsubst %.dmj,%_danmuji_test.go,$(DMJFILES))

.PHONY: fmt fmt-check verify-fmt verify-danmuji canopy-index canopy-stats canopy-clean build-bootstrap test test-unit test-cli test-ci-partitions test-race test-race-pr test-fuzz-smoke test-js test-editor test-wasm test-wasm-islands wasm-size-budget test-e2e test-perf-browser test-ouroboros-smoke test-water-prod test-water-profile-evidence water-profile-evidence test-desktop test-desktop-macos perf-budget perf-budget-ci build-cli build-desktop-windows build-desktop-macos build-runtime ci test-motion-parity test-physics-parity release-gate

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

# The production-build integration tests in cmd/gosx require TinyGo and account
# for nearly all of `go test ./...` wall time. CI runs this exhaustive
# non-CLI partition beside test-cli. internal/citest discovers the packages,
# proves the two partitions are disjoint and complete, and prints the exact
# package counts before it delegates to `go test`.
test-unit:
	GOSX_CI_GO="$(GO)" $(GO) run ./internal/citest test unit

test-cli:
	$(GO) test ./cmd/gosx

test-ci-partitions:
	$(GO) test ./internal/citest
	GOSX_CI_GO="$(GO)" $(GO) run ./internal/citest verify

test-race:
	$(GO) test -race ./...

# Pull requests exercise the reviewed shared-state surfaces without rerunning
# CPU-heavy codec and vector kernels under the race detector. Protected-branch
# pushes still run test-race across every package.
test-race-pr:
	GOSX_CI_GO="$(GO)" $(GO) run ./internal/citest test race

test-fuzz-smoke:
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./session -run '^$$' -fuzz FuzzDanmujiDecodeSessionCookieNeverPanics -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./crdt -run '^$$' -fuzz FuzzDanmujiLoadDocumentNeverPanics -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./physics -run '^$$' -fuzz FuzzDanmujiRaycastHandlesBoundedNumericInputs -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./route -run '^$$' -fuzz FuzzDanmujiRouterHandlesArbitraryEscapedPaths -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./client/vm -run '^$$' -fuzz FuzzVMEvalNeverPanics -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)
	GOMAXPROCS=$(FUZZ_PARALLEL) $(GO) test ./client/vm -run '^$$' -fuzz FuzzIslandReuseMatchesFullEval -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_PARALLEL) -timeout=$(FUZZ_TIMEOUT)

# build-bootstrap regenerates the client bootstrap bundles (pure Go — no npm, no
# node_modules; see cmd/buildbootstrap).
#
# cmd/buildbootstrap is its OWN module on purpose. It needs a JS minifier
# (esbuild's Go API) and compressors, and gosx advertises a small external
# dependency surface — five runtime deps. A build tool must not spend that
# budget: nesting it keeps those requires out of the library's go.mod and out of
# every consumer's module graph. It is invoked from its own directory for the
# same reason.
BOOTSTRAP_GRAMMAR_TAGS := grammar_subset grammar_subset_typescript grammar_subset_tsx

build-bootstrap:
	cd cmd/buildbootstrap && $(GO) run -tags '$(BOOTSTRAP_GRAMMAR_TAGS)' .

# test-js runs three independent checks:
#   1. The unit tests of the bundle builder itself. cmd/buildbootstrap
#      writes every shipped client bundle, so a defect there corrupts
#      bootstrap.js, bootstrap-lite.js, bootstrap-runtime.js, every
#      bootstrap-feature-*.js, and each .br/.gz/.map sibling. Check 2
#      below detects a STALE bundle; only these tests detect a WRONG
#      one. They also pin the chunk-to-symbol map, so a split that
#      re-duplicates a payload fails here by name.
#   2. The bundle staleness check (pure Go). cmd/buildbootstrap is
#      its own module (see build-bootstrap above). So a repo-local
#      go.work that omits it makes `go run .` fail with "main module
#      does not contain package". GOWORK=off forces module mode for
#      this one command. The target then works the same, with or
#      without a local go.work.
#   3. The JS runtime unit tests (`node --test`, stdlib-only, with no
#      npm dependencies to install), across every *.test.js /
#      *.test.mjs file. The glob picks up new test files on its own,
#      so nothing here needs an edit when a suite is added or split.
#      This includes the 562 client-runtime tests in the
#      runtime-NN-*.test.js files (split out of the former
#      runtime.test.js; their shared setup lives in
#      client/js/runtime-test-harness.js, which the glob skips
#      because it is not a *.test.js file) and the size-budget gates
#      in bootstrap-size.test.mjs.
test-js:
	cd cmd/buildbootstrap && GOWORK=off $(GO) test -tags '$(BOOTSTRAP_GRAMMAR_TAGS)' ./...
	cd cmd/buildbootstrap && GOWORK=off $(GO) run -tags '$(BOOTSTRAP_GRAMMAR_TAGS)' . --check
	$(NODE) --test ./client/js/*.test.js ./client/js/*.test.mjs

# test-editor builds, vets and tests the nested editor module.
#
# editor/ is its own Go module, so nothing at the repository root reaches it:
# `go list ./editor/...` fails, `make test` never compiles it, and the string
# "editor" appeared nowhere in this file. That hid 3404 non-test lines and 68
# tests from every gate.
#
# GOWORK=off is required, for the same reason as build-bootstrap: the repo-local
# go.work lists the root, ../prism and ../selena, so a command run inside
# editor/ fails with "directory prefix . does not contain modules listed in
# go.work". The module reaches gosx through a replace directive to the parent
# directory, so this target tests the editor against the working tree.
#
# fmt-check already covers these files: GOFILES walks the whole tree with find,
# which crosses the module boundary.
test-editor:
	cd editor && GOWORK=off $(GO) build ./...
	cd editor && GOWORK=off $(GO) vet ./...
	cd editor && GOWORK=off $(GO) test ./...

# build-wasm-all: the gate behind the zero-CGo portability claim.
#
# README says every package compiles to WASM. Nothing enforced that, so the
# claim drifted: examples/vecdb-webgpu-smoke referenced a vecdb prepared-query
# API that had been deleted, and js/wasm had not built cleanly for some time.
# The breakage was documented in prose and in an e2e test gated behind the tag
# `webgpusmoke`, which no target, workflow or script ever passed — so the guard
# could never fail and the prose rotted beside it.
#
# `make test` cannot catch this: it builds for the host, where a _js.go suffix
# excludes the offending file. test-wasm below builds only ./client/wasm.
# This target builds EVERY package for js/wasm, which is what the claim says.
build-wasm-all:
	GOOS=js GOARCH=wasm $(GO) build ./...

test-wasm:
	GOOS=js GOARCH=wasm $(GO) test -exec="$(GO_WASM_EXEC)" ./client/wasm

test-wasm-islands:
	GOOS=js GOARCH=wasm $(GO) test -tags='gosx_tiny_runtime gosx_tiny_islands_only' -exec="$(GO_WASM_EXEC)" ./client/wasm

# test-motion-parity: native↔WASM parity gate for the motion evaluator.
# Runs TestGolden (and the full motion suite) under GOOS=js GOARCH=wasm so that
# the native-generated golden corpus proves FMA/float parity across targets.
test-motion-parity:
	$(GO) test ./motion/
	GOOS=js GOARCH=wasm $(GO) test -exec="$(GO_WASM_EXEC)" ./motion/ -run TestGolden -v
	GOOS=js GOARCH=wasm $(GO) test -exec="$(GO_WASM_EXEC)" ./motion/

# test-physics-parity: native↔WASM parity gate for the rigid body engine.
# Replays the golden corpus under GOOS=js GOARCH=wasm and demands bit equality,
# so a server's authoritative step and a client's predicted step cannot drift.
# The claim needs a build that does not fuse a floating point multiply and add:
# GOAMD64 v2 or lower (the default) on amd64, and js/wasm, which has no fused
# multiply-add instruction. GOAMD64 v3 and arm64 fuse and fail with that reason.
test-physics-parity:
	$(GO) test ./physics/...
	GOOS=js GOARCH=wasm $(GO) test -exec="$(GO_WASM_EXEC)" ./physics/ -run 'TestParityCorpus|TestFusedMultiplyAddProbeIsSound' -v
	GOOS=js GOARCH=wasm $(GO) test -exec="$(GO_WASM_EXEC)" ./physics/

# wasm-size-budget builds both client/wasm flavors and asserts they stay within
# the budget. Override WASM_FULL_BUDGET_KB / WASM_TINY_BUDGET_KB to raise the
# bar for a planned-growth slice (require an ADR for any >10% bump).
wasm-size-budget:
	./scripts/check-wasm-size.sh

test-e2e:
	$(GO) test -tags e2e -timeout 30m ./e2e

# test-perf-browser runs the perf driver's own browser tests.
#
# perf/ holds six test files behind `//go:build browser`, and until this target
# existed NOTHING in the repository passed -tags browser: not this file, not any
# workflow, not any script. So `go test ./perf/` reported "ok ... [no tests to
# run]" and 11 tests over about 505 lines never compiled. The first run under
# the tag failed: TestRecordGIF proved that Page.startScreencast captures
# nothing over a page that holds still, which broke `gosx perf --record` for
# every still page. See perf/record.go.
#
# The tag is correct and stays: these tests launch Chrome. This target is the
# thing that was missing.
#
# GOSX_REQUIRE_CHROME turns "Chrome not found" from a skip into a failure. Every
# test in perf/ needs a browser, so without it this target would print "ok" over
# zero executed tests — the same invisible pass the build tag produced. A plain
# `go test -tags browser ./perf/...` still skips, which is right for a machine
# without Chrome.
test-perf-browser:
	GOSX_REQUIRE_CHROME=1 $(GO) test -tags browser -timeout 10m ./perf/...

# Requires OUROBOROS_SMOKE_BASELINE to point at a committed smoke artifact.
# CI intentionally does not invoke this until that versioned browser capture lands.
test-ouroboros-smoke:
	$(SHELL) ./scripts/ouroboros-smoke-ci.sh

# Build the deployable docs bundle and prove the production server can serve
# the water route and its content-addressed Scene3D runtime assets.
test-water-prod:
	$(SHELL) ./scripts/prod-water-smoke.sh

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

test-water-profile-evidence:
	$(NODE) --test scripts/water-profile-evidence.test.mjs
	$(NODE) scripts/water-profile-evidence.mjs --check-config --budget $(WATER_EVIDENCE_BUDGET)

water-profile-evidence:
	$(NODE) scripts/water-profile-evidence.mjs --url $(WATER_EVIDENCE_URL) --out-dir $(WATER_EVIDENCE_OUT) --budget $(WATER_EVIDENCE_BUDGET) $(WATER_EVIDENCE_FLAGS)

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

# release-gate: cheap, always-on checks that make the next bad tag impossible.
# Each gate below exists because of a specific past incident:
#   1. `go run ./cmd/gosx release check` - internal/version, README, and CHANGELOG
#      drifted out of sync and sat wrong for four releases at v0.25.3 undetected.
#   2. go.mod replace-directive scan - `go run mod@version` fails outright when
#      go.mod contains ANY replace directive; this is what made v0.27.0 a bad tag.
#   3. tracked-filename scan - a stray file with a shell-redirect-style name broke
#      Go module zip creation and forced the v0.29.0 retraction.
#   4. module-zip smoke (`git archive --format=zip`) - reproduces the exact
#      operation that broke v0.29.0 so a zip-breaking commit fails fast.
release-gate:
	@echo "release-gate (1/4): go run ./cmd/gosx release check"
	$(GO) run ./cmd/gosx release check
	@echo "release-gate (2/4): go.mod replace-directive scan"
	@if grep -E '^replace ' go.mod; then \
		echo "release-gate: go.mod has a replace directive; 'go run mod@version' fails with any replace present (this is why v0.27.0 was a bad tag). Remove it before release."; \
		exit 1; \
	fi
	@echo "release-gate (3/4): tracked-filename scan"
	@bad="$$(git ls-files | grep -E '[:"|<>?*[:cntrl:]]' || true)"; \
	if [ -n "$$bad" ]; then \
		echo "release-gate: tracked filenames contain characters Go's module zip format rejects (a file like this broke v0.29.0's module zip and forced its retraction):"; \
		echo "$$bad"; \
		exit 1; \
	fi
	@echo "release-gate (4/4): module-zip smoke (git archive --format=zip)"
	@git archive --format=zip -o /dev/null HEAD
	@echo "release-gate: all gates passed"

ci: fmt-check verify-danmuji test test-race test-fuzz-smoke test-js test-editor test-wasm test-wasm-islands test-motion-parity test-physics-parity wasm-size-budget test-e2e test-perf-browser perf-budget-ci test-desktop test-desktop-macos build-cli build-desktop-windows build-desktop-macos build-runtime
