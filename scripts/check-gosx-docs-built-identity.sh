#!/usr/bin/env sh
set -eu

usage() {
	echo "usage: check-gosx-docs-built-identity.sh --root DIST --app APP --framework-version VERSION --revision SHA --built-at RFC3339 --public-url URL [--base-url URL]" >&2
}

dist_root=""
app_path=""
framework_version=""
revision=""
built_at=""
public_url=""
base_url=""
curl_cmd="${CURL:-curl}"
sleep_cmd="${SLEEP:-sleep}"
python_cmd="${PYTHON:-python3}"
identity_port="${GOSX_DOCS_IDENTITY_PORT:-}"
liveness_delay="${GOSX_DOCS_IDENTITY_LIVENESS_DELAY:-}"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--root)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			dist_root="$2"
			shift 2
			;;
		--app)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			app_path="$2"
			shift 2
			;;
		--framework-version)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			framework_version="$2"
			shift 2
			;;
		--revision)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			revision="$2"
			shift 2
			;;
		--built-at)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			built_at="$2"
			shift 2
			;;
		--public-url)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			public_url="${2%/}"
			shift 2
			;;
		--base-url)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			base_url="${2%/}"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			usage
			echo "gosx docs built identity: unexpected argument: $1" >&2
			exit 2
			;;
	esac
done

for pair in "root:$dist_root" "app:$app_path" "framework version:$framework_version" "revision:$revision" "built at:$built_at" "public url:$public_url"; do
	label="${pair%%:*}"
	value="${pair#*:}"
	if [ -z "$value" ]; then
		echo "gosx docs built identity: ${label} is required" >&2
		exit 2
	fi
done

for command_name in "$curl_cmd" jq; do
	if ! command -v "$command_name" >/dev/null 2>&1; then
		echo "gosx docs built identity: required command is unavailable: ${command_name}" >&2
		exit 2
	fi
done

tmp_dir="$(mktemp -d)"
pid=""
started_server=0
cleanup() {
	if [ -n "$pid" ]; then
		kill "$pid" >/dev/null 2>&1 || true
		wait "$pid" >/dev/null 2>&1 || true
	fi
	rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

if [ -z "$base_url" ]; then
	for command_name in "$python_cmd" "$sleep_cmd" ps; do
		if ! command -v "$command_name" >/dev/null 2>&1; then
			echo "gosx docs built identity: required command is unavailable: ${command_name}" >&2
			exit 2
		fi
	done
	if [ ! -x "$app_path" ]; then
		echo "gosx docs built identity: app is not executable: ${app_path}" >&2
		exit 1
	fi
	if [ ! -d "$dist_root" ]; then
		echo "gosx docs built identity: dist root is missing: ${dist_root}" >&2
		exit 1
	fi
	if [ -n "$identity_port" ]; then
		case "$identity_port" in
			*[!0123456789]*)
				echo "gosx docs built identity: GOSX_DOCS_IDENTITY_PORT must be a numeric TCP port" >&2
				exit 2
				;;
		esac
		port="$identity_port"
	else
		port="$("$python_cmd" -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
	fi
	base_url="http://127.0.0.1:${port}"
	GOSX_APP_ROOT="$dist_root" \
		PORT="127.0.0.1:${port}" \
		PUBLIC_URL="$public_url" \
		GOSX_DOCS_REVISION="$revision" \
		GOSX_DOCS_BUILT_AT="$built_at" \
		SESSION_SECRET="${SESSION_SECRET:-gosx-docs-local-identity-secret}" \
		"$app_path" >"${tmp_dir}/server.log" 2>&1 &
	pid="$!"
	started_server=1
fi

fetch_json() {
	path="$1"
	output="$2"
	if ! "$curl_cmd" --fail --silent --show-error --connect-timeout 2 --max-time 5 \
		--header 'cache-control: no-cache' "${base_url}${path}" >"$output"; then
		if [ -s "${tmp_dir}/server.log" ]; then
			tail -n 20 "${tmp_dir}/server.log" >&2 || true
		fi
		return 1
	fi
	assert_server_alive "$path"
}

assert_server_alive() {
	probe_name="$1"
	if [ -n "$liveness_delay" ]; then
		"$sleep_cmd" "$liveness_delay"
	fi
	server_dead=0
	if [ "$started_server" -eq 1 ]; then
		if ! kill -0 "$pid" >/dev/null 2>&1; then
			server_dead=1
		else
			server_state="$(ps -p "$pid" -o stat= 2>/dev/null || true)"
			case "$server_state" in
				Z*) server_dead=1 ;;
			esac
		fi
	fi
	if [ "$server_dead" -eq 1 ]; then
		echo "gosx docs built identity: built docs server exited after successful ${probe_name} request" >&2
		if [ -s "${tmp_dir}/server.log" ]; then
			tail -n 40 "${tmp_dir}/server.log" >&2 || true
		fi
		exit 1
	fi
}

if [ "$started_server" -eq 1 ]; then
	ready=0
	attempt=1
	while [ "$attempt" -le 30 ]; do
		if "$curl_cmd" --fail --silent --show-error --connect-timeout 2 --max-time 5 \
			--header 'cache-control: no-cache' "${base_url}/readyz" >"${tmp_dir}/ready-probe.json" 2>/dev/null; then
			assert_server_alive "/readyz"
			if jq -e '.ok == true' "${tmp_dir}/ready-probe.json" >/dev/null 2>&1; then
				ready=1
				break
			fi
		fi
		if [ -n "$pid" ] && ! kill -0 "$pid" >/dev/null 2>&1; then
			echo "gosx docs built identity: built docs server exited before readiness" >&2
			if [ -s "${tmp_dir}/server.log" ]; then
				tail -n 40 "${tmp_dir}/server.log" >&2 || true
			fi
			exit 1
		fi
		"$sleep_cmd" 1
		attempt=$((attempt + 1))
	done
	if [ "$ready" -ne 1 ]; then
		echo "gosx docs built identity: built docs server did not become ready" >&2
		exit 1
	fi
fi

fetch_json "/api/site" "${tmp_dir}/site.json"
fetch_json "/healthz" "${tmp_dir}/health.json"
fetch_json "/readyz" "${tmp_dir}/ready.json"

if ! jq -e \
	--arg frameworkVersion "$framework_version" \
	--arg revision "$revision" \
	--arg builtAt "$built_at" \
	--arg publicURL "$public_url" \
	'.site == "gosx-docs" and .status == "ok" and
	 .apiVersion == "1" and .framework == "GoSX" and
	 .frameworkVersion == $frameworkVersion and
	 .revision == $revision and .builtAt == $builtAt and
	 (.publicURL | rtrimstr("/")) == $publicURL' \
	"${tmp_dir}/site.json" >/dev/null; then
	echo "gosx docs built identity: /api/site does not match the intended deployment identity" >&2
	cat "${tmp_dir}/site.json" >&2
	exit 1
fi
if ! jq -e '.ok == true' "${tmp_dir}/health.json" >/dev/null; then
	echo "gosx docs built identity: /healthz is not healthy" >&2
	cat "${tmp_dir}/health.json" >&2
	exit 1
fi
if ! jq -e '.ok == true' "${tmp_dir}/ready.json" >/dev/null; then
	echo "gosx docs built identity: /readyz is not ready" >&2
	cat "${tmp_dir}/ready.json" >&2
	exit 1
fi

echo "gosx docs built identity: ${framework_version} ${revision} ${built_at} is locally ready"
