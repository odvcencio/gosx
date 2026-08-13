#!/usr/bin/env sh
set -eu

base_url="${1:-}"
expected_framework_version="${2:-}"
expected_revision="${3:-}"
expected_built_at="${4:-}"
expected_public_url="${5:-$base_url}"
base_url="${base_url%/}"
expected_public_url="${expected_public_url%/}"
attempts="${GOSX_DOCS_PUBLIC_CONVERGENCE_ATTEMPTS:-12}"
delay_seconds="${GOSX_DOCS_PUBLIC_CONVERGENCE_DELAY_SECONDS:-5}"
curl_cmd="${CURL:-curl}"
sleep_cmd="${SLEEP:-sleep}"

if [ -z "$base_url" ] || [ -z "$expected_framework_version" ] || \
	[ -z "$expected_revision" ] || [ -z "$expected_built_at" ] || \
	[ -z "$expected_public_url" ]; then
	echo "usage: $0 BASE_URL FRAMEWORK_VERSION REVISION BUILT_AT [PUBLIC_URL]" >&2
	exit 2
fi
case "$attempts" in
	''|0|*[!0-9]*)
		echo "gosx docs deploy: public convergence attempts must be a positive integer" >&2
		exit 2
		;;
esac
case "$delay_seconds" in
	''|*[!0-9]*)
		echo "gosx docs deploy: public convergence delay must be a non-negative integer" >&2
		exit 2
		;;
esac
for command_name in "$curl_cmd" "$sleep_cmd" jq; do
	if ! command -v "$command_name" >/dev/null 2>&1; then
		echo "gosx docs deploy: public convergence command is unavailable: ${command_name}" >&2
		exit 2
	fi
done

attempt=1
observed="unavailable"
while [ "$attempt" -le "$attempts" ]; do
	body=""
	if body="$($curl_cmd \
		--fail --silent --show-error --connect-timeout 5 --max-time 15 \
		--header 'cache-control: no-cache' \
		"${base_url}/api/site" 2>/dev/null)" && \
		printf '%s\n' "$body" | jq -e \
			--arg frameworkVersion "$expected_framework_version" \
			--arg revision "$expected_revision" \
			--arg builtAt "$expected_built_at" \
			--arg publicURL "$expected_public_url" \
			'.site == "gosx-docs" and .status == "ok" and
			 .frameworkVersion == $frameworkVersion and
			 .revision == $revision and .builtAt == $builtAt and
			 (.publicURL | rtrimstr("/")) == $publicURL' >/dev/null 2>&1; then
		echo "gosx docs deploy: public release converged after ${attempt}/${attempts} checks" >&2
		exit 0
	fi

	if observed_value="$(printf '%s\n' "$body" | jq -er \
		'[.frameworkVersion, .revision, .builtAt, .publicURL] | map(tostring) | join(" ")' 2>/dev/null)"; then
		observed="$observed_value"
	else
		observed="unavailable"
	fi
	echo "gosx docs deploy: waiting for public release (${attempt}/${attempts}); observed ${observed}" >&2
	if [ "$attempt" -lt "$attempts" ] && [ "$delay_seconds" -gt 0 ]; then
		"$sleep_cmd" "$delay_seconds"
	fi
	attempt=$((attempt + 1))
done

echo "gosx docs deploy: public release did not converge to ${expected_framework_version} ${expected_revision} (${expected_built_at})" >&2
exit 1
