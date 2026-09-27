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

if [[ -n "${SHOWCASE_FEATURED_PATH:-}" ]]; then
  featured_path=$SHOWCASE_FEATURED_PATH
elif curl -fsS "$base_url/demos/tabletop/" >/dev/null; then
  featured_path=/demos/tabletop/
else
  featured_path=/demos/water/
fi
tabletop_path=
if [[ "$featured_path" == "/demos/tabletop/" ]]; then tabletop_path=/demos/tabletop/; fi
gpu_paths=${SHOWCASE_GPU_PATHS:-"$tabletop_path /demos/showreel/ /demos/checkers/ /demos/beacon/ /demos/orrery/ /demos/water/ /demos/scene3d/ /demos/scene3d-bench/ /demos/html-surface/"}

if [[ ! -d "$dist_dir" ]]; then
  echo "production build not found at $dist_dir" >&2
  exit 1
fi
if [[ ! -x "$tools_dir/lh.sh" || ! -f "$tools_dir/gpu-capture.js" ]]; then
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
nice -n 10 "$tools_dir/lh.sh" "$base_url" "$evidence_dir/lighthouse" 3 "${pages[@]}"

read -r -a gpu_path_list <<< "$gpu_paths"
CDP_URL="$cdp_url" nice -n 10 node "$tools_dir/gpu-capture.js" \
  "$base_url" "$evidence_dir/gpu" showcase-receipts "${gpu_path_list[@]}"

quickstart_tmp=$(mktemp -d "$evidence_dir/quickstart/cache.XXXXXX")
mkdir -p "$quickstart_tmp/bin"
GOWORK=off GOMODCACHE="$quickstart_tmp/mod" GOCACHE="$quickstart_tmp/build" GOBIN="$quickstart_tmp/bin" \
  nice -n 10 /usr/bin/time -f '%e' -o "$evidence_dir/quickstart/cold.seconds" \
  go install m31labs.dev/gosx/cmd/gosx@latest \
  > "$evidence_dir/quickstart/cold.log" 2>&1
GOWORK=off GOMODCACHE="$quickstart_tmp/mod" GOCACHE="$quickstart_tmp/build" GOBIN="$quickstart_tmp/bin" \
  nice -n 10 /usr/bin/time -f '%e' -o "$evidence_dir/quickstart/warm.seconds" \
  go install m31labs.dev/gosx/cmd/gosx@latest \
  > "$evidence_dir/quickstart/warm.log" 2>&1

cpu_model=$(awk -F: '/model name/ {sub(/^ /, "", $2); print $2; exit}' /proc/cpuinfo)
memory_gib=$(awk '/MemTotal/ {printf "%.2f", $2/1024/1024}' /proc/meminfo)
machine_name="WSL2 VM · $(nproc) vCPU"
os_name="$(uname -sr)"
measured_at=$(date -Iseconds)
load_average_end=$(awk '{print $1 " " $2 " " $3}' /proc/loadavg)
load_average="start: $load_average_start; end: $load_average_end"
commit=$(git -C "$repo_root" rev-parse HEAD)
meta_file="$evidence_dir/measurement-meta.json"
python3 - "$meta_file" "$measured_at" "$commit" "$machine_name" "$os_name" "$cpu_model" "$memory_gib" "$load_average" "$featured_path" "$evidence_dir/quickstart/cold.seconds" "$evidence_dir/quickstart/warm.seconds" <<'PY'
import json, pathlib, sys

out, measured, commit, machine, os_name, cpu, memory, load, featured, cold, warm = sys.argv[1:]
pathlib.Path(out).write_text(json.dumps({
    "measuredAt": measured,
    "commit": commit,
    "machine": {"name": machine, "os": os_name, "cpu": cpu, "memoryGiB": float(memory), "loadAverage": load},
    "featuredPath": featured,
    "quickstart": {"coldSeconds": float(pathlib.Path(cold).read_text()), "warmSeconds": float(pathlib.Path(warm).read_text())},
}, indent=2) + "\n")
PY

node "$repo_root/scripts/showcase-receipts.mjs" \
  "$evidence_dir" "$dist_dir" "$repo_root/examples/gosx-docs/app/performance/receipts.json"
rm -rf "$quickstart_tmp"
echo "Wrote examples/gosx-docs/app/performance/receipts.json from $evidence_dir"
echo "For a complete /performance receipt, rebuild the site with this JSON and rerun once."
