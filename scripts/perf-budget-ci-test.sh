#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"

cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fake_go="$tmp_dir/fake-go"
cat >"$fake_go" <<'EOF'
#!/usr/bin/env sh
set -eu

if [ "$#" -ge 4 ] && [ "$1" = "run" ] && [ "$2" = "./cmd/gosx" ] && [ "$3" = "dev" ]; then
	if [ "$*" != "run ./cmd/gosx dev ./examples/gosx-docs" ]; then
		echo "unexpected dev invocation: $*" >&2
		exit 98
	fi
	trap 'if [ -n "${FAKE_SERVER_STOP_FILE:-}" ]; then echo stopped >"$FAKE_SERVER_STOP_FILE"; fi; exit 0' TERM INT
	echo "fake docs server starting"
	while :; do sleep 1; done
fi

if [ "$#" -ge 4 ] && [ "$1" = "run" ] && [ "$2" = "./cmd/gosx" ] && [ "$3" = "perf" ]; then
	expected="run ./cmd/gosx perf --mobile pixel7 --throttle 4 --coverage --timeout 45s --budget perf/budgets/default.json --json http://127.0.0.1:3971/docs/getting-started http://127.0.0.1:3971/demos/water"
	if [ "$*" != "$expected" ]; then
		echo "unexpected perf invocation: $*" >&2
		echo "expected: $expected" >&2
		exit 97
	fi
	printf '{"url":"http://127.0.0.1:3971/docs/getting-started","timestamp":"2026-08-30T00:00:00Z","pages":[{"url":"http://127.0.0.1:3971/docs/getting-started","fullyLoadedMs":1}]}\n'
	exit "${FAKE_PERF_STATUS:-0}"
fi

echo "unexpected fake go invocation: $*" >&2
exit 99
EOF
chmod 700 "$fake_go"

fake_curl="$tmp_dir/curl"
cat >"$fake_curl" <<'EOF'
#!/usr/bin/env sh
set -eu
exit 0
EOF
chmod 700 "$fake_curl"

run_case() {
	name="$1"
	status="$2"
	want_status="$3"
	case_dir="$tmp_dir/$name"
	mkdir -p "$case_dir"
	out="$case_dir/perf-report.json"
	log="$case_dir/perf-server.log"
	err="$case_dir/stderr.txt"
	stopped="$case_dir/server-stopped.txt"
	if (
		cd "$repo_root"
		PATH="$tmp_dir:$PATH" \
		GO="$fake_go" \
		FAKE_PERF_STATUS="$status" \
		FAKE_SERVER_STOP_FILE="$stopped" \
		PERF_PORT=3971 \
		PERF_BASE_URL=http://127.0.0.1:3971 \
		PERF_URLS="http://127.0.0.1:3971/docs/getting-started http://127.0.0.1:3971/demos/water" \
		PERF_FLAGS="--mobile pixel7 --throttle 4 --coverage --timeout 45s" \
		PERF_OUT="$out" \
		PERF_LOG="$log" \
		sh scripts/perf-budget-ci.sh
	) 2>"$err"; then
		got_status=0
	else
		got_status=$?
	fi
	if [ "$got_status" -ne "$want_status" ]; then
		echo "perf-budget-ci-test: $name got status $got_status, want $want_status" >&2
		cat "$err" >&2 || true
		exit 1
	fi
	wait_count=0
	while [ ! -s "$stopped" ] && [ "$wait_count" -lt 20 ]; do
		sleep 0.1
		wait_count=$((wait_count + 1))
	done
	if [ ! -s "$stopped" ]; then
		echo "perf-budget-ci-test: $name did not clean up the docs server" >&2
		cat "$err" >&2 || true
		exit 1
	fi
	if [ ! -s "$out" ]; then
		echo "perf-budget-ci-test: $name did not preserve a JSON report" >&2
		exit 1
	fi
	if [ "$want_status" -ne 0 ]; then
		if ! grep -q "preserved JSON report" "$err"; then
			echo "perf-budget-ci-test: $name did not report preserved JSON" >&2
			cat "$err" >&2 || true
			exit 1
		fi
		if ! grep -q "server log tail" "$err"; then
			echo "perf-budget-ci-test: $name did not print server log tail" >&2
			cat "$err" >&2 || true
			exit 1
		fi
	fi
}

run_case success 0 0
run_case budget_failure 7 7

if ! grep -q "Upload perf budget failure diagnostics" "$repo_root/.github/workflows/ci.yml" ||
	! grep -q "if: failure()" "$repo_root/.github/workflows/ci.yml" ||
	! grep -q "actions/upload-artifact@v4" "$repo_root/.github/workflows/ci.yml" ||
	! grep -q "retention-days: 7" "$repo_root/.github/workflows/ci.yml"; then
	echo "perf-budget-ci-test: CI workflow no longer uploads bounded failure diagnostics" >&2
	exit 1
fi

echo "perf-budget-ci-test: ok"
