#!/bin/bash
# check-wasm-size.sh — Phase 1d WASM size-budget CI gate.
#
# Builds both flavors of client/wasm (full + islands-only/tiny) and asserts
# the resulting WebAssembly artifacts stay within budget. The gate fires when
# either flavor exceeds its budget so that planned growth (e.g. Phase 2's
# <CanvasBoard> primitive, future opcode expansion) requires a deliberate
# budget bump rather than slipping in unnoticed.
#
# Override the budget for a planned-growth slice by exporting WASM_FULL_BUDGET_KB
# and/or WASM_TINY_BUDGET_KB. Any budget increase >10% relative to the
# Phase 1c shipped sizes (full=7883412 bytes, tiny=5618126 bytes) requires an
# ADR per the Phase 1d plan.
set -euo pipefail

# Phase 1d baseline budgets:
#   - Phase 1c shipped: full=7883412 bytes (~7699 KB), tiny=5618126 bytes (~5486 KB).
#   - +5% headroom + room for Phase 1d's unified dispatcher additions yields
#     the budgets below. Phase 2's <CanvasBoard> primitive may need a one-time
#     bump (track via ADR).
FULL_BUDGET_KB="${WASM_FULL_BUDGET_KB:-8500}"
TINY_BUDGET_KB="${WASM_TINY_BUDGET_KB:-5900}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${TMPDIR:-/tmp}"
FULL_PATH="${OUT_DIR}/gosx-wasm-full.wasm"
TINY_PATH="${OUT_DIR}/gosx-wasm-tiny.wasm"

echo "[check-wasm-size] building full client/wasm"
GOOS=js GOARCH=wasm go build -C "${REPO_ROOT}" -o "${FULL_PATH}" ./client/wasm

echo "[check-wasm-size] building tiny client/wasm (gosx_tiny_islands_only)"
GOOS=js GOARCH=wasm go build -C "${REPO_ROOT}" -tags gosx_tiny_islands_only -o "${TINY_PATH}" ./client/wasm

full_bytes=$(stat -c%s "${FULL_PATH}")
tiny_bytes=$(stat -c%s "${TINY_PATH}")
full_kb=$((full_bytes / 1024))
tiny_kb=$((tiny_bytes / 1024))

printf 'full:  %s KB (%s bytes) — budget %s KB\n' "${full_kb}" "${full_bytes}" "${FULL_BUDGET_KB}"
printf 'tiny:  %s KB (%s bytes) — budget %s KB\n' "${tiny_kb}" "${tiny_bytes}" "${TINY_BUDGET_KB}"

over_budget=0
if [ "${full_kb}" -gt "${FULL_BUDGET_KB}" ]; then
  echo "[check-wasm-size] FAIL: full WASM over budget (${full_kb} KB > ${FULL_BUDGET_KB} KB)"
  over_budget=1
fi
if [ "${tiny_kb}" -gt "${TINY_BUDGET_KB}" ]; then
  echo "[check-wasm-size] FAIL: tiny WASM over budget (${tiny_kb} KB > ${TINY_BUDGET_KB} KB)"
  over_budget=1
fi

if [ "${over_budget}" -ne 0 ]; then
  echo "[check-wasm-size] To raise the budget intentionally, export"
  echo "  WASM_FULL_BUDGET_KB and/or WASM_TINY_BUDGET_KB (require an ADR for any"
  echo "  increase >10% over the Phase 1c baseline)."
  exit 1
fi

echo "[check-wasm-size] OK"
