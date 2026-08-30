#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

go test ./internal/perfbrowserpin
sh scripts/check-perf-browser-pin.sh .github/workflows/ci.yml >/dev/null

echo "check-perf-browser-pin-test: ok"
