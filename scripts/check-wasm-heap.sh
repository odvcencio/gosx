#!/bin/bash
# check-wasm-heap.sh - WASM heap gate for the signal batch path.
#
# TinyGo scans memory conservatively, and it gives every call from JavaScript a
# goroutine whose stack is a block of the garbage-collected heap. Dead stacks
# that a stray word in the program's static data points into stay alive, with
# whatever their frames pointed to, and before the batch path avoided that the
# heap doubled at each collection: 144 MiB, 288, 576, 1,152, with a collection
# of the larger heaps taking 0.5 to 16 seconds.
#
# The gate sends batches of three synthetic payloads (about 2 KB, 8 KB and
# 30 KB) through the page's own signal code to every published production
# runtime (core, engine, collab, full), the way the loaders let the page send
# them, and fails if a run's WASM memory is still growing in the second half
# of the run (by more than HEAP_GATE_GROWTH_MIB) or ends above HEAP_GATE_MAX_MIB.
# It also fails if a runtime does not export the direct batch entry, because
# then the page would fall back to the call that starts a goroutine.
#
# Override with HEAP_GATE_ITERS (batches per payload, default 1000),
# HEAP_GATE_MAX_MIB (default 256) and HEAP_GATE_GROWTH_MIB (default 64). The
# runtimes come from GOSX_WASM_DIR when it holds a build (make wasm-size-budget
# leaves one in ${TMPDIR:-/tmp}/gosx-wasm-size), and are built otherwise.
set -euo pipefail

ITERS="${HEAP_GATE_ITERS:-1000}"
MAX_MIB="${HEAP_GATE_MAX_MIB:-256}"
GROWTH_MIB="${HEAP_GATE_GROWTH_MIB:-64}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${GOSX_WASM_DIR:-${TMPDIR:-/tmp}/gosx-wasm-size}"

if [ ! -f "${OUT_DIR}/gosx-runtime.wasm" ] || [ ! -f "${OUT_DIR}/wasm_exec.js" ]; then
  OUT_DIR="${TMPDIR:-/tmp}/gosx-wasm-heap"
  echo "[check-wasm-heap] building production TinyGo runtimes in ${OUT_DIR}"
  rm -rf "${OUT_DIR}"
  mkdir -p "${OUT_DIR}"
  ( cd "${REPO_ROOT}" && GOWORK=off go run ./cmd/gosx build-runtime "${OUT_DIR}" )
fi

failed=0
for runtime in gosx-runtime-core gosx-runtime-engine gosx-runtime-collab gosx-runtime; do
  echo "[check-wasm-heap] ${runtime}"
  node "${REPO_ROOT}/client/js/signal-batch.bench.mjs" --soak --gate --direct \
    --iters "${ITERS}" --scenario hud,ui,full \
    --max-memory-mib "${MAX_MIB}" --allow-growth-mib "${GROWTH_MIB}" \
    --label "${runtime#gosx-}" \
    --wasm "${OUT_DIR}/${runtime}.wasm" --wasm-exec "${OUT_DIR}/wasm_exec.js" || failed=1
done

if [ "${failed}" -ne 0 ]; then
  echo "[check-wasm-heap] FAIL"
  exit 1
fi
echo "[check-wasm-heap] OK"
