#!/usr/bin/env sh
set -eu

mode="${GOSX_DOCS_FAKE_IDENTITY_MODE:?}"
framework_version="${GOSX_DOCS_FAKE_FRAMEWORK_VERSION:-v0.53.9}"
revision="${GOSX_DOCS_FAKE_REVISION:-1111111111111111111111111111111111111111}"
built_at="${GOSX_DOCS_FAKE_BUILT_AT:-2026-08-30T00:00:00Z}"
public_url="${GOSX_DOCS_FAKE_PUBLIC_URL:-https://gosx.m31labs.dev}"

request_url=""
for request_arg in "$@"; do
	request_url="$request_arg"
done

site_json() {
	printf '{"site":"gosx-docs","status":"ok","apiVersion":"1","framework":"GoSX","frameworkVersion":"%s","revision":"%s","builtAt":"%s","runtime":"go1.26.0","publicURL":"%s"}\n' \
		"$1" "$2" "$3" "$4"
}

case "$request_url" in
	*/api/site)
		case "$mode" in
			ok) site_json "$framework_version" "$revision" "$built_at" "$public_url" ;;
			stale-revision) site_json "$framework_version" "2222222222222222222222222222222222222222" "$built_at" "$public_url" ;;
			unknown-revision) site_json "$framework_version" "unknown" "$built_at" "$public_url" ;;
			wrong-framework) site_json "v0.1.0" "$revision" "$built_at" "$public_url" ;;
			wrong-built-at) site_json "$framework_version" "$revision" "2026-08-29T00:00:00Z" "$public_url" ;;
			wrong-public-url) site_json "$framework_version" "$revision" "$built_at" "https://wrong.example.test" ;;
			malformed-site) printf '%s\n' 'not json' ;;
			*) site_json "$framework_version" "$revision" "$built_at" "$public_url" ;;
		esac
		;;
	*/healthz)
		case "$mode" in
			unhealthy) printf '%s\n' '{"ok":false}' ;;
			*) printf '%s\n' '{"ok":true}' ;;
		esac
		;;
	*/readyz)
		case "$mode" in
			unready) printf '%s\n' '{"ok":false}' ;;
			*) printf '%s\n' '{"ok":true}' ;;
		esac
		;;
	*)
		echo "fake identity curl: unexpected URL ${request_url}" >&2
		exit 2
		;;
esac
