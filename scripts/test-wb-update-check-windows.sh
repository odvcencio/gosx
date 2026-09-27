#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
powerShell='/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe'
windows_root='/mnt/c/Temp/wb-rel-installer'
evidence_root='/home/draco/.local/state/nightwatch/reports/wb-release/installer'

if [[ ! -x "$powerShell" ]] || ! command -v wslpath >/dev/null 2>&1; then
  echo 'ERROR: Windows update-check smoke requires WSL interop and Windows PowerShell at /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe.' >&2
  exit 1
fi
for tool in curl python3 sha256sum rg; do
  command -v "$tool" >/dev/null 2>&1 || { echo "ERROR: missing WSL tool $tool." >&2; exit 1; }
done

run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
run_dir="$evidence_root/update-$run_id"
build_dir="/tmp/wb-release-update-$run_id"
fixture_pid=''
windows_status='not-run'

check_port_free() {
  python3 - "$1" <<'PY'
import socket
import sys

port = int(sys.argv[1])
probe = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
try:
    probe.bind(("0.0.0.0", port))
except OSError as error:
    raise SystemExit(f"ERROR: 127.0.0.1:{port} is not free: {error}")
finally:
    probe.close()
print(f"PASS: 127.0.0.1:{port} was free before the Windows update test.")
PY
}

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

mkdir -p "$run_dir" "$build_dir"
mkdir -p "$windows_root"
if [[ -e "$windows_root/.wb-rel-installer-test" ]]; then
  remove_windows_root
  mkdir -p "$windows_root"
elif [[ "$(find "$windows_root" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]]; then
  echo 'ERROR: C:\Temp\wb-rel-installer exists without this test lane marker; refusing to remove it.' >&2
  exit 1
fi
touch "$windows_root/.wb-rel-installer-test"

cleanup() {
  if [[ -n "$fixture_pid" ]]; then
    kill "$fixture_pid" 2>/dev/null || true
    wait "$fixture_pid" 2>/dev/null || true
  fi
  if [[ -f "$windows_root/.wb-rel-installer-test" ]]; then
    remove_windows_root || true
  fi
  rm -rf "$build_dir"
}
trap cleanup EXIT

head_sha="$(git -C "$repo_root" rev-parse HEAD)"
check_port_free 8210 > "$run_dir/port-8210.txt"
check_port_free 8211 > "$run_dir/port-8211.txt"

GOWORK=off nice -n 10 go build -trimpath \
  -o "$build_dir/update-check-fixture" ./test/desktop/update-check-fixture
GOWORK=off GOOS=windows GOARCH=amd64 CGO_ENABLED=0 nice -n 10 go build -trimpath \
  -o "$windows_root/update-check-smoke.exe" ./test/desktop/update-check-smoke
cp "$repo_root/test/desktop/windows-update-check-smoke.ps1" "$windows_root/windows-update-check-smoke.ps1"

public_key_file="$build_dir/public-key.txt"
manifest_file="$run_dir/latest.json"
signature_file="$run_dir/latest.json.sig"
"$build_dir/update-check-fixture" \
  --listen 0.0.0.0:8210 \
  --public-key-file "$public_key_file" \
  --manifest-file "$manifest_file" \
  --signature-file "$signature_file" \
  > "$run_dir/server.log" 2>&1 &
fixture_pid=$!

ready=0
for attempt in $(seq 1 100); do
  if curl --silent --fail --output /dev/null --max-time 1 http://127.0.0.1:8210/health; then
    ready=1
    break
  fi
  if ! kill -0 "$fixture_pid" 2>/dev/null; then
    cat "$run_dir/server.log" >&2
    echo 'ERROR: update fixture server exited before listening.' >&2
    exit 1
  fi
  sleep 0.1
done
if [[ "$ready" -ne 1 ]]; then
  echo 'ERROR: update fixture server did not start on 127.0.0.1:8210.' >&2
  cat "$run_dir/server.log" >&2
  exit 1
fi

public_key="$(tr -d '\r\n' < "$public_key_file")"
windows_log="$run_dir/windows.log"
set +e
(cd /mnt/c/Temp && "$powerShell" -NoProfile -ExecutionPolicy Bypass \
  -File 'C:\Temp\wb-rel-installer\windows-update-check-smoke.ps1' \
  -Client 'C:\Temp\wb-rel-installer\update-check-smoke.exe' \
  -PublicKey "$public_key") > "$windows_log" 2>&1
windows_status=$?
set -e

sleep 0.2
cat > "$run_dir/run.txt" <<EOF
run_id=$run_id
worktree=$repo_root
head=$head_sha
windows_powershell=$powerShell
windows_cwd=C:\Temp
windows_exit=$windows_status
update_fixture=http://127.0.0.1:8210
fixture_bind=0.0.0.0:8210 (WSL Windows localhost forwarding; client connects to 127.0.0.1)
offline_probe=http://127.0.0.1:8211 (free and not bound)
public_key_sha256=$(sha256sum "$public_key_file" | awk '{print $1}')
manifest_sha256=$(sha256sum "$manifest_file" | awk '{print $1}')
signature_sha256=$(sha256sum "$signature_file" | awk '{print $1}')
windows_client_sha256=$(sha256sum "$windows_root/update-check-smoke.exe" | awk '{print $1}')
windows_script_safety_seconds=170
EOF

if [[ "$windows_status" -ne 0 ]]; then
  echo "ERROR: Windows update-check smoke failed. Evidence: $run_dir" >&2
  cat "$windows_log" >&2
  cat "$run_dir/server.log" >&2
  exit "$windows_status"
fi
rg -q 'PASS: newer version 2.0.0' "$windows_log"
rg -q 'PASS: second check on the same state file skipped within 24 hours' "$windows_log"
rg -q 'PASS: same version reported up-to-date' "$windows_log"
rg -q 'PASS: invalid detached signature rejected' "$windows_log"
rg -q 'PASS: offline state skipped without a request' "$windows_log"
rg -q 'PASS: no test-only HKCU Uninstall keys were created' "$windows_log"
rg -q 'PASS: Windows update-check matrix completed in ' "$windows_log"
[[ "$(rg -c 'GET /new/latest.json$' "$run_dir/server.log")" -eq 1 ]]
[[ "$(rg -c 'GET /new/latest.json.sig$' "$run_dir/server.log")" -eq 1 ]]
! rg -q 'offline' "$run_dir/server.log"

kill "$fixture_pid"
wait "$fixture_pid" 2>/dev/null || true
fixture_pid=''
check_port_free 8210 >> "$run_dir/port-8210.txt"
check_port_free 8211 >> "$run_dir/port-8211.txt"

remove_windows_root
if [[ -e "$windows_root" ]]; then
  echo "ERROR: C:\Temp\wb-rel-installer still exists after update-check cleanup." >&2
  exit 1
fi
cat >> "$run_dir/run.txt" <<EOF
windows_test_root_removed=true
test_uninstall_keys_absent=true (asserted before and after test)
EOF
cat "$windows_log"
echo 'PASS: Windows update client, state files, and fixture executable removed from C:\Temp\wb-rel-installer.'
echo "WINDOWS UPDATE CHECK SMOKE PASS: evidence $run_dir"
