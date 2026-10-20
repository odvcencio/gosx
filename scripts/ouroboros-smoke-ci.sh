#!/usr/bin/env sh
set -eu

go_cmd="${GO:-go}"
routes="${OUROBOROS_SMOKE_ROUTES:-R00,R01,R02,R08}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)-$$"
out="${OUROBOROS_SMOKE_OUT:-build/ouroboros/o0.2/browser-smoke-ci/${stamp}}"
command_timeout="${OUROBOROS_SMOKE_COMMAND_TIMEOUT:-180s}"
repo_root="$(pwd -P)"
budget="${OUROBOROS_SMOKE_BUDGET:-perf/ouroboros/budgets.v1.json}"
baseline="${OUROBOROS_SMOKE_BASELINE:-}"

if [ -z "$baseline" ]; then
  echo "OUROBOROS_SMOKE_BASELINE must name a committed versioned smoke manifest" >&2
  exit 1
fi

case "$baseline" in
  /*)
    case "$baseline" in
      "$repo_root"/*) baseline_rel="${baseline#"$repo_root"/}" ;;
      *) echo "ouroboros smoke baseline must be inside the repository: $baseline" >&2; exit 1 ;;
    esac
    ;;
  *) baseline_rel="$baseline" ;;
esac
if ! git ls-files --error-unmatch -- "$baseline_rel" >/dev/null 2>&1; then
  echo "ouroboros smoke baseline is not committed: $baseline_rel" >&2
  exit 1
fi
if ! git diff --quiet HEAD -- "$(dirname "$baseline_rel")"; then
  echo "ouroboros smoke baseline has uncommitted changes: $baseline_rel" >&2
  exit 1
fi
baseline="$repo_root/$baseline_rel"

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

if ! grep -Eq '"canonical"[[:space:]]*:[[:space:]]*false' "$manifest"; then
  echo "ouroboros smoke did not prove manifest canonical:false: $manifest" >&2
  exit 1
fi

run_bounded "$go_cmd" run ./cmd/gosx ouroboros compare \
  --mode smoke \
  --baseline "$baseline" \
  --candidate "$manifest" \
  --budget "$budget" \
  --out "$compare_report"

if ! grep -Eq '"status"[[:space:]]*:[[:space:]]*"pass"' "$compare_report"; then
  echo "ouroboros smoke committed-baseline compare did not pass: $compare_report" >&2
  exit 1
fi

echo "ouroboros smoke committed-baseline compare passed: $compare_report"
