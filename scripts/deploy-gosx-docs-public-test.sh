#!/usr/bin/env sh
set -eu

for command_name in awk dirname jq mktemp true; do
	if ! command -v "$command_name" >/dev/null 2>&1; then
		echo "deploy public test: required command is unavailable: ${command_name}" >&2
		exit 1
	fi
done

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
# shellcheck source=deploy-gosx-docs-public.sh
. "${script_dir}/deploy-gosx-docs-public.sh"
wait_script="${script_dir}/wait-gosx-docs-release.sh"
fake_curl="${script_dir}/testdata/fake-gosx-docs-curl.sh"

tmp_dir="$(mktemp -d)"
cleanup() {
	find "$tmp_dir" -type f -delete
	find "$tmp_dir" -depth -type d -empty -delete
}
trap cleanup EXIT INT TERM
counter_file="${tmp_dir}/counter"

framework_version="v0.39.0"
revision="6c9b990f711705707e8bceac52d6c331fe8857ed"
built_at="2026-08-13T06:22:15Z"
public_url="https://gosx.m31labs.dev"

reset_counter() {
	printf '%s\n' 0 >"$counter_file"
}

read_counter() {
	awk 'NR == 1 { print $1 }' "$counter_file"
}

run_wait() {
	GOSX_DOCS_FAKE_CURL_MODE="$1" \
		GOSX_DOCS_FAKE_CURL_STATE="$counter_file" \
		GOSX_DOCS_FAKE_FRAMEWORK_VERSION="$framework_version" \
		GOSX_DOCS_FAKE_REVISION="$revision" \
		GOSX_DOCS_FAKE_BUILT_AT="$built_at" \
		GOSX_DOCS_FAKE_PUBLIC_URL="$public_url" \
		GOSX_DOCS_PUBLIC_CONVERGENCE_ATTEMPTS="$2" \
		GOSX_DOCS_PUBLIC_CONVERGENCE_DELAY_SECONDS=1 \
		GOSX_DOCS_PUBLIC_CONVERGENCE_SUCCESSES="${3:-3}" \
		CURL="$fake_curl" SLEEP=true \
		sh "$wait_script" "$public_url" "$framework_version" \
			"$revision" "$built_at" "$public_url"
}

reset_counter
run_wait release-converges 5
if [ "$(read_counter)" -ne 15 ]; then
	echo "deploy public test: release convergence did not require three stable checks" >&2
	exit 1
fi

reset_counter
run_wait release-flaps 6
if [ "$(read_counter)" -ne 18 ]; then
	echo "deploy public test: an unhealthy edge response did not reset the stability streak" >&2
	exit 1
fi

reset_counter
if run_wait release-health-fails 3; then
	echo "deploy public test: persistently unhealthy release unexpectedly converged" >&2
	exit 1
fi
if [ "$(read_counter)" -ne 9 ]; then
	echo "deploy public test: unhealthy release did not exhaust its bounded checks" >&2
	exit 1
fi

reset_counter
if run_wait release-ready-fails 3; then
	echo "deploy public test: persistently unready release unexpectedly converged" >&2
	exit 1
fi
if [ "$(read_counter)" -ne 9 ]; then
	echo "deploy public test: unready release did not exhaust its bounded checks" >&2
	exit 1
fi

reset_counter
if run_wait release-converges 5 2; then
	echo "deploy public test: fewer than three required successes unexpectedly passed validation" >&2
	exit 1
fi
if [ "$(read_counter)" -ne 0 ]; then
	echo "deploy public test: invalid stability configuration made a public request" >&2
	exit 1
fi

reset_counter
if run_wait release-converges 2 3; then
	echo "deploy public test: successes greater than attempts unexpectedly passed validation" >&2
	exit 1
fi
if [ "$(read_counter)" -ne 0 ]; then
	echo "deploy public test: impossible stability configuration made a public request" >&2
	exit 1
fi

reset_counter
if run_wait release-never-matches 3; then
	echo "deploy public test: incomplete release identity unexpectedly converged" >&2
	exit 1
fi
if [ "$(read_counter)" -ne 9 ]; then
	echo "deploy public test: release mismatch did not exhaust its bounded attempts" >&2
	exit 1
fi

reset_counter
GOSX_DOCS_FAKE_CURL_MODE=health-recovers \
	GOSX_DOCS_FAKE_CURL_STATE="$counter_file" CURL="$fake_curl" SLEEP=true \
	gosx_docs_wait_for_public_health "$fake_curl" "$public_url" 4 1
if [ "$(read_counter)" -ne 3 ]; then
	echo "deploy public test: rollback health did not retry transient failures" >&2
	exit 1
fi

reset_counter
if GOSX_DOCS_FAKE_CURL_MODE=health-never-recovers \
	GOSX_DOCS_FAKE_CURL_STATE="$counter_file" CURL="$fake_curl" SLEEP=true \
	gosx_docs_wait_for_public_health "$fake_curl" "$public_url" 3 1; then
	echo "deploy public test: unhealthy rollback unexpectedly passed" >&2
	exit 1
fi
if [ "$(read_counter)" -ne 3 ]; then
	echo "deploy public test: unhealthy rollback did not exhaust its bounded attempts" >&2
	exit 1
fi

if gosx_docs_wait_for_public_health "$fake_curl" "$public_url" 0 0; then
	echo "deploy public test: zero retry attempts unexpectedly passed" >&2
	exit 1
fi

printf '%s\n' "deploy public test: release convergence and rollback health retries passed"
