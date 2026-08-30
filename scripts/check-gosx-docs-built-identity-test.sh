#!/usr/bin/env sh
set -eu

for command_name in curl dirname jq mktemp ps python3 sleep true; do
	if ! command -v "$command_name" >/dev/null 2>&1; then
		echo "gosx docs built identity test: required command is unavailable: ${command_name}" >&2
		exit 1
	fi
done

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
identity_script="${script_dir}/check-gosx-docs-built-identity.sh"
fake_curl="${script_dir}/testdata/fake-gosx-docs-built-identity-curl.sh"
fake_app="${script_dir}/testdata/fake-gosx-docs-built-identity-app.py"
tmp_dir="$(mktemp -d)"
cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

framework_version="v0.53.9"
revision="1111111111111111111111111111111111111111"
built_at="2026-08-30T00:00:00Z"
public_url="https://gosx.m31labs.dev"

run_identity() {
	GOSX_DOCS_FAKE_IDENTITY_MODE="$1" \
		GOSX_DOCS_FAKE_FRAMEWORK_VERSION="$framework_version" \
		GOSX_DOCS_FAKE_REVISION="$revision" \
		GOSX_DOCS_FAKE_BUILT_AT="$built_at" \
		GOSX_DOCS_FAKE_PUBLIC_URL="$public_url" \
		CURL="$fake_curl" SLEEP=true \
		sh "$identity_script" \
			--root "$tmp_dir" \
			--app "$tmp_dir/fake-app" \
			--framework-version "$framework_version" \
			--revision "$revision" \
			--built-at "$built_at" \
			--public-url "$public_url" \
			--base-url "http://127.0.0.1:65535"
}

run_identity ok >/dev/null

run_real_identity() {
	mode="$1"
	shift
	GOSX_FAKE_IDENTITY_APP_MODE="$mode" \
		GOSX_DOCS_REVISION_FRAMEWORK_VERSION="$framework_version" \
		GOSX_DOCS_IDENTITY_PORT="${GOSX_DOCS_IDENTITY_PORT:-}" \
		sh "$identity_script" \
			--root "$tmp_dir" \
			--app "$fake_app" \
			--framework-version "$framework_version" \
			--revision "$revision" \
			--built-at "$built_at" \
			--public-url "$public_url" \
			"$@"
}

pid_file="${tmp_dir}/identity.pid"
secret_log="${tmp_dir}/identity-secret.log"
GOSX_FAKE_IDENTITY_APP_PID_FILE="$pid_file" \
	GOSX_FAKE_IDENTITY_APP_SECRET_LOG="$secret_log" \
	SESSION_SECRET="disposable-test-secret" \
	run_real_identity ok >/dev/null
identity_pid="$(cat "$pid_file")"
if kill -0 "$identity_pid" >/dev/null 2>&1; then
	echo "gosx docs built identity test: successful local identity server was not cleaned up" >&2
	exit 1
fi
if ! grep -Fx "disposable-test-secret" "$secret_log" >/dev/null; then
	echo "gosx docs built identity test: local identity server did not receive the disposable secret" >&2
	exit 1
fi

if GOSX_FAKE_IDENTITY_APP_MODE=exit-after-site \
	GOSX_DOCS_REVISION_FRAMEWORK_VERSION="$framework_version" \
	GOSX_DOCS_IDENTITY_LIVENESS_DELAY=1 \
	sh "$identity_script" \
		--root "$tmp_dir" \
		--app "$fake_app" \
		--framework-version "$framework_version" \
		--revision "$revision" \
		--built-at "$built_at" \
		--public-url "$public_url" >"${tmp_dir}/exit-after-site.out" 2>&1; then
	echo "gosx docs built identity test: server exit after successful probe unexpectedly passed" >&2
	cat "${tmp_dir}/exit-after-site.out" >&2
	exit 1
fi
if ! grep -F "built docs server exited after successful /api/site request" "${tmp_dir}/exit-after-site.out" >/dev/null; then
	echo "gosx docs built identity test: server exit failure did not prove post-request liveness" >&2
	cat "${tmp_dir}/exit-after-site.out" >&2
	exit 1
fi

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
GOSX_FAKE_IDENTITY_APP_MODE=wrong-responder \
	GOSX_DOCS_REVISION_FRAMEWORK_VERSION="$framework_version" \
	PORT="127.0.0.1:${port}" \
	"$fake_app" >"${tmp_dir}/wrong-responder.log" 2>&1 &
wrong_pid="$!"
wrong_ready=0
wrong_attempt=1
while [ "$wrong_attempt" -le 30 ]; do
	if curl --fail --silent --show-error "http://127.0.0.1:${port}/readyz" >/dev/null 2>&1; then
		wrong_ready=1
		break
	fi
	if ! kill -0 "$wrong_pid" >/dev/null 2>&1; then
		echo "gosx docs built identity test: wrong responder exited before readiness" >&2
		cat "${tmp_dir}/wrong-responder.log" >&2
		exit 1
	fi
	sleep 1
	wrong_attempt=$((wrong_attempt + 1))
done
if [ "$wrong_ready" -ne 1 ]; then
	echo "gosx docs built identity test: wrong responder did not become ready" >&2
	cat "${tmp_dir}/wrong-responder.log" >&2
	exit 1
fi
if GOSX_DOCS_IDENTITY_PORT="$port" \
	GOSX_DOCS_IDENTITY_LIVENESS_DELAY=1 \
	GOSX_FAKE_IDENTITY_APP_MODE=ok \
	GOSX_DOCS_REVISION_FRAMEWORK_VERSION="$framework_version" \
	sh "$identity_script" \
		--root "$tmp_dir" \
		--app "$fake_app" \
		--framework-version "$framework_version" \
		--revision "$revision" \
		--built-at "$built_at" \
		--public-url "$public_url" >"${tmp_dir}/port-collision.out" 2>&1; then
	kill "$wrong_pid" >/dev/null 2>&1 || true
	wait "$wrong_pid" >/dev/null 2>&1 || true
	echo "gosx docs built identity test: port collision with wrong responder unexpectedly passed" >&2
	cat "${tmp_dir}/port-collision.out" >&2
	exit 1
fi
kill "$wrong_pid" >/dev/null 2>&1 || true
wait "$wrong_pid" >/dev/null 2>&1 || true
if ! grep -E "built docs server exited after successful /readyz request|/api/site does not match the intended deployment identity" "${tmp_dir}/port-collision.out" >/dev/null; then
	echo "gosx docs built identity test: port collision with wrong responder was not rejected by liveness or identity" >&2
	cat "${tmp_dir}/port-collision.out" >&2
	exit 1
fi

if GOSX_DOCS_IDENTITY_PORT="-1" run_real_identity ok >"${tmp_dir}/bad-port.out" 2>&1; then
	echo "gosx docs built identity test: option-like identity port unexpectedly passed" >&2
	cat "${tmp_dir}/bad-port.out" >&2
	exit 1
fi
if ! grep -F "GOSX_DOCS_IDENTITY_PORT must be a numeric TCP port" "${tmp_dir}/bad-port.out" >/dev/null; then
	echo "gosx docs built identity test: bad port failure did not explain numeric requirement" >&2
	cat "${tmp_dir}/bad-port.out" >&2
	exit 1
fi

for mode in stale-revision unknown-revision wrong-framework wrong-built-at wrong-public-url unhealthy unready malformed-site; do
	if run_identity "$mode" >"${tmp_dir}/${mode}.out" 2>&1; then
		echo "gosx docs built identity test: ${mode} unexpectedly passed" >&2
		cat "${tmp_dir}/${mode}.out" >&2
		exit 1
	fi
	case "$mode" in
		unhealthy)
			want="/healthz is not healthy"
			;;
		unready)
			want="/readyz is not ready"
			;;
		*)
			want="/api/site does not match"
			;;
	esac
	if ! grep -F "$want" "${tmp_dir}/${mode}.out" >/dev/null; then
		echo "gosx docs built identity test: ${mode} failure did not explain ${want}" >&2
		cat "${tmp_dir}/${mode}.out" >&2
		exit 1
	fi
done

echo "gosx docs built identity test: all checks passed"
