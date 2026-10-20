#!/usr/bin/env sh
set -eu

workflow="${1:-.github/workflows/ci.yml}"
case "$workflow" in
	/*) ;;
	*) workflow="$(pwd)/$workflow" ;;
esac

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
exec go run ./internal/perfbrowserpin/cmd "$workflow"
