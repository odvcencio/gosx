#!/usr/bin/env sh
set -eu

mode="${GOSX_DOCS_FAKE_CURL_MODE:?}"
state_file="${GOSX_DOCS_FAKE_CURL_STATE:?}"
framework_version="${GOSX_DOCS_FAKE_FRAMEWORK_VERSION:-v0.39.0}"
revision="${GOSX_DOCS_FAKE_REVISION:-6c9b990f711705707e8bceac52d6c331fe8857ed}"
built_at="${GOSX_DOCS_FAKE_BUILT_AT:-2026-08-13T06:22:15Z}"
public_url="${GOSX_DOCS_FAKE_PUBLIC_URL:-https://gosx.m31labs.dev}"

count="$(awk 'NR == 1 { print $1 }' "$state_file")"
count=$((count + 1))
printf '%s\n' "$count" >"$state_file"
request_url=""
for request_arg in "$@"; do
	request_url="$request_arg"
done

write_current_release() {
	printf '{"site":"gosx-docs","status":"ok","frameworkVersion":"%s","revision":"%s","builtAt":"%s","publicURL":"%s/"}\n' \
		"$framework_version" "$revision" "$built_at" "$public_url"
}

write_endpoint_success() {
	case "$request_url" in
		*/api/site) write_current_release ;;
		*/healthz|*/readyz) printf '%s\n' '{"ok":true}' ;;
		*) echo "fake curl: unexpected URL ${request_url}" >&2; exit 2 ;;
	esac
}

case "$mode" in
	release-converges)
		case "$count" in
			1) exit 28 ;;
			4)
				printf '%s\n' '{"site":"gosx-docs","status":"ok","frameworkVersion":"v0.32.0","revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","builtAt":"2026-07-19T00:00:00Z","publicURL":"https://gosx.m31labs.dev"}'
				;;
			*) write_endpoint_success ;;
		esac
		;;
	release-flaps)
		case "$count" in
			2|8) exit 22 ;;
			*) write_endpoint_success ;;
		esac
		;;
	release-health-fails)
		case "$request_url" in
			*/healthz) exit 22 ;;
			*) write_endpoint_success ;;
		esac
		;;
	release-ready-fails)
		case "$request_url" in
			*/readyz) printf '%s\n' '{"ok":false}' ;;
			*) write_endpoint_success ;;
		esac
		;;
	release-never-matches)
		case "$request_url" in
			*/api/site)
				# Matching framework and revision are insufficient: builtAt
				# belongs to the immutable deployment identity too.
				printf '{"site":"gosx-docs","status":"ok","frameworkVersion":"%s","revision":"%s","builtAt":"2026-08-13T00:00:00Z","publicURL":"%s"}\n' \
					"$framework_version" "$revision" "$public_url"
				;;
			*/healthz|*/readyz) printf '%s\n' '{"ok":true}' ;;
			*) echo "fake curl: unexpected URL ${request_url}" >&2; exit 2 ;;
		esac
		;;
	health-recovers)
		case "$count" in
			1) exit 28 ;;
			2) printf '%s\n' '{"ok":false}' ;;
			*) printf '%s\n' '{"ok":true}' ;;
		esac
		;;
	health-never-recovers)
		printf '%s\n' '{"ok":false}'
		;;
	*)
		echo "fake curl: unknown mode ${mode}" >&2
		exit 2
		;;
esac
