#!/bin/bash
# check-wasm-size.sh — WASM size-budget CI gate.
#
# Measures the PRODUCTION runtime artifacts — the TinyGo builds that actually
# ship (core, full, and compatibility-islands) — and asserts they stay
# within budget.
#
# Earlier revisions measured a standard-go `go build` of client/wasm, but that
# dev artifact is ~3x larger than what ships: standard go wasm can't drop the
# host-only .gsx compiler and its gotreesitter/grammargen + go/parser
# dependencies from the closure, whereas the TinyGo production build excludes
# them (the compiler files are //go:build !tinygo). After the engine-surface
# feature pulled the compiler into the closure, the std-go number ballooned to
# ~24 MB while the shipped TinyGo runtime stayed under 1 MB — so the gate was
# tracking host-only code that never ships. We now measure the real artifact.
#
# Override budgets with WASM_FULL_BUDGET_KB, WASM_TINY_BUDGET_KB,
# WASM_CORE_BUDGET_KB, or WASM_CORE_BROTLI_BUDGET_KB.
set -euo pipefail

# TinyGo 0.41.1 without wasm-opt, measured on 2026-09-29: core 654 KiB
# raw and 215 KiB brotli; full 2199 KiB raw. Keep about 7% headroom for build variance.
FULL_BUDGET_KB="${WASM_FULL_BUDGET_KB:-2350}"
TINY_BUDGET_KB="${WASM_TINY_BUDGET_KB:-700}"
CORE_BUDGET_KB="${WASM_CORE_BUDGET_KB:-700}"
CORE_BROTLI_BUDGET_KB="${WASM_CORE_BROTLI_BUDGET_KB:-230}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${TMPDIR:-/tmp}/gosx-wasm-size"
rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"

# Package initializers survive dead-code elimination. Guard the import closure
# as well as bytes so unused renderer/compiler/collaboration code stays out.
blocked_imports=$(
  cd "${REPO_ROOT}"
  GOWORK=off GOOS=js GOARCH=wasm go list -tags='tinygo gosx_tiny_runtime gosx_runtime_core' -deps ./client/wasm |
    awk '/^(image|regexp|crypto)(\/|$)|^m31labs[.]dev\/gosx\/(crdt|ir)(\/|$)|^m31labs[.]dev\/turboquant(\/|$)/'
)
if [ -n "${blocked_imports}" ]; then
  printf '[check-wasm-size] FAIL: unused core imports\n%s\n' "${blocked_imports}"
  exit 1
fi

echo "[check-wasm-size] building production TinyGo runtimes sequentially"
( cd "${REPO_ROOT}" && GOWORK=off go run ./cmd/gosx build-runtime "${OUT_DIR}" )

over_budget=0
check_budget() {
  local label="$1" path="$2" budget_kb="$3" bytes
  bytes=$(stat -c%s "${path}")
  printf '%s: %s KiB (%s bytes) — budget %s KiB\n' "${label}" "$((bytes / 1024))" "${bytes}" "${budget_kb}"
  if [ "${bytes}" -gt "$((budget_kb * 1024))" ]; then
    echo "[check-wasm-size] FAIL: ${label} runtime over budget"
    over_budget=1
  fi
}

check_budget full "${OUT_DIR}/gosx-runtime.wasm" "${FULL_BUDGET_KB}"
check_budget tiny "${OUT_DIR}/gosx-runtime-islands.wasm" "${TINY_BUDGET_KB}"
check_budget core "${OUT_DIR}/gosx-runtime-core.wasm" "${CORE_BUDGET_KB}"
check_budget 'core brotli' "${OUT_DIR}/gosx-runtime-core.wasm.br" "${CORE_BROTLI_BUDGET_KB}"

if [ "${over_budget}" -ne 0 ]; then
  exit 1
fi

echo "[check-wasm-size] OK"
