#!/usr/bin/env sh
set -eu

browser_path="${1:-}"
action_version="${2:-}"
receipt="${3:-build/perf-browser-identity.txt}"
expected_snapshot="1688711"
expected_version="154.0.8034.0"

actual_status=0
actual_product=""
if [ -z "$browser_path" ]; then
	actual_status=127
	actual_product="browser path is empty"
elif actual_product="$("$browser_path" --version 2>&1)"; then
	actual_status=0
else
	actual_status=$?
fi
normalized_product="$(printf '%s\n' "$actual_product" | sed 's/[[:space:]]*$//')"

mkdir -p "$(dirname "$receipt")"
{
	printf 'configuredSnapshot=%s\n' "$expected_snapshot"
	printf 'actionVersion=%s\n' "$action_version"
	printf 'cliProduct=%s\n' "$actual_product"
	printf 'cliProductNormalized=%s\n' "$normalized_product"
	printf 'cliStatus=%s\n' "$actual_status"
	printf 'path=%s\n' "$browser_path"
} | tee "$receipt"

if [ "$action_version" != "$expected_version" ]; then
	echo "verify-perf-browser-identity: action version mismatch: got $action_version, want $expected_version" >&2
	exit 1
fi
if [ "$actual_status" -ne 0 ] || [ "$normalized_product" != "Chromium ${expected_version}" ]; then
	echo "verify-perf-browser-identity: CLI identity mismatch: status $actual_status product '$normalized_product', want 'Chromium ${expected_version}'" >&2
	exit 1
fi

echo "verify-perf-browser-identity: configured snapshot $expected_snapshot resolved to Chromium $expected_version"
