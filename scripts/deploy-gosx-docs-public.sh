#!/usr/bin/env sh

# Public-path rollback verification for the docs deployment. This helper is
# sourced by deploy-gosx-docs.sh and intentionally does not set shell options.

gosx_docs_wait_for_public_health() {
	gosx_public_curl="$1"
	gosx_public_base_url="${2%/}"
	gosx_public_attempts="${3:-6}"
	gosx_public_delay="${4:-2}"
	gosx_public_sleep="${SLEEP:-sleep}"

	case "$gosx_public_attempts" in
		''|0|*[!0-9]*)
			echo "gosx docs deploy: rollback health attempts must be a positive integer" >&2
			return 1
			;;
	esac
	case "$gosx_public_delay" in
		''|*[!0-9]*)
			echo "gosx docs deploy: rollback health delay must be a non-negative integer" >&2
			return 1
			;;
	esac
	if ! command -v "$gosx_public_curl" >/dev/null 2>&1; then
		echo "gosx docs deploy: rollback health curl command is unavailable: ${gosx_public_curl}" >&2
		return 1
	fi
	if ! command -v "$gosx_public_sleep" >/dev/null 2>&1; then
		echo "gosx docs deploy: rollback health sleep command is unavailable: ${gosx_public_sleep}" >&2
		return 1
	fi

	gosx_public_attempt=1
	while [ "$gosx_public_attempt" -le "$gosx_public_attempts" ]; do
		gosx_public_body=""
		if gosx_public_body="$($gosx_public_curl \
			--fail --silent --show-error --connect-timeout 5 --max-time 10 \
			"${gosx_public_base_url}/healthz" 2>/dev/null)" && \
			printf '%s\n' "$gosx_public_body" | jq -e '.ok == true' >/dev/null 2>&1; then
			echo "gosx docs deploy: rolled-back public health recovered after ${gosx_public_attempt}/${gosx_public_attempts} checks" >&2
			return 0
		fi
		echo "gosx docs deploy: waiting for rolled-back public health (${gosx_public_attempt}/${gosx_public_attempts})" >&2
		if [ "$gosx_public_attempt" -lt "$gosx_public_attempts" ] && [ "$gosx_public_delay" -gt 0 ]; then
			"$gosx_public_sleep" "$gosx_public_delay"
		fi
		gosx_public_attempt=$((gosx_public_attempt + 1))
	done

	echo "gosx docs deploy: captured Deployment was restored, but public health did not recover" >&2
	return 1
}
