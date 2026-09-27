#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
powerShell='/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe'
stage_wsl='/mnt/c/Temp/wb-rel-gosx-desktop'
evidence_root='/home/draco/.local/state/nightwatch/reports/wb-release/gosx-desktop'
nuget_url='https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/1.0.4191.47/microsoft.web.webview2.1.0.4191.47.nupkg'
nuget_sha='f492bbf547d0da329553b6727435b677579b1e9f91cc9e4a1ad029366d5f23d0'
loader_sha='c66e4a92fdc7a216118e43b7a5024ea2200e8c43f9310bf20d96a0084f82c5bc'
expect_failure=0
shipping_features=0

for arg in "$@"; do
  case "$arg" in
    --expect-lifetime-failure) expect_failure=1 ;;
    --shipping-features) shipping_features=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

if [[ ! -x "$powerShell" ]] || ! command -v wslpath >/dev/null 2>&1; then
  echo 'SKIP: Windows desktop smoke requires WSL interop and Windows PowerShell at /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe.'
  exit 0
fi
if ! (cd /mnt/c && "$powerShell" -NoProfile -NonInteractive -Command '$PSVersionTable.PSVersion.ToString()' >/dev/null 2>&1); then
  echo 'SKIP: WSL interop is unavailable; Windows PowerShell could not be started from /mnt/c.'
  exit 0
fi

for tool in curl sha256sum unzip python3; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: Windows desktop smoke requires $tool in WSL." >&2
    exit 1
  fi
done

run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
run_dir="$evidence_root/$run_id"
deps_dir="$evidence_root/deps"
mkdir -p "$run_dir" "$deps_dir" "$stage_wsl"
server_pid=''
cleanup_server() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
}
trap cleanup_server EXIT

nupkg="$deps_dir/microsoft.web.webview2.1.0.4191.47.nupkg"
if [[ ! -f "$nupkg" ]]; then
  curl --fail --location --connect-timeout 20 --max-time 120 --retry 4 --retry-all-errors --retry-delay 60 \
    --output "$nupkg" "$nuget_url"
fi
actual_nupkg_sha="$(sha256sum "$nupkg" | awk '{print $1}')"
if [[ "$actual_nupkg_sha" != "$nuget_sha" ]]; then
  echo "ERROR: WebView2 NuGet package SHA-256 $actual_nupkg_sha did not match $nuget_sha." >&2
  exit 1
fi
loader="$run_dir/WebView2Loader.dll"
unzip -p "$nupkg" build/native/x64/WebView2Loader.dll > "$loader"
actual_loader_sha="$(sha256sum "$loader" | awk '{print $1}')"
if [[ "$actual_loader_sha" != "$loader_sha" ]]; then
  echo "ERROR: WebView2Loader.dll SHA-256 $actual_loader_sha did not match $loader_sha." >&2
  exit 1
fi

binary="$run_dir/gosx.exe"
if [[ -n "${GOSX_DESKTOP_SMOKE_BINARY:-}" ]]; then
  if [[ ! -f "$GOSX_DESKTOP_SMOKE_BINARY" ]]; then
    echo "ERROR: requested smoke binary does not exist: $GOSX_DESKTOP_SMOKE_BINARY" >&2
    exit 1
  fi
  cp "$GOSX_DESKTOP_SMOKE_BINARY" "$binary"
else
  (cd "$repo_root" && GOWORK=off GOOS=windows GOARCH=amd64 nice -n 10 go build -o "$binary" ./cmd/gosx)
fi

cp "$binary" "$stage_wsl/gosx.exe"
cp "$loader" "$stage_wsl/WebView2Loader.dll"
cp "$repo_root/test/desktop/windows-smoke.ps1" "$stage_wsl/windows-smoke.ps1"

report_file="$run_dir/report.json"
server_log="$run_dir/server.log"
python3 "$repo_root/test/desktop/windows-smoke-server.py" --port 8175 --output "$report_file" > "$server_log" 2>&1 &
server_pid=$!
ready=0
for _ in $(seq 1 50); do
  if curl --silent --fail http://127.0.0.1:8175/health >/dev/null; then ready=1; break; fi
  if ! kill -0 "$server_pid" 2>/dev/null; then break; fi
  sleep 0.1
done
if [[ "$ready" -ne 1 ]]; then
  echo 'ERROR: smoke page server did not start on port 8175.' >&2
  cat "$server_log" >&2
  exit 1
fi

windows_log="$run_dir/windows.log"
ps_args=(-NoProfile -NonInteractive -ExecutionPolicy Bypass -File 'C:\Temp\wb-rel-gosx-desktop\windows-smoke.ps1' -RunId "$run_id")
if [[ "$expect_failure" -eq 1 ]]; then ps_args+=(-ExpectLifetimeFailure); fi
if [[ "$shipping_features" -eq 1 ]]; then ps_args+=(-UseOptionsArguments -CheckShippingFeatures); fi

set +e
(cd /mnt/c && "$powerShell" "${ps_args[@]}") > "$windows_log" 2>&1
ps_status=$?
set -e

cp "$stage_wsl/host-$run_id.stdout.txt" "$run_dir/host.stdout.txt" 2>/dev/null || true
cp "$stage_wsl/host-$run_id.stderr.txt" "$run_dir/host.stderr.txt" 2>/dev/null || true
cp "$stage_wsl/window-$run_id.png" "$run_dir/window.png" 2>/dev/null || true
if [[ "$shipping_features" -eq 1 ]]; then
  cp "$stage_wsl/runtime-version-$run_id.stdout.txt" "$run_dir/runtime-version.stdout.txt" 2>/dev/null || true
  cp "$stage_wsl/runtime-version-$run_id.stderr.txt" "$run_dir/runtime-version.stderr.txt" 2>/dev/null || true
  cp -R "$stage_wsl/probe-missing-loader-$run_id" "$run_dir/probe-missing-loader" 2>/dev/null || true
  cp -R "$stage_wsl/probe-missing-runtime-$run_id" "$run_dir/probe-missing-runtime" 2>/dev/null || true
fi

cat > "$run_dir/run.txt" <<EOF
run_id=$run_id
worktree=$repo_root
head=$(git -C "$repo_root" rev-parse HEAD)
binary_source=${GOSX_DESKTOP_SMOKE_BINARY:-worktree cross-build}
nuget_sha256=$actual_nupkg_sha
loader_sha256=$actual_loader_sha
windows_powershell=$powerShell
smoke_exit=$ps_status
expected_lifetime_failure=$expect_failure
shipping_feature_assertions=$shipping_features
EOF

if [[ "$expect_failure" -eq 1 ]]; then
  if [[ "$ps_status" -ne 0 ]] || ! rg -q 'EXPECTED_BLOCKER_CAUGHT: origin/main failed the profile lifetime check' "$windows_log"; then
    echo "ERROR: origin/main did not produce the expected lifetime failure; see $windows_log" >&2
    cat "$windows_log" >&2
    exit 1
  fi
  echo "EXPECTED BASELINE FAILURE CAUGHT: profile processes exited before 15 seconds; evidence $run_dir"
  exit 0
fi

if [[ "$ps_status" -ne 0 ]]; then
  echo "ERROR: Windows smoke failed; see $windows_log" >&2
  cat "$windows_log" >&2
  exit "$ps_status"
fi

python3 - "$report_file" "$shipping_features" <<'PY'
import json
import sys

path = sys.argv[1]
shipping = sys.argv[2] == "1"
with open(path, encoding="utf-8") as source:
    state = json.load(source)
report = state.get("report") or {}
checks = {
    "WebGL2": report.get("webgl2") is True,
    "chrome.webview": report.get("chromeWebview") is True,
    "JS -> Go -> JS bridge": report.get("bridgeRoundTrip") is True,
    "single page load": state.get("probe_gets") == 1,
}
if shipping:
    checks["HTML fullscreen enter"] = report.get("fullscreenEntered") is True
    checks["HTML fullscreen exit"] = report.get("fullscreenExited") is True
for label, passed in checks.items():
    print(f"{'PASS' if passed else 'FAIL'}: {label}")
if not all(checks.values()):
    raise SystemExit(1)
PY

if ! rg -q 'CLOSE_PASS host_exit_code=0 host_process_gone=true no_profile_webview2_processes=true' "$windows_log"; then
  echo "ERROR: Windows host teardown assertion is missing; see $windows_log" >&2
  cat "$windows_log" >&2
  exit 1
fi

echo "WINDOWS DESKTOP SMOKE PASS: evidence $run_dir"
