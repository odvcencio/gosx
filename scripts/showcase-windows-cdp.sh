#!/usr/bin/env bash
# Shared helpers for driving headless Windows Chrome from WSL over DevTools.
# Source this file; do not run it. See showcase-receipts-windows.sh.
#
# WSL cannot reach a Windows loopback port, so a small TCP relay on the WSL
# gateway address forwards to Chrome's 127.0.0.1 DevTools port. Windows reaches
# WSL servers at http://localhost:<port>. Every process is stopped by PID, and
# every profile lives under C:\Temp so no UNC path reaches Chrome.

WCDP_POWERSHELL=${WCDP_POWERSHELL:-/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe}
WCDP_TASKKILL=${WCDP_TASKKILL:-/mnt/c/Windows/System32/taskkill.exe}
WCDP_CHROME=${WCDP_CHROME:-C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe}
WCDP_LANE=${GOSX_RECEIPT_LANE:-gosx-receipts}
WCDP_DIR_WSL=/mnt/c/Temp/$WCDP_LANE
WCDP_DIR_WIN="C:\\Temp\\$WCDP_LANE"

wcdp_host_ip() {
  ip route show default | awk '{print $3; exit}'
}

wcdp_init() {
  mkdir -p "$WCDP_DIR_WSL"
  cat > "$WCDP_DIR_WSL/start-chrome.ps1" <<'PS'
param([string]$ChromePath, [string]$ArgsFile)
$ErrorActionPreference = 'Stop'
$arguments = @(Get-Content -LiteralPath $ArgsFile | Where-Object { $_ })
$process = Start-Process -FilePath $ChromePath -ArgumentList $arguments -PassThru
$process.Id
PS
  cat > "$WCDP_DIR_WSL/start-bridge.ps1" <<'PS'
param([string]$ListenIp, [string]$Pairs)
$ErrorActionPreference = 'Stop'
Add-Type -TypeDefinition @'
using System;
using System.Net;
using System.Net.Sockets;
using System.Threading;
using System.Threading.Tasks;
public static class GosxCdpBridge {
    public static void Start(string ip, int listenPort, int targetPort) {
        var listener = new TcpListener(IPAddress.Parse(ip), listenPort);
        listener.Start();
        var thread = new Thread(() => {
            while (true) {
                TcpClient client = listener.AcceptTcpClient();
                Task.Run(() => Handle(client, targetPort));
            }
        });
        thread.IsBackground = false;
        thread.Start();
    }
    static async Task Handle(TcpClient client, int targetPort) {
        using (client)
        using (var upstream = new TcpClient()) {
            try {
                await upstream.ConnectAsync(IPAddress.Loopback, targetPort);
                var a = client.GetStream(); var b = upstream.GetStream();
                await Task.WhenAny(a.CopyToAsync(b), b.CopyToAsync(a));
            } catch (Exception) { }
        }
    }
}
'@
foreach ($pair in $Pairs.Split(',')) {
  $ports = $pair.Split(':')
  [GosxCdpBridge]::Start($ListenIp, [int]$ports[0], [int]$ports[1])
}
Start-Sleep -Seconds 86400
PS
}

wcdp_win_path() { wslpath -w "$1"; }

# wcdp_start_bridge "listen:target,listen:target" -> prints the Windows PID
wcdp_start_bridge() {
  local pairs=$1 ip
  ip=$(wcdp_host_ip)
  "$WCDP_POWERSHELL" -NoProfile -ExecutionPolicy Bypass -Command \
    "(Start-Process powershell.exe -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','$WCDP_DIR_WIN\\start-bridge.ps1','-ListenIp','$ip','-Pairs','$pairs' -WindowStyle Hidden -PassThru).Id" | tr -d '\r'
}

# wcdp_start_chrome <port> <profile-name> <flags-file-wsl> -> prints the Windows PID
# The flags file holds one Chrome flag per line. This function adds the
# headless, debugging-port, and profile flags.
wcdp_start_chrome() {
  local port=$1 profile=$2 flags=$3 argsfile="$WCDP_DIR_WSL/args-$2.txt"
  rm -rf "$WCDP_DIR_WSL/$profile" || return 1
  {
    echo "--headless=new"
    echo "--mute-audio"
    echo "--remote-debugging-port=$port"
    echo "--user-data-dir=$WCDP_DIR_WIN\\$profile"
    echo "--no-first-run"
    echo "--no-default-browser-check"
    cat "$flags"
    echo "about:blank"
  } > "$argsfile" || return 1
  "$WCDP_POWERSHELL" -NoProfile -ExecutionPolicy Bypass -File "$WCDP_DIR_WIN\\start-chrome.ps1" \
    -ChromePath "$WCDP_CHROME" -ArgsFile "$WCDP_DIR_WIN\\args-$profile.txt" | tr -d '\r' | tail -1
}

wcdp_stop_pid() {
  [[ -n "${1:-}" ]] || return 0
  "$WCDP_TASKKILL" /PID "$1" /T /F >/dev/null 2>&1 || true
}

# Measurement loops need proof that the old browser exited before starting
# another cold profile. The best-effort helper above remains suitable for traps.
wcdp_stop_pid_checked() {
  [[ "${1:-}" =~ ^[1-9][0-9]*$ ]] || return 1
  "$WCDP_TASKKILL" /PID "$1" /T /F >/dev/null 2>&1 || true
  "$WCDP_POWERSHELL" -NoProfile -Command \
    "if (Get-Process -Id $1 -ErrorAction SilentlyContinue) { exit 1 }; exit 0" >/dev/null
}

wcdp_remove_profile() {
  # Windows may hold profile files for a moment after taskkill.
  local i
  for i in 1 2 3 4 5 6 7 8; do
    rm -rf "$WCDP_DIR_WSL/$1" 2>/dev/null
    [[ -e "$WCDP_DIR_WSL/$1" ]] || return 0
    sleep 1
  done
}

wcdp_wait_port() {
  local url=$1 i
  for i in $(seq 1 60); do
    if curl -fsS --max-time 2 "$url/json/version" >/dev/null 2>&1; then return 0; fi
    sleep 0.5
  done
  echo "DevTools did not answer at $url" >&2
  return 1
}
