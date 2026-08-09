#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 4 ]]; then
  cat >&2 <<'USAGE'
usage: scripts/ouroboros-pixel-evidence.sh <route-id> <backend> <artifact-dir> <url> [extra gosx visual flags...]

Examples:
  scripts/ouroboros-pixel-evidence.sh R08 webgl build/ouroboros/o0.2/pixels/R08 http://127.0.0.1:8080/scene/basic
  scripts/ouroboros-pixel-evidence.sh R10 webgpu build/ouroboros/o0.2/pixels/R10 http://127.0.0.1:3000/demos/water
USAGE
  exit 2
fi

route_id="$1"
backend="$2"
artifact_dir="$3"
url="$4"
shift 4

backend_flags=()
if [[ "$backend" == "webgl" ]]; then
  backend_flags+=(--ouroboros-force-webgl)
fi

go run ./cmd/gosx visual \
  --ouroboros-route-id "$route_id" \
  --require-backend "$backend" \
  --ouroboros-pixels-out "$artifact_dir" \
  "${backend_flags[@]}" \
  "$@" \
  "$url"
