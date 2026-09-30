#!/usr/bin/env bash
set -euo pipefail

# Re-measure the docs performance receipt on a WSL host whose Linux browsers are
# blocked. Lighthouse runs in WSL and drives headless Windows Chrome over
# DevTools; the GPU cadence capture drives a second headless Windows Chrome on
# the NVIDIA GPU. showcase-receipts.sh runs unchanged: this wrapper only points
# it at a tools directory whose lh.sh is the Windows Chrome version, and at the
# GPU Chrome's DevTools relay. Because showcase-receipts.sh is a measurement
# input of the receipt gate, this wrapper must not edit it.
#
# Usage, from a clean, committed worktree with the docs site already built and
# served (see showcase-receipts.sh for SHOWCASE_BASE_URL and SHOWCASE_DIST_DIR):
#   GOSX_RECEIPT_BROWSER=windows-cdp scripts/showcase-receipts-windows.sh
#
# Environment:
#   GOSX_RECEIPT_LANE          Windows profile root name under C:\Temp (default gosx-receipts)
#   GOSX_RECEIPT_GPU_PORT      GPU Chrome DevTools port on Windows loopback (default 8291)
#   GOSX_RECEIPT_GPU_BRIDGE    relay port for the GPU Chrome (default 8292)
#   GOSX_RECEIPT_LH_PORT       Lighthouse Chrome DevTools port (default 8293)
#   GOSX_RECEIPT_LH_BRIDGE     relay port for the Lighthouse Chrome (default 8294)
#   SHOWCASE_TOOLS_DIR         directory holding node_modules with lighthouse,
#                              chrome-launcher, and playwright (same as showcase-receipts.sh)

if [[ "${GOSX_RECEIPT_BROWSER:-windows-cdp}" != "windows-cdp" ]]; then
  echo "GOSX_RECEIPT_BROWSER=${GOSX_RECEIPT_BROWSER} is not supported here; run scripts/showcase-receipts.sh directly" >&2
  exit 2
fi

script_dir=$(cd "$(dirname "$(readlink -f "$0")")" && pwd)
repo_root=$(git rev-parse --show-toplevel)
# shellcheck source=showcase-windows-cdp.sh
source "$script_dir/showcase-windows-cdp.sh"

tools_dir=${SHOWCASE_TOOLS_DIR:?set SHOWCASE_TOOLS_DIR to a directory holding lh.sh plus node_modules with lighthouse and playwright}
evidence_dir=${SHOWCASE_EVIDENCE_DIR:-${TMPDIR:-/tmp}/gosx-showcase-receipts}
gpu_port=${GOSX_RECEIPT_GPU_PORT:-8291}
gpu_bridge=${GOSX_RECEIPT_GPU_BRIDGE:-8292}
lh_port=${GOSX_RECEIPT_LH_PORT:-8293}
lh_bridge=${GOSX_RECEIPT_LH_BRIDGE:-8294}

if [[ -n "$(git -C "$repo_root" status --porcelain)" ]]; then
  echo "commit the source tree before generating receipts so the recorded SHA names the measured build" >&2
  exit 1
fi
if [[ ! -d "$tools_dir/node_modules/lighthouse" || ! -d "$tools_dir/node_modules/playwright" ]]; then
  echo "lighthouse and playwright must be installed under $tools_dir/node_modules" >&2
  exit 1
fi

host_ip=$(wcdp_host_ip)
bridge_pid=
gpu_pid=
shim_dir=$(mktemp -d)
cleanup() {
  wcdp_stop_pid "$gpu_pid"
  wcdp_stop_pid "$bridge_pid"
  wcdp_remove_profile gpu
  rm -rf "$shim_dir"
}
trap cleanup EXIT

wcdp_init
bridge_pid=$(wcdp_start_bridge "$gpu_bridge:$gpu_port,$lh_bridge:$lh_port")
echo "relay PID $bridge_pid on $host_ip ($gpu_bridge -> $gpu_port, $lh_bridge -> $lh_port)"

gpu_flags=$(mktemp "$shim_dir/gpu-flags.XXXXXX")
printf '%s\n' --use-angle=d3d11 --enable-unsafe-webgpu --ignore-gpu-blocklist \
  --disable-backgrounding-occluded-windows --disable-renderer-backgrounding \
  --disable-background-timer-throttling > "$gpu_flags"
gpu_pid=$(wcdp_start_chrome "$gpu_port" gpu "$gpu_flags")
echo "GPU Chrome PID $gpu_pid, profile $WCDP_DIR_WIN\\gpu"
wcdp_wait_port "http://$host_ip:$gpu_bridge"

# Adapter proof: refuse to measure on a software renderer or a non-NVIDIA GPU.
mkdir -p "$evidence_dir/gpu"
adapter_file="$evidence_dir/gpu/browser-adapter.json"
GPU_URL="http://$host_ip:$gpu_bridge" node --input-type=module - > "$adapter_file" <<'JS'
const base = process.env.GPU_URL;
const version = await (await fetch(`${base}/json/version`)).json();
const socket = new WebSocket(version.webSocketDebuggerUrl);
await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
const info = await new Promise((resolve, reject) => {
  socket.onmessage = event => { const message = JSON.parse(event.data); if (message.id === 1) resolve(message); };
  socket.send(JSON.stringify({ id: 1, method: 'SystemInfo.getInfo' }));
  setTimeout(() => reject(new Error('SystemInfo.getInfo timed out')), 15000);
});
socket.close();
const gpu = info.result.gpu;
const device = gpu.devices[0] || {};
const primary = gpu.devices.find(d => /nvidia/i.test(d.deviceString || '') || d.vendorId === 0x10de) || device;
const feature = info.result.gpu.featureStatus || {};
process.stdout.write(JSON.stringify({
  browser: version.Browser,
  vendorId: primary.vendorId,
  device: primary.deviceString,
  driverVersion: primary.driverVersion,
  devices: gpu.devices.map(d => d.deviceString),
  webgl: feature.webgl,
  webgpu: feature.webgpu,
  auxGlRenderer: gpu.auxAttributes && gpu.auxAttributes.glRenderer,
}, null, 2) + '\n');
JS
cat "$adapter_file"
if ! grep -i '"auxGlRenderer"' "$adapter_file" | grep -qi 'nvidia' || grep -qi 'swiftshader' "$adapter_file"; then
  echo "GPU Chrome is not on an NVIDIA adapter; refusing to measure" >&2
  exit 1
fi

# Tools directory with the Windows Chrome lh.sh; everything else is shared.
for entry in "$tools_dir"/* "$tools_dir"/.[!.]*; do
  [[ -e "$entry" ]] || continue
  [[ "$(basename "$entry")" == "lh.sh" ]] && continue
  ln -s "$entry" "$shim_dir/$(basename "$entry")"
done
ln -s "$script_dir/showcase-receipts-windows-lh.sh" "$shim_dir/lh.sh"

export GOSX_RECEIPT_LH_NODE_DIR="$tools_dir"
export GOSX_RECEIPT_LH_CHROME_PORT="$lh_port"
export GOSX_RECEIPT_LH_BRIDGE="$lh_bridge"
export GOSX_RECEIPT_LH_BRIDGE_PORT="$lh_bridge"
export GOSX_RECEIPT_LANE
export SHOWCASE_TOOLS_DIR="$shim_dir"
export SHOWCASE_EVIDENCE_DIR="$evidence_dir"
export CDP_URL="http://$host_ip:$gpu_bridge"

"$script_dir/showcase-receipts.sh"

# Name the adapter in the receipt. The gpu.browser field is a free-form string.
python3 - "$repo_root/examples/gosx-docs/app/performance/receipts.json" "$adapter_file" <<'PY'
import json, sys
path, adapter_path = sys.argv[1:]
adapter = json.load(open(adapter_path))
data = json.load(open(path))
if adapter["device"] not in data["gpu"]["browser"]:
    data["gpu"]["browser"] += " · " + adapter["device"] + ", driver " + str(adapter.get("driverVersion", "unknown")) + ", headless"
open(path, "w").write(json.dumps(data, indent=2, ensure_ascii=False) + "\n")
PY
echo "Recorded adapter: $(python3 -c "import json;print(json.load(open('$adapter_file'))['device'])")"
