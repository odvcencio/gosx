#!/usr/bin/env sh
# wire-gate.sh builds two production apps, serves them, and checks every
# budgeted route against perf/budgets/wire.json with cmd wiregate:
#
#   scaffold  a fresh `gosx init` app plus one counter-island page
#             (perf/wire/testdata/counter)
#   docs      examples/gosx-docs
#
# The measurement needs no browser: it fetches each page and every resource
# on its load path with `Accept-Encoding: br, gzip`, so bytes, request counts,
# compression, cache and cookie headers are deterministic for a given build.
#
# Environment:
#   WIRE_GATE_MODE=check|update  update rewrites the budget to the
#                                measurements (limits only move down).
#   WIRE_GATE_BASE_BUDGET=FILE   base-branch budget for the ratchet check.
#   WIRE_GATE_REUSE_DOCS=1       reuse an existing examples/gosx-docs/dist.
#   WIRE_GATE_OUT=DIR            report directory (default build/wire-gate).
set -eu

mode="${WIRE_GATE_MODE:-check}"
out="${WIRE_GATE_OUT:-build/wire-gate}"
scaffold_port="${WIRE_GATE_SCAFFOLD_PORT:-8742}"
docs_port="${WIRE_GATE_DOCS_PORT:-8743}"
go_cmd="${GO:-go}"
root="$(pwd)"

mkdir -p "$out"
out="$(CDPATH= cd -- "$out" && pwd)"
gosx_bin="$out/gosx"
wiregate_bin="$out/wiregate"
# The scaffold lives outside the repository so repository-wide checks (gofmt,
# go list) never see it.
scaffold_dir="$(mktemp -d "${TMPDIR:-/tmp}/gosx-wire-gate.XXXXXX")/scaffold"
pids=""
cleanup() {
	for pid in $pids; do
		kill "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
	done
	rm -rf "$(dirname "$scaffold_dir")"
}
trap cleanup EXIT INT TERM

GOWORK=off "$go_cmd" build -o "$gosx_bin" ./cmd/gosx
GOWORK=off "$go_cmd" build -o "$wiregate_bin" ./perf/wire/cmd/wiregate

"$gosx_bin" init "$scaffold_dir" --module example.com/wiregate >/dev/null
mkdir -p "$scaffold_dir/app/counter"
cp perf/wire/testdata/counter/page.gsx perf/wire/testdata/counter/page.server.go "$scaffold_dir/app/counter/"
(cd "$scaffold_dir" && GOWORK=off nice -n 10 "$gosx_bin" build --prod . >"$out/scaffold-build.log" 2>&1) || {
	echo "wire-gate: scaffold production build failed" >&2
	tail -n 50 "$out/scaffold-build.log" >&2
	exit 1
}

if [ "${WIRE_GATE_REUSE_DOCS:-}" != "1" ] || [ ! -x examples/gosx-docs/dist/run.sh ]; then
	GOWORK=off nice -n 10 "$gosx_bin" build --prod ./examples/gosx-docs >"$out/docs-build.log" 2>&1 || {
		echo "wire-gate: docs production build failed" >&2
		tail -n 50 "$out/docs-build.log" >&2
		exit 1
	}
fi


start() {
	name="$1"
	dir="$2"
	port="$3"
	ready="$4"
	PORT="$port" \
	PUBLIC_URL="http://127.0.0.1:${port}" \
	SESSION_SECRET="wire-gate-session-secret-0123456789" \
		"$dir/dist/run.sh" >"$out/${name}-server.log" 2>&1 &
	pid=$!
	pids="$pids $pid"
	deadline=$(( $(date +%s) + 60 ))
	until curl -fsS "http://127.0.0.1:${port}${ready}" >/dev/null 2>&1; do
		if ! kill -0 "$pid" 2>/dev/null; then
			echo "wire-gate: $name server exited before readiness" >&2
			cat "$out/${name}-server.log" >&2 || true
			exit 1
		fi
		if [ "$(date +%s)" -ge "$deadline" ]; then
			echo "wire-gate: timed out waiting for $name" >&2
			cat "$out/${name}-server.log" >&2 || true
			exit 1
		fi
		sleep 0.25
	done
}

start scaffold "$scaffold_dir" "$scaffold_port" /api/health
start docs "$root/examples/gosx-docs" "$docs_port" /readyz

write_flag=""
if [ "$mode" = "update" ]; then
	write_flag="-write"
fi

status=0
"$wiregate_bin" check \
	-budget perf/budgets/wire.json \
	-app "scaffold=http://127.0.0.1:${scaffold_port}" \
	-app "docs=http://127.0.0.1:${docs_port}" \
	-report "$out/wire-report.json" \
	-markdown "$out/wire-report.md" \
	$write_flag || status=$?

if [ -n "${WIRE_GATE_BASE_BUDGET:-}" ]; then
	"$wiregate_bin" ratchet -base "$WIRE_GATE_BASE_BUDGET" -head perf/budgets/wire.json || status=1
fi

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	{
		echo "## Wire gate"
		echo
		cat "$out/wire-report.md"
	} >>"$GITHUB_STEP_SUMMARY"
fi
exit "$status"
