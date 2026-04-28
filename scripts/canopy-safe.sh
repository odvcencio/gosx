#!/usr/bin/env bash
set -euo pipefail

CANOPY_BIN="${CANOPY:-canopy}"
CANOPY_TIMEOUT="${CANOPY_TIMEOUT:-120s}"
CANOPY_GOMAXPROCS="${CANOPY_GOMAXPROCS:-2}"
CANOPY_GOMEMLIMIT="${CANOPY_GOMEMLIMIT:-1536MiB}"
CANOPY_MAX_VMEM_KB="${CANOPY_MAX_VMEM_KB:-4194304}"

if [[ $# -lt 1 ]]; then
  cat >&2 <<'USAGE'
usage: scripts/canopy-safe.sh <canopy args...>

Runs canopy with the repo's .canopyignore, a wall-clock timeout, and a
virtual-memory ceiling so accidental whole-repo parses fail locally instead of
putting the machine at OOM risk.

Environment:
  CANOPY               canopy binary name/path (default: canopy)
  CANOPY_TIMEOUT       timeout duration passed to timeout(1) (default: 120s)
  CANOPY_GOMAXPROCS    Go scheduler width for canopy (default: 2)
  CANOPY_GOMEMLIMIT    Go soft heap target for canopy (default: 1536MiB)
  CANOPY_MAX_VMEM_KB   ulimit -v hard ceiling in KB (default: 4194304)
USAGE
  exit 2
fi

if ! command -v "$CANOPY_BIN" >/dev/null 2>&1; then
  echo "canopy-safe: $CANOPY_BIN not found on PATH" >&2
  exit 127
fi

run_canopy() {
  ulimit -v "$CANOPY_MAX_VMEM_KB" 2>/dev/null || \
    echo "canopy-safe: warning: ulimit -v unsupported; relying on GOMEMLIMIT and timeout" >&2
  GOMAXPROCS="$CANOPY_GOMAXPROCS" GOMEMLIMIT="$CANOPY_GOMEMLIMIT" exec "$CANOPY_BIN" "$@"
}

timeout_seconds() {
  case "$CANOPY_TIMEOUT" in
    *s) printf '%s\n' "${CANOPY_TIMEOUT%s}" ;;
    *m) printf '%s\n' "$(( ${CANOPY_TIMEOUT%m} * 60 ))" ;;
    *h) printf '%s\n' "$(( ${CANOPY_TIMEOUT%h} * 3600 ))" ;;
    *) printf '%s\n' "$CANOPY_TIMEOUT" ;;
  esac
}

if command -v timeout >/dev/null 2>&1; then
  timeout --preserve-status "$CANOPY_TIMEOUT" bash -c '
    set -euo pipefail
    CANOPY_BIN="$1"
    CANOPY_MAX_VMEM_KB="$2"
    CANOPY_GOMAXPROCS="$3"
    CANOPY_GOMEMLIMIT="$4"
    shift 4
    ulimit -v "$CANOPY_MAX_VMEM_KB" 2>/dev/null || \
      echo "canopy-safe: warning: ulimit -v unsupported; relying on GOMEMLIMIT and timeout" >&2
    GOMAXPROCS="$CANOPY_GOMAXPROCS" GOMEMLIMIT="$CANOPY_GOMEMLIMIT" exec "$CANOPY_BIN" "$@"
  ' bash "$CANOPY_BIN" "$CANOPY_MAX_VMEM_KB" "$CANOPY_GOMAXPROCS" "$CANOPY_GOMEMLIMIT" "$@"
else
  seconds="$(timeout_seconds)"
  if ! [[ "$seconds" =~ ^[0-9]+$ ]] || [[ "$seconds" -le 0 ]]; then
    echo "canopy-safe: unsupported CANOPY_TIMEOUT=$CANOPY_TIMEOUT without timeout(1)" >&2
    exit 2
  fi

  (
    run_canopy "$@"
  ) &
  child=$!

  (
    sleep "$seconds"
    if kill -0 "$child" >/dev/null 2>&1; then
      echo "canopy-safe: timed out after ${CANOPY_TIMEOUT}; terminating canopy" >&2
      kill -TERM "$child" >/dev/null 2>&1 || true
      sleep 2
      kill -KILL "$child" >/dev/null 2>&1 || true
    fi
  ) &
  timer=$!

  status=0
  wait "$child" || status=$?
  kill "$timer" >/dev/null 2>&1 || true
  wait "$timer" >/dev/null 2>&1 || true
  exit "$status"
fi
