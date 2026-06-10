#!/usr/bin/env bash
# run.sh — build the WASM and start the validation harness.
#
# Usage (from the gosx repo root):
#
#   bash examples/canvasboard-webgpu-verify/run.sh
#
# Then open http://localhost:8765 in Windows Chrome (WSL localhost forwarding).
# The page auto-validates and POSTs results to /report.
# Results are written to /tmp/gosx-webgpu-verify/.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WASM_OUT="/tmp/gosx-webgpu-verify/gosx-runtime.wasm"

echo "[run.sh] repo root: ${REPO_ROOT}"
echo ""
echo "=== Step 1: Build full WASM (standard Go, !gosx_tiny_islands_only included) ==="
echo "    Command: GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w'"
echo "             -o ${WASM_OUT} m31labs.dev/gosx/client/wasm"
echo "    (cmd/gosx/build.go:662-668 — goWASMBuildArgs)"
echo ""

mkdir -p "$(dirname "${WASM_OUT}")"

( cd "${REPO_ROOT}" && \
  GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" \
    -o "${WASM_OUT}" \
    m31labs.dev/gosx/client/wasm \
)

echo "[run.sh] WASM built: ${WASM_OUT} ($(du -sh "${WASM_OUT}" | cut -f1))"
echo ""
echo "=== Step 2: Start validation server on 0.0.0.0:8765 ==="
echo "    Open http://localhost:8765 in Windows Chrome."
echo "    Report → /tmp/gosx-webgpu-verify/report.json"
echo "    Screenshot → /tmp/gosx-webgpu-verify/board.png"
echo ""

( cd "${REPO_ROOT}" && go run ./examples/canvasboard-webgpu-verify )
