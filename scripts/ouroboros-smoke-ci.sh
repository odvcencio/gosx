#!/usr/bin/env sh
set -eu

go_cmd="${GO:-go}"
routes="${OUROBOROS_SMOKE_ROUTES:-R00,R01}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)-$$"
out="${OUROBOROS_SMOKE_OUT:-build/ouroboros/o0.2/browser-smoke-ci/${stamp}}"
command_timeout="${OUROBOROS_SMOKE_COMMAND_TIMEOUT:-180s}"
repo_root="$(pwd -P)"
if [ "${OUROBOROS_SMOKE_BUDGET+x}" = x ]; then
  budget="$OUROBOROS_SMOKE_BUDGET"
else
  budget=""
fi

case "$out" in
  */browser-smoke-ci/*|browser-smoke-ci/*|build/ouroboros/o0.2/browser-smoke-ci/*) ;;
  *)
    echo "ouroboros smoke refuses non-smoke output root: $out" >&2
    exit 1
    ;;
esac

if [ -e "$out" ]; then
  echo "ouroboros smoke output root already exists: $out" >&2
  exit 1
fi

case "$out" in
  /*) ;;
  *) out="$repo_root/$out" ;;
esac

run_bounded() {
  if command -v timeout >/dev/null 2>&1; then
    timeout "$command_timeout" "$@"
  else
    "$@"
  fi
}

assert_smoke_manifest() {
  manifest="$1"
  raw_samples="$2"
  command -v jq >/dev/null 2>&1 || {
    echo "ouroboros smoke requires jq for manifest assertions" >&2
    exit 1
  }
  jq -e '
    .canonical == false
    and ([.corpus.routes[].id] == ["R00", "R01"])
    and .sampling.canonical == false
    and .sampling.coldSamples == 1
    and .sampling.warmSamples == 1
  ' "$manifest" >/dev/null || {
    echo "ouroboros smoke manifest does not contain exactly R00,R01 noncanonical smoke: $manifest" >&2
    exit 1
  }
  jq -s -e '
    length == 4
    and ([.[] | {routeID, cacheMode, sampleLane, sampleIndex, discarded, pilot}]
      | sort_by(.routeID, .cacheMode, .sampleLane, .sampleIndex)
      == [
        {"routeID":"R00","cacheMode":"cold","sampleLane":"product","sampleIndex":0,"discarded":false,"pilot":false},
        {"routeID":"R00","cacheMode":"warm","sampleLane":"product","sampleIndex":0,"discarded":false,"pilot":false},
        {"routeID":"R01","cacheMode":"cold","sampleLane":"product","sampleIndex":0,"discarded":false,"pilot":false},
        {"routeID":"R01","cacheMode":"warm","sampleLane":"product","sampleIndex":0,"discarded":false,"pilot":false}
      ])
  ' "$raw_samples" >/dev/null || {
    echo "ouroboros smoke raw samples do not contain exact R00,R01 cold/warm product matrix: $raw_samples" >&2
    exit 1
  }
}

if [ "${OUROBOROS_SMOKE_ASSERT_ONLY:-}" != "" ]; then
  assert_smoke_manifest "$OUROBOROS_SMOKE_ASSERT_ONLY/manifest.json" "$OUROBOROS_SMOKE_ASSERT_ONLY/perf/raw-samples.jsonl"
  exit 0
fi

run_bounded "$go_cmd" run ./cmd/gosx perf ouroboros \
  --root . \
  --serve \
  --samples smoke \
  --routes "$routes" \
  --out "$out" \
  --environment headless-logic \
  --trace=false \
  --coverage=false \
  --heap-snapshots=false \
  --timeout 60s

manifest="$out/manifest.json"
compare_report="$out/compare.json"

if [ -z "$budget" ]; then
  budget="$out/smoke-budget.v1.json"
  cat > "$budget" <<'JSON'
{
  "schemaVersion": "gosx.ouroboros.compare-budget.v1",
  "contractVersion": "O0.2",
  "defaults": {
    "browser.transferBytes": { "allowedAbs": 0, "allowedPct": 0, "exact": true },
    "runtime.transferBytes": { "allowedAbs": 0, "allowedPct": 0, "exact": true },
    "startup.ttfbMs": { "allowedAbs": 10, "allowedPct": 5 },
    "startup.dclMs": { "allowedAbs": 10, "allowedPct": 5 },
    "startup.fullyLoadedMs": { "allowedAbs": 10, "allowedPct": 5 },
    "startup.firstUsableMs": { "allowedAbs": 10, "allowedPct": 5 },
    "hydration.totalMs": { "allowedAbs": 5, "allowedPct": 5 },
    "hydration.maxIslandMs": { "allowedAbs": 5, "allowedPct": 5 },
    "interaction.dispatchMs": { "allowedAbs": 5, "allowedPct": 5 },
    "interaction.patchCount": { "allowedAbs": 0, "allowedPct": 0, "exact": true },
    "memory.jsHeapUsedMb": { "allowedAbs": 2, "allowedPct": 10 },
    "memory.jsHeapTotalMb": { "allowedAbs": 2, "allowedPct": 10 },
    "memory.domNodeCount": { "allowedAbs": 0, "allowedPct": 0, "exact": true },
    "console.entryCount": { "allowedAbs": 0, "allowedPct": 0, "exact": true }
  }
}
JSON
fi

if ! grep -Eq '"canonical"[[:space:]]*:[[:space:]]*false' "$manifest"; then
  echo "ouroboros smoke did not prove manifest canonical:false: $manifest" >&2
  exit 1
fi

assert_smoke_manifest "$manifest" "$out/perf/raw-samples.jsonl"

run_bounded "$go_cmd" run ./cmd/gosx ouroboros compare \
  --mode smoke \
  --baseline "$manifest" \
  --candidate "$manifest" \
  --budget "$budget" \
  --out "$compare_report"

if ! grep -Eq '"status"[[:space:]]*:[[:space:]]*"pass"' "$compare_report"; then
  echo "ouroboros smoke self-compare did not pass: $compare_report" >&2
  exit 1
fi

echo "ouroboros smoke self-compare passed: $compare_report"
