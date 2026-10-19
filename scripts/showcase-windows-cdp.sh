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
. (Join-Path $PSScriptRoot 'stop-owned-process.ps1')
$arguments = @(Get-Content -LiteralPath $ArgsFile | Where-Object { $_ })
$process = Start-Process -FilePath $ChromePath -ArgumentList $arguments -PassThru
try {
  $null = $process.Handle
  $native = Get-CimInstance Win32_Process -Filter "ProcessId=$($process.Id)"
  if (!$native -or !$native.CommandLine) { throw 'Cannot record owned Chrome identity' }
  @{ id=$process.Id; started=$process.StartTime.ToUniversalTime().Ticks.ToString(); executable=$process.Path; command=$native.CommandLine } |
    ConvertTo-Json -Compress | Set-Content -Encoding UTF8 -LiteralPath (Join-Path $PSScriptRoot "process-$($process.Id).json")
  $process.Id
} catch {
  $failure = $_
  try {
    if (!$process.HasExited) {
      $killed = Invoke-WcdpTaskkill $process.Id
      [Console]::Error.WriteLine("unreported Chrome PID $($process.Id): taskkill exit $($killed.code): $($killed.message)")
    }
    if (!$process.WaitForExit(5000)) { throw 'unreported Chrome did not exit within 5000 ms' }
  } catch {
    [Console]::Error.WriteLine("unreported Chrome PID $($process.Id): cleanup failed: $_")
  }
  throw $failure
} finally {
  $process.Dispose()
}
PS
  cat > "$WCDP_DIR_WSL/stop-owned-process.ps1" <<'PS'
param([int]$ProcessId, [int]$WaitMilliseconds=5000)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Invoke-WcdpTaskkill([int]$TargetId) {
  # Explicit pipes and ExitCode avoid relying on WSL-inherited console handles
  # or PowerShell's native-command output/exit-status propagation.
  $info = New-Object Diagnostics.ProcessStartInfo
  $info.FileName = Join-Path ([Environment]::GetFolderPath('System')) 'taskkill.exe'
  $info.Arguments = "/PID $TargetId /T /F"
  $info.UseShellExecute = $false
  $info.CreateNoWindow = $true
  $info.RedirectStandardOutput = $true
  $info.RedirectStandardError = $true
  $command = New-Object Diagnostics.Process
  $command.StartInfo = $info
  try {
    $null = $command.Start()
    $stdout = $command.StandardOutput.ReadToEndAsync()
    $stderr = $command.StandardError.ReadToEndAsync()
    if (!$command.WaitForExit(3000)) {
      $command.Kill()
      return @{ code=-1; message='taskkill itself exceeded 3000 ms' }
    }
    return @{ code=$command.ExitCode; message=($stdout.Result + $stderr.Result).Trim() }
  } finally { $command.Dispose() }
}

function Stop-WcdpOwnedProcess([int]$TargetId, [int]$WaitMilliseconds) {
  # Query failure is inconclusive, never evidence of absence. Keep a handle to
  # the recorded process so waiting cannot accidentally follow a reused PID.
  $native = Get-CimInstance Win32_Process -Filter "ProcessId=$TargetId" -ErrorAction Stop
  if (!$native) { return @{ code=0; message='already absent' } }
  $ownerPath = Join-Path $PSScriptRoot "process-$TargetId.json"
  if (!(Test-Path -LiteralPath $ownerPath)) {
    return @{ code=21; message='no ownership record; refusing termination' }
  }
  $owner = Get-Content -Raw -LiteralPath $ownerPath | ConvertFrom-Json
  $process = Get-Process -Id $TargetId -ErrorAction Stop
  try {
    $null = $process.Handle
    if ($owner.id -ne $TargetId -or
        $owner.started -ne $process.StartTime.ToUniversalTime().Ticks.ToString() -or
        $owner.executable -ine $process.Path -or $owner.command -cne $native.CommandLine -or
        $native.CommandLine.IndexOf(($PSScriptRoot.TrimEnd('\') + '\'), [StringComparison]::OrdinalIgnoreCase) -lt 0) {
      return @{ code=21; message='process identity or lane mismatch; refusing termination' }
    }
    $killed = Invoke-WcdpTaskkill $TargetId
    if (!$process.WaitForExit($WaitMilliseconds)) {
      if ($killed.code -ne 0) {
        return @{ code=23; message="taskkill failed ($($killed.code)) and exit is unproven: $($killed.message)" }
      }
      return @{ code=20; message="process did not exit within $WaitMilliseconds ms" }
    }
    return @{ code=0; message="process exit verified; taskkill exit $($killed.code): $($killed.message)" }
  } finally {
    $process.Dispose()
  }
}

if ($MyInvocation.InvocationName -ne '.') {
  try {
    $result = Stop-WcdpOwnedProcess $ProcessId $WaitMilliseconds
  } catch {
    $result = @{ code=22; message="process verification failed: $_" }
  }
  [Console]::Error.WriteLine("wcdp cleanup PID ${ProcessId}: $($result.message)")
  Write-Output "WCDP_STOP_RESULT=$($result.code)"
  exit $result.code
}
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
  if [[ -f "$WCDP_DIR_WSL/process-$1.json" ]]; then
    # EXIT traps must not bypass a checked cleanup's ownership rejection.
    wcdp_stop_pid_checked "$1" || true
    return
  fi
  "$WCDP_TASKKILL" /PID "$1" /T /F >/dev/null 2>&1 || true
}

# Measurement loops need proof that the old browser exited before starting
# another cold profile. The best-effort helper above remains suitable for traps.
wcdp_stop_pid_checked() {
  [[ "${1:-}" =~ ^[1-9][0-9]*$ ]] || return 1
  local attempt output status result
  for attempt in 1 2 3; do
    if output=$(timeout -k 1 12 "$WCDP_POWERSHELL" -NoProfile -ExecutionPolicy Bypass \
      -File "$WCDP_DIR_WIN\\stop-owned-process.ps1" -ProcessId "$1" 2>&1); then
      status=0
    else
      status=$?
    fi
    result=$(printf '%s\n' "$output" | tr -d '\r' | tail -1)
    printf 'wcdp cleanup PID %s attempt %s exit %s: %s\n' "$1" "$attempt" "$status" "$output" >&2
    case "$result" in
      WCDP_STOP_RESULT=0) [[ "$status" == 0 ]] && return 0 ;;
      WCDP_STOP_RESULT=20|WCDP_STOP_RESULT=21|WCDP_STOP_RESULT=22|WCDP_STOP_RESULT=23) return 1 ;;
    esac
    # No valid completion means a failed/uncertain WSL native transport. Retry
    # with a fresh invocation; it revalidates ownership before any repeat kill.
    [[ "$attempt" == 3 ]] || sleep 1
  done
  return 1
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
