#!/bin/bash
# check-wasm-size.sh — WASM size-budget CI gate.
#
# Measures the PRODUCTION runtime artifacts — the TinyGo builds that actually
# ship (gosx-runtime.wasm + gosx-runtime-islands.wasm) — and asserts they stay
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
# Override a planned-growth budget by exporting WASM_FULL_BUDGET_KB and/or
# WASM_TINY_BUDGET_KB. Any deliberate increase should be recorded in an ADR.
set -euo pipefail

# TinyGo production runtime budgets. Historical reference (pre-engine-surface,
# wasm-opt -Oz): full ~862 KB, islands ~425 KB. The budgets below leave room
# for the engine-surface runtime and for CI environments without wasm-opt
# (which produces a larger, unoptimized binary). These are provisional — once
# CI reports the actual sizes, tighten them toward the observed value + ~15%.
FULL_BUDGET_KB="${WASM_FULL_BUDGET_KB:-5500}"
TINY_BUDGET_KB="${WASM_TINY_BUDGET_KB:-3200}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${TMPDIR:-/tmp}/gosx-wasm-size"
rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"

echo "[check-wasm-size] building production TinyGo runtimes (full + islands-only)"
( cd "${REPO_ROOT}" && go run ./cmd/gosx build-runtime "${OUT_DIR}" )

FULL_PATH="${OUT_DIR}/gosx-runtime.wasm"
TINY_PATH="${OUT_DIR}/gosx-runtime-islands.wasm"

full_bytes=$(stat -c%s "${FULL_PATH}")
tiny_bytes=$(stat -c%s "${TINY_PATH}")
full_kb=$((full_bytes / 1024))
tiny_kb=$((tiny_bytes / 1024))

printf 'full:  %s KB (%s bytes) — budget %s KB\n' "${full_kb}" "${full_bytes}" "${FULL_BUDGET_KB}"
printf 'tiny:  %s KB (%s bytes) — budget %s KB\n' "${tiny_kb}" "${tiny_bytes}" "${TINY_BUDGET_KB}"

over_budget=0
if [ "${full_kb}" -gt "${FULL_BUDGET_KB}" ]; then
  echo "[check-wasm-size] FAIL: full runtime over budget (${full_kb} KB > ${FULL_BUDGET_KB} KB)"
  over_budget=1
fi
if [ "${tiny_kb}" -gt "${TINY_BUDGET_KB}" ]; then
  echo "[check-wasm-size] FAIL: tiny runtime over budget (${tiny_kb} KB > ${TINY_BUDGET_KB} KB)"
  over_budget=1
fi

if [ "${over_budget}" -ne 0 ]; then
  echo "[check-wasm-size] To raise the budget intentionally, export"
  echo "  WASM_FULL_BUDGET_KB and/or WASM_TINY_BUDGET_KB (record the bump in an ADR)."
  exit 1
fi

echo "[check-wasm-size] OK"
