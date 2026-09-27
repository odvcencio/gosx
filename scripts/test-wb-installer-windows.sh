#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
powerShell='/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe'
windows_root='/mnt/c/Temp/wb-rel-installer'
evidence_root='/home/draco/.local/state/nightwatch/reports/wb-release/installer'
nuget_url='https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/1.0.4191.47/microsoft.web.webview2.1.0.4191.47.nupkg'
nuget_sha='f492bbf547d0da329553b6727435b677579b1e9f91cc9e4a1ad029366d5f23d0'
loader_sha='c66e4a92fdc7a216118e43b7a5024ea2200e8c43f9310bf20d96a0084f82c5bc'
bootstrapper_url='https://go.microsoft.com/fwlink/p/?LinkId=2124703'

if [[ ! -x "$powerShell" ]] || ! command -v wslpath >/dev/null 2>&1; then
  echo 'ERROR: Windows installer smoke requires WSL interop and Windows PowerShell at /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe.' >&2
  exit 1
fi
for tool in curl sha256sum unzip python3; do
  command -v "$tool" >/dev/null 2>&1 || { echo "ERROR: missing WSL tool $tool." >&2; exit 1; }
done

run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
run_dir="$evidence_root/installer-$run_id"
build_dir="/tmp/wb-release-installer-pr1-$run_id"
remove_windows_root() {
  python3 - "$windows_root" <<'PY'
import shutil
import sys
import time

path = sys.argv[1]
for attempt in range(10):
    try:
        shutil.rmtree(path, ignore_errors=False)
        break
    except FileNotFoundError:
        break
    except OSError:
        if attempt == 9:
            raise
        time.sleep(0.5)
PY
}
mkdir -p "$run_dir" "$build_dir" "$windows_root"
if [[ -e "$windows_root/.wb-rel-installer-test" ]]; then
  remove_windows_root
  mkdir -p "$windows_root"
elif [[ "$(find "$windows_root" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]]; then
  echo 'ERROR: C:\Temp\wb-rel-installer exists without this test lane marker; refusing to remove it.' >&2
  exit 1
fi
touch "$windows_root/.wb-rel-installer-test"
cleanup() {
  if [[ -f "$windows_root/.wb-rel-installer-test" ]]; then
    remove_windows_root || true
  fi
  rm -rf "$build_dir"
}
trap cleanup EXIT

nuget="$build_dir/microsoft.web.webview2.1.0.4191.47.nupkg"
curl --fail --location --connect-timeout 20 --max-time 180 --retry 4 --retry-all-errors --retry-delay 60 \
  --output "$nuget" "$nuget_url"
actual_nuget_sha="$(sha256sum "$nuget" | awk '{print $1}')"
if [[ "$actual_nuget_sha" != "$nuget_sha" ]]; then
  echo "ERROR: WebView2 SDK package SHA-256 $actual_nuget_sha did not match $nuget_sha." >&2
  exit 1
fi
loader="$build_dir/WebView2Loader.dll"
unzip -p "$nuget" build/native/x64/WebView2Loader.dll > "$loader"
actual_loader_sha="$(sha256sum "$loader" | awk '{print $1}')"
if [[ "$actual_loader_sha" != "$loader_sha" ]]; then
  echo "ERROR: WebView2Loader.dll SHA-256 $actual_loader_sha did not match $loader_sha." >&2
  exit 1
fi

bootstrapper="$build_dir/MicrosoftEdgeWebView2Setup.exe"
curl --fail --location --connect-timeout 20 --max-time 180 --retry 4 --retry-all-errors --retry-delay 60 \
  --output "$bootstrapper" "$bootstrapper_url"
bootstrapper_sha="$(sha256sum "$bootstrapper" | awk '{print $1}')"

stage_v1="$build_dir/stage-v1"
stage_v2="$build_dir/stage-v2"
mkdir -p "$stage_v1" "$stage_v2"
for version in 1.0.0 1.0.1; do
  if [[ "$version" == 1.0.0 ]]; then stage="$stage_v1"; else stage="$stage_v2"; fi
  GOWORK=off GOOS=windows GOARCH=amd64 CGO_ENABLED=0 nice -n 10 go build -trimpath \
    -ldflags="-H=windowsgui -X main.appVersion=$version" \
    -o "$stage/wb-smoke.exe" ./test/desktop/installer-smoke
  cp "$loader" "$stage/WebView2Loader.dll"
done

cat > "$build_dir/config-v1.json" <<EOF
{
  "app_id": "dev.gosx.wbinstaller.smoke",
  "name": "WELDBREAKERS Smoke",
  "publisher": "WELDBREAKERS Test",
  "version": "1.0.0",
  "icon": "wb-smoke.exe",
  "host_exe": "wb-smoke.exe",
  "webview2_bootstrapper": "$bootstrapper",
  "webview2_sha256": "$bootstrapper_sha",
  "update_public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
  "data_dir": "C:\\\\Temp\\\\wb-rel-installer\\\\player-data",
  "channel": "stable",
  "released": "2026-09-27",
  "notes": "Windows installer smoke fixture.",
  "download_page": "https://example.invalid/wb-release"
}
EOF
sed 's/"version": "1.0.0"/"version": "1.0.1"/' "$build_dir/config-v1.json" > "$build_dir/config-v2.json"

out_v1="$build_dir/out-v1"
out_v2="$build_dir/out-v2"
GOWORK=off nice -n 10 go run ./cmd/gosx desktop package --input "$stage_v1" --config "$build_dir/config-v1.json" --output "$out_v1"
GOWORK=off nice -n 10 go run ./cmd/gosx desktop package --input "$stage_v2" --config "$build_dir/config-v2.json" --output "$out_v2"
setup_v1="$out_v1/WELDBREAKERS-Smoke-Setup-1.0.0.exe"
setup_v2="$out_v2/WELDBREAKERS-Smoke-Setup-1.0.1.exe"
test -s "$setup_v1" && test -s "$setup_v2"
test -s "$out_v1/WELDBREAKERS-Smoke-1.0.0-portable.zip"
test -s "$out_v1/latest.json" && test -s "$out_v1/SHA256SUMS"

cp "$setup_v1" "$windows_root/setup-v1.exe"
cp "$setup_v2" "$windows_root/setup-v2.exe"
cp "$repo_root/test/desktop/windows-installer-smoke.ps1" "$windows_root/windows-installer-smoke.ps1"
windows_log="$run_dir/windows.log"
set +e
(cd /mnt/c/Temp && "$powerShell" -NoProfile -ExecutionPolicy Bypass \
  -File 'C:\Temp\wb-rel-installer\windows-installer-smoke.ps1' \
  -SetupV1 'C:\Temp\wb-rel-installer\setup-v1.exe' \
  -SetupV2 'C:\Temp\wb-rel-installer\setup-v2.exe') > "$windows_log" 2>&1
windows_status=$?
set -e

cat > "$run_dir/run.txt" <<EOF
run_id=$run_id
worktree=$repo_root
head=$(git -C "$repo_root" rev-parse HEAD)
windows_powershell=$powerShell
windows_cwd=C:\Temp
windows_exit=$windows_status
nuget_sha256=$actual_nuget_sha
webview2_loader_sha256=$actual_loader_sha
microsoft_bootstrapper_url=$bootstrapper_url
microsoft_bootstrapper_sha256=$bootstrapper_sha
setup_v1_sha256=$(sha256sum "$setup_v1" | awk '{print $1}')
setup_v2_sha256=$(sha256sum "$setup_v2" | awk '{print $1}')
test_timeout_seconds=180
EOF

if [[ "$windows_status" -ne 0 ]]; then
  echo "ERROR: Windows installer smoke failed. Evidence: $run_dir" >&2
  cat "$windows_log" >&2
  exit "$windows_status"
fi
rg -q 'PASS: installer matrix completed in ' "$windows_log"
remove_windows_root
if [[ -e "$windows_root" ]]; then
  echo "ERROR: C:\Temp\wb-rel-installer still exists after cleanup." >&2
  exit 1
fi
echo "PASS: Windows installer test area removed, including the test app data and binaries."
echo "WINDOWS INSTALLER SMOKE PASS: evidence $run_dir"
