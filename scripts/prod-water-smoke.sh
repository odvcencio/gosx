#!/usr/bin/env sh
set -eu

port="${WATER_PROD_SMOKE_PORT:-8128}"
base_url="http://127.0.0.1:${port}"
app_dir="examples/gosx-docs"
dist_dir="${app_dir}/dist"
log="${WATER_PROD_SMOKE_LOG:-build/water-prod-smoke.log}"
go_cmd="${GO:-go}"

mkdir -p "$(dirname "$log")"
rm -f "$log"

"$go_cmd" run ./cmd/gosx build --prod "$app_dir"

test -x "${dist_dir}/run.sh"
test -x "${dist_dir}/server/app"
test -f "${dist_dir}/build.json"

PORT="$port" \
PUBLIC_URL="$base_url" \
SESSION_SECRET="${SESSION_SECRET:-gosx-water-prod-smoke-secret}" \
	"${dist_dir}/run.sh" >"$log" 2>&1 &
server_pid=$!

cleanup() {
	if kill -0 "$server_pid" 2>/dev/null; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
}
trap cleanup EXIT INT TERM

deadline=$(( $(date +%s) + 45 ))
while ! curl -fsS "${base_url}/readyz" >/dev/null 2>&1; do
	if ! kill -0 "$server_pid" 2>/dev/null; then
		echo "water production smoke: server exited before readiness" >&2
		cat "$log" >&2 || true
		exit 1
	fi
	if [ "$(date +%s)" -ge "$deadline" ]; then
		echo "water production smoke: timed out waiting for readiness" >&2
		cat "$log" >&2 || true
		exit 1
	fi
	sleep 0.25
done

page="$(curl -fsS "${base_url}/demos/water")"
printf '%s' "$page" | grep -q 'data-gosx-scene3d'

for asset_name in bootstrap-feature-scene3d bootstrap-feature-scene3d-webgpu; do
	asset_src="$(printf '%s' "$page" | grep -oE "src=\"[^\"]*${asset_name}\.[^\"]+\.js\"" | head -n 1 | cut -d'"' -f2 || true)"
	if [ -z "$asset_src" ]; then
		echo "water production smoke: missing hashed ${asset_name} script src" >&2
		exit 1
	fi
	case "$asset_src" in
		http://*|https://*) asset_url="$asset_src" ;;
		/*) asset_url="${base_url}${asset_src}" ;;
		*) asset_url="${base_url}/demos/water/${asset_src}" ;;
	esac
	asset_file="$(mktemp)"
	if ! curl -fsS "$asset_url" -o "$asset_file" || [ ! -s "$asset_file" ]; then
		rm -f "$asset_file"
		echo "water production smoke: ${asset_name} asset is unavailable or empty: ${asset_url}" >&2
		exit 1
	fi
	rm -f "$asset_file"
done

printf '%s\n' "water production smoke: /demos/water and hashed Scene3D assets returned nonempty 200 responses"
