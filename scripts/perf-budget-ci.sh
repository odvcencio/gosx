#!/usr/bin/env sh
set -eu

port="${PERF_PORT:-3071}"
base_url="${PERF_BASE_URL:-http://127.0.0.1:${port}}"
urls="${PERF_URLS:-${base_url}/docs/getting-started ${base_url}/demos/water}"
budget="${PERF_BUDGET:-perf/budgets/default.json}"
out="${PERF_OUT:-build/perf-report.json}"
flags="${PERF_FLAGS:---mobile pixel7 --throttle 4 --coverage --timeout 45s}"
log="${PERF_LOG:-build/perf-server.log}"
go_cmd="${GO:-go}"

mkdir -p "$(dirname "$out")" "$(dirname "$log")"
rm -f "$log"

PORT="$port" \
PUBLIC_URL="$base_url" \
SESSION_SECRET="${SESSION_SECRET:-gosx-perf-budget-ci-secret}" \
	"$go_cmd" run ./cmd/gosx dev ./examples/gosx-docs >"$log" 2>&1 &
server_pid=$!

cleanup() {
	if kill -0 "$server_pid" 2>/dev/null; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
}
trap cleanup EXIT INT TERM

deadline=$(( $(date +%s) + 45 ))
ready=""
while [ "$(date +%s)" -lt "$deadline" ]; do
	if curl -fsS "${base_url}/readyz" >/dev/null 2>&1; then
		ready="true"
		break
	fi
	if ! kill -0 "$server_pid" 2>/dev/null; then
		echo "gosx perf-budget-ci: docs server exited before readiness" >&2
		cat "$log" >&2 || true
		exit 1
	fi
	sleep 0.25
done

if [ "$ready" != "true" ]; then
	echo "gosx perf-budget-ci: timed out waiting for ${base_url}/readyz" >&2
	cat "$log" >&2 || true
	exit 1
fi

# PERF_FLAGS and PERF_URLS intentionally split on shell words so callers can
# pass the same flag and URL lists used by `make perf-budget`.
# shellcheck disable=SC2086
if GOSX_CHROME_NO_SANDBOX="${GOSX_CHROME_NO_SANDBOX:-1}" \
	"$go_cmd" run ./cmd/gosx perf $flags --budget "$budget" --json $urls >"$out"; then
	exit 0
else
	status=$?
	echo "gosx perf-budget-ci: perf collection or budget gate failed with status ${status}" >&2
	if [ -s "$out" ]; then
		echo "gosx perf-budget-ci: preserved JSON report at ${out}" >&2
		if command -v jq >/dev/null 2>&1; then
			if jq empty "$out" >/dev/null 2>&1; then
				echo "gosx perf-budget-ci: report JSON is parseable" >&2
			else
				echo "gosx perf-budget-ci: report JSON is not parseable" >&2
			fi
		fi
	else
		echo "gosx perf-budget-ci: no perf report was written to ${out}" >&2
	fi
	if [ -s "$log" ]; then
		echo "gosx perf-budget-ci: server log tail from ${log}" >&2
		tail -n 200 "$log" >&2 || true
	fi
	exit "$status"
fi
