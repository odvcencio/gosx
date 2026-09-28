#!/usr/bin/env bash
set -euo pipefail

# Regenerate the public receipts from a production-shaped docs build. Start the
# lane's Windows Chrome and CDP bridge first; this script records Lighthouse,
# real-GPU frame, bundle, and install measurements into one checked-in JSON.

repo_root=$(git rev-parse --show-toplevel)
tools_dir=${SHOWCASE_TOOLS_DIR:-/home/draco/.local/state/nightwatch/reports/gosx-showcase/tools}
base_url=${SHOWCASE_BASE_URL:-http://localhost:8118}
evidence_dir=${SHOWCASE_EVIDENCE_DIR:-/home/draco/.local/state/nightwatch/reports/gosx-showcase/lanes/showcase-system/receipts}
dist_dir=${SHOWCASE_DIST_DIR:-$repo_root/examples/gosx-docs/dist}
cdp_url=${CDP_URL:-http://172.29.240.1:8119}

if [[ -n "$(git -C "$repo_root" status --porcelain)" ]]; then
  echo "commit the source tree before generating receipts so the recorded SHA names the measured build" >&2
  exit 1
fi

route_status() {
  curl -sS --max-time 5 -o /dev/null -w '%{http_code}' "$base_url$1" || true
}

tabletop_path=
if [[ "$(route_status /demos/tabletop/)" == "200" ]]; then
  tabletop_path=/demos/tabletop/
elif [[ "$(route_status /demos/tabletop)" == "200" ]]; then
  tabletop_path=/demos/tabletop
fi
if [[ -n "${SHOWCASE_FEATURED_PATH:-}" ]]; then
  featured_path=$SHOWCASE_FEATURED_PATH
elif [[ -n "$tabletop_path" ]]; then
  featured_path=$tabletop_path
else
  featured_path=/demos/water
fi

if [[ -n "${SHOWCASE_GPU_PATHS:-}" ]]; then
  read -r -a gpu_path_list <<< "$SHOWCASE_GPU_PATHS"
else
  gpu_path_list=(/demos/showreel/ /demos/checkers/ /demos/beacon /demos/orrery/ /demos/water /demos/scene3d/ /demos/scene3d-bench /demos/html-surface/)
  if [[ -n "$tabletop_path" ]]; then gpu_path_list=("$tabletop_path" "${gpu_path_list[@]}"); fi
fi

if [[ ! -d "$dist_dir" ]]; then
  echo "production build not found at $dist_dir" >&2
  exit 1
fi
if [[ ! -x "$tools_dir/lh.sh" || ! -f "$repo_root/scripts/showcase-gpu-capture.mjs" ]]; then
  echo "shared showcase tools not found at $tools_dir" >&2
  exit 1
fi
mkdir -p "$evidence_dir/lighthouse" "$evidence_dir/gpu" "$evidence_dir/quickstart"
if ! curl -fsS "$base_url/api/site" >/dev/null; then
  echo "site is not responding at $base_url" >&2
  exit 1
fi
if ! curl -fsS "$cdp_url/json/version" > "$evidence_dir/gpu/browser-version.json"; then
  echo "Windows Chrome CDP is not responding at $cdp_url" >&2
  exit 1
fi

load_average_start=$(awk '{print $1 " " $2 " " $3}' /proc/loadavg)

pages=(/ /docs /docs/getting-started/ /demos/ "$featured_path" /capabilities/ /performance/)
load_wait_started=$(date +%s)
while :; do
	load_average_lighthouse_sample=$(cat /proc/loadavg)
	load_average_lighthouse_start=$(awk '{print $1 " " $2 " " $3}' <<<"$load_average_lighthouse_sample")
	load_one_minute=$(awk '{print $1}' <<<"$load_average_lighthouse_start")
	if awk -v value="$load_one_minute" 'BEGIN { exit !(value < 30) }'; then
		break
	fi
	load_wait_elapsed=$(( $(date +%s) - load_wait_started ))
	if (( load_wait_elapsed >= 2700 )); then
		echo "Lighthouse has waited 45 minutes; measuring under load average $load_one_minute" >&2
		break
	fi
	echo "Lighthouse waiting for 1-minute load average below 30; current load is $load_average_lighthouse_start" >&2
	sleep 60
done
echo "Lighthouse batch starts at /proc/loadavg: $load_average_lighthouse_start"
nice -n 10 "$tools_dir/lh.sh" "$base_url" "$evidence_dir/lighthouse" 3 "${pages[@]}"
load_average_lighthouse_end=$(awk '{print $1 " " $2 " " $3}' /proc/loadavg)
echo "Lighthouse batch ends at /proc/loadavg: $load_average_lighthouse_end"

CDP_URL="$cdp_url" SHOWCASE_TOOLS_DIR="$tools_dir" nice -n 10 node "$repo_root/scripts/showcase-gpu-capture.mjs" \
  "$base_url" "$evidence_dir/gpu" showcase-receipts "${gpu_path_list[@]}"

quickstart_tmp=$(mktemp -d "$evidence_dir/quickstart/cache.XXXXXX")
mkdir -p "$quickstart_tmp/bin"
run_go_install() {
  local label=$1
  local attempt=1
  local log_file="$evidence_dir/quickstart/$label.log"
  local seconds_file="$evidence_dir/quickstart/$label.seconds"
  while true; do
    if GOWORK=off GOMODCACHE="$quickstart_tmp/mod" GOCACHE="$quickstart_tmp/build" GOBIN="$quickstart_tmp/bin" \
      nice -n 10 /usr/bin/time -f '%e' -o "$seconds_file" \
      go install m31labs.dev/gosx/cmd/gosx@latest > "$log_file" 2>&1; then
      return 0
    fi
    if ! grep -Eqi 'EAI_AGAIN|no such host|temporary failure in name resolution|i/o timeout|connection timed out|network is unreachable|TLS handshake timeout|proxyconnect tcp|connection reset by peer' "$log_file"; then
      cat "$log_file" >&2
      return 1
    fi
    if (( attempt >= 5 )); then
      cat "$log_file" >&2
      return 1
    fi
    echo "network error during $label quickstart install; retrying in 60 seconds (attempt $((attempt + 1))/5)" >&2
    sleep 60
    attempt=$((attempt + 1))
  done
}
run_go_install cold
run_go_install warm

cpu_model=$(awk -F: '/model name/ {sub(/^ /, "", $2); print $2; exit}' /proc/cpuinfo)
memory_gib=$(awk '/MemTotal/ {printf "%.2f", $2/1024/1024}' /proc/meminfo)
machine_name="WSL2 VM · $(nproc) vCPU"
os_name="$(uname -sr)"
measured_at=$(date -Iseconds)
load_average_end=$(awk '{print $1 " " $2 " " $3}' /proc/loadavg)
load_average="start: $load_average_start; end: $load_average_end"
commit=$(git -C "$repo_root" rev-parse HEAD)
tree=$(git -C "$repo_root" rev-parse "$commit^{tree}")
meta_file="$evidence_dir/measurement-meta.json"
python3 - "$meta_file" "$measured_at" "$commit" "$tree" "$machine_name" "$os_name" "$cpu_model" "$memory_gib" "$load_average" "$load_average_lighthouse_start" "$load_average_lighthouse_end" "$featured_path" "$evidence_dir/quickstart/cold.seconds" "$evidence_dir/quickstart/warm.seconds" <<'PY'
import json, pathlib, sys

out, measured, commit, tree, machine, os_name, cpu, memory, load, lighthouse_start, lighthouse_end, featured, cold, warm = sys.argv[1:]
pathlib.Path(out).write_text(json.dumps({
    "measuredAt": measured,
    "commit": commit,
    "tree": tree,
    "machine": {"name": machine, "os": os_name, "cpu": cpu, "memoryGiB": float(memory), "loadAverage": load},
    "lighthouseLoadAverageStart": lighthouse_start,
    "lighthouseLoadAverageEnd": lighthouse_end,
    "featuredPath": featured,
    "quickstart": {"coldSeconds": float(pathlib.Path(cold).read_text()), "warmSeconds": float(pathlib.Path(warm).read_text())},
}, indent=2) + "\n")
PY

node "$repo_root/scripts/showcase-receipts.mjs" \
  "$evidence_dir" "$dist_dir" "$repo_root/examples/gosx-docs/app/performance/receipts.json"
python3 - "$quickstart_tmp" <<'PY'
import os, shutil, sys

def make_removable(function, filename, _error):
    parent = os.path.dirname(filename) or "."
    try:
        os.chmod(parent, 0o700)
    except OSError:
        pass
    try:
        os.chmod(filename, 0o700)
    except OSError:
        pass
    function(filename)

shutil.rmtree(sys.argv[1], onerror=make_removable)
PY
echo "Wrote examples/gosx-docs/app/performance/receipts.json from $evidence_dir"
echo "For a complete /performance receipt, rebuild the site with this JSON and rerun once."
