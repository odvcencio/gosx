param(
  [Parameter(Mandatory = $true)][string]$RunId,
  [switch]$ExpectLifetimeFailure,
  [switch]$UseOptionsArguments,
  [switch]$CheckShippingFeatures
)

$ErrorActionPreference = 'Stop'
$stage = 'C:\Temp\wb-rel-gosx-desktop'
$exe = Join-Path $stage 'gosx.exe'
$profile = Join-Path $stage ("profile-" + $RunId)
$stdout = Join-Path $stage ("host-" + $RunId + '.stdout.txt')
$stderr = Join-Path $stage ("host-" + $RunId + '.stderr.txt')
$capture = Join-Path $stage ("window-" + $RunId + '.png')
$title = 'wb-rel-gosx-smoke'
$url = 'http://127.0.0.1:8175/probe?run=' + [uri]::EscapeDataString($RunId)
$failures = New-Object 'System.Collections.Generic.List[string]'
$process = $null
$processHandle = [IntPtr]::Zero
$lifetimeFailure = $false
$cleanupPassed = $false
$hostExited = $false
$hostExitCode = $null
$profileProcessesClear = $false
$resultsRequest = 0
$startedAt = Get-Date

Set-Location $stage

Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Text;
using System.Runtime.InteropServices;
public static class WbDesktopSmokeNative {
  public delegate bool EnumWindowsCallback(IntPtr h, IntPtr l);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int command);
  [DllImport("user32.dll")] public static extern IntPtr WindowFromPoint(POINT p);
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsCallback callback, IntPtr l);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint processId);
  [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int GetClassName(IntPtr h, StringBuilder className, int size);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
  [DllImport("user32.dll")] public static extern bool PostMessageW(IntPtr h, uint msg, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool BringWindowToTop(IntPtr h);
  [DllImport("user32.dll")] public static extern IntPtr SetActiveWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern IntPtr SetFocus(IntPtr h);
  [DllImport("user32.dll")] public static extern bool AttachThreadInput(uint idAttach, uint idAttachTo, bool attach);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint flags, uint dx, uint dy, uint data, UIntPtr extra);
  [DllImport("user32.dll")] public static extern void keybd_event(byte key, byte scan, uint flags, UIntPtr extra);
  [DllImport("user32.dll")] public static extern IntPtr SendMessageW(IntPtr h, uint msg, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool DestroyIcon(IntPtr icon);
  [DllImport("shell32.dll", CharSet = CharSet.Unicode)] public static extern uint ExtractIconExW(string file, int index, out IntPtr large, out IntPtr small, uint count);
  [DllImport("kernel32.dll", SetLastError = true)] public static extern bool GetExitCodeProcess(IntPtr process, out uint code);
  [DllImport("kernel32.dll")] public static extern uint GetCurrentThreadId();
  public static bool ActivateWindow(IntPtr h) {
    uint targetProcessId;
    uint targetThread = GetWindowThreadProcessId(h, out targetProcessId);
    IntPtr foreground = GetForegroundWindow();
    uint foregroundProcessId;
    uint foregroundThread = GetWindowThreadProcessId(foreground, out foregroundProcessId);
    uint currentThread = GetCurrentThreadId();
    bool attachedForeground = false;
    bool attachedTarget = false;
    if (foregroundThread != 0 && foregroundThread != currentThread) {
      attachedForeground = AttachThreadInput(currentThread, foregroundThread, true);
    }
    if (targetThread != 0 && targetThread != currentThread && targetThread != foregroundThread) {
      attachedTarget = AttachThreadInput(currentThread, targetThread, true);
    }
    try {
      ShowWindow(h, 9);
      BringWindowToTop(h);
      SetForegroundWindow(h);
      SetActiveWindow(h);
      SetFocus(h);
      return GetForegroundWindow() == h;
    } finally {
      if (attachedTarget) AttachThreadInput(currentThread, targetThread, false);
      if (attachedForeground) AttachThreadInput(currentThread, foregroundThread, false);
    }
  }
  public static int CountGoSXWindowsForProcess(uint targetProcessId) {
    int count = 0;
    EnumWindowsCallback callback = delegate(IntPtr h, IntPtr l) {
      uint processId;
      GetWindowThreadProcessId(h, out processId);
      if (processId == targetProcessId) {
        StringBuilder className = new StringBuilder(128);
        GetClassName(h, className, className.Capacity);
        if (className.ToString() == "GoSXDesktopWindow") count++;
      }
      return true;
    };
    EnumWindows(callback, IntPtr.Zero);
    GC.KeepAlive(callback);
    return count;
  }
}
"@

function Get-ProfileProcesses {
  return @(Get-CimInstance Win32_Process -Filter "Name='msedgewebview2.exe'" |
    Where-Object { $_.CommandLine -and $_.CommandLine.IndexOf($profile, [StringComparison]::OrdinalIgnoreCase) -ge 0 })
}

function Activate-SmokeWindow([IntPtr]$handle) {
  for ($attempt = 0; $attempt -lt 10; $attempt++) {
    if ([WbDesktopSmokeNative]::ActivateWindow($handle)) { return $true }
    Start-Sleep -Milliseconds 150
  }
  return $false
}

function Get-SmokeResults {
  $curl = Join-Path $env:SystemRoot 'System32\curl.exe'
  if (-not (Test-Path -LiteralPath $curl)) { throw 'curl.exe is required to read the WSL smoke report without a system proxy' }
  $script:resultsRequest++
  $uri = "http://127.0.0.1:8175/results?run=$RunId&request=$script:resultsRequest"
  $raw = & $curl --noproxy '*' --fail --silent --show-error --max-time 6 $uri
  if ($LASTEXITCODE -ne 0) { throw ("smoke results curl failed with exit code {0}" -f $LASTEXITCODE) }
  return ($raw -join "`n") | ConvertFrom-Json
}

function Get-WindowRect([IntPtr]$handle) {
  $rect = New-Object WbDesktopSmokeNative+RECT
  if (-not [WbDesktopSmokeNative]::GetWindowRect($handle, [ref]$rect)) {
    throw 'GetWindowRect failed for the GoSX smoke window'
  }
  return $rect
}

function Get-RectSize($rect) {
  return @{ Width = $rect.Right - $rect.Left; Height = $rect.Bottom - $rect.Top }
}

function Save-OwnWindowCapture([IntPtr]$handle) {
  $rect = Get-WindowRect $handle
  $size = Get-RectSize $rect
  if ($size.Width -le 0 -or $size.Height -le 0) { throw 'smoke window has empty bounds' }
  $bitmap = New-Object System.Drawing.Bitmap $size.Width, $size.Height
  $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
  $dc = $graphics.GetHdc()
  try {
    $ok = [WbDesktopSmokeNative]::PrintWindow($handle, $dc, 2)
  } finally {
    $graphics.ReleaseHdc($dc)
    $graphics.Dispose()
  }
  if (-not $ok) { $bitmap.Dispose(); throw 'PrintWindow failed for the GoSX smoke window' }
  $bitmap.Save($capture)
  $bitmap.Dispose()
  Write-Output ("CAPTURE path={0} size={1}x{2}" -f $capture, $size.Width, $size.Height)
  return $rect
}

function Send-Key([byte]$key) {
  [WbDesktopSmokeNative]::keybd_event($key, 0, 0, [UIntPtr]::Zero)
  [WbDesktopSmokeNative]::keybd_event($key, 0, 2, [UIntPtr]::Zero)
}

function Send-Control-R {
  [WbDesktopSmokeNative]::keybd_event(0x11, 0, 0, [UIntPtr]::Zero)
  Send-Key 0x52
  [WbDesktopSmokeNative]::keybd_event(0x11, 0, 2, [UIntPtr]::Zero)
}

function Get-LastObservation($results) {
  if ($null -eq $results.observations -or $results.observations.Count -eq 0) { return $null }
  return $results.observations[$results.observations.Count - 1]
}

function Invoke-RuntimeVersionProbe {
  $versionOut = Join-Path $stage ("runtime-version-$RunId.stdout.txt")
  $versionErr = Join-Path $stage ("runtime-version-$RunId.stderr.txt")
  $probe = Start-Process -FilePath $exe -ArgumentList @('desktop', '--webview2-runtime-version') `
    -WorkingDirectory $stage -RedirectStandardOutput $versionOut -RedirectStandardError $versionErr -PassThru
  $probeHandle = $probe.Handle
  if (-not $probe.WaitForExit(15000)) {
    $info = Get-CimInstance Win32_Process -Filter ("ProcessId={0}" -f $probe.Id)
    if ($info.ExecutablePath -and $info.ExecutablePath.StartsWith($stage, [StringComparison]::OrdinalIgnoreCase)) {
      Stop-Process -Id $probe.Id -Force
    }
    throw 'RUNTIME_VERSION_ASSERTION: version query did not exit within 15 seconds'
  }
  [uint32]$code = 0
  if (-not [WbDesktopSmokeNative]::GetExitCodeProcess($probeHandle, [ref]$code) -or $code -ne 0) {
    throw ("RUNTIME_VERSION_ASSERTION: runtime query exited with code {0}" -f $code)
  }
  $version = (Get-Content -LiteralPath $versionOut -Raw).Trim()
  if ($version -notmatch '^\d+\.\d+\.\d+\.\d+$') {
    throw ("RUNTIME_VERSION_ASSERTION: unexpected version output {0}" -f $version)
  }
  Write-Output ("RUNTIME_VERSION_PASS version={0}" -f $version)
}

function Invoke-AvailabilityFailureProbe([string]$name, [string]$expectedText, [string]$browserFolder, [bool]$copyLoader) {
  $probeDir = Join-Path $stage ("probe-{0}-{1}" -f $name, $RunId)
  New-Item -ItemType Directory -Path $probeDir -Force | Out-Null
  $probeExe = Join-Path $probeDir 'gosx.exe'
  Copy-Item -LiteralPath $exe -Destination $probeExe -Force
  if ($copyLoader) { Copy-Item -LiteralPath (Join-Path $stage 'WebView2Loader.dll') -Destination (Join-Path $probeDir 'WebView2Loader.dll') -Force }
  $probeOut = Join-Path $probeDir 'probe.stdout.txt'
  $probeErr = Join-Path $probeDir 'probe.stderr.txt'
  $probeArgs = @('desktop', "--url=$url", '--native-bridge', '--additional-browser-arguments=--mute-audio')
  if ($browserFolder -ne '') { $probeArgs += "--browser-executable-folder=$browserFolder" }
  $probe = Start-Process -FilePath $probeExe -ArgumentList $probeArgs -WorkingDirectory $probeDir `
    -RedirectStandardOutput $probeOut -RedirectStandardError $probeErr -PassThru
  $probeHandle = $probe.Handle
  if (-not $probe.WaitForExit(15000)) {
    $info = Get-CimInstance Win32_Process -Filter ("ProcessId={0}" -f $probe.Id)
    if ($info.ExecutablePath -and $info.ExecutablePath.StartsWith($stage, [StringComparison]::OrdinalIgnoreCase)) {
      Stop-Process -Id $probe.Id -Force
    }
    throw ("AVAILABILITY_ASSERTION: {0} probe did not exit within 15 seconds" -f $name)
  }
  [uint32]$code = 0
  if (-not [WbDesktopSmokeNative]::GetExitCodeProcess($probeHandle, [ref]$code) -or $code -ne 1) {
    throw ("AVAILABILITY_ASSERTION: {0} probe exit code was {1}, want 1" -f $name, $code)
  }
  $output = (Get-Content -LiteralPath $probeOut -Raw) + (Get-Content -LiteralPath $probeErr -Raw)
  if ($output -notmatch [regex]::Escape($expectedText)) {
    throw ("AVAILABILITY_ASSERTION: {0} probe did not report {1}: {2}" -f $name, $expectedText, $output)
  }
  if ([WbDesktopSmokeNative]::CountGoSXWindowsForProcess([uint32]$probe.Id) -gt 0) {
    throw ("AVAILABILITY_ASSERTION: {0} probe created a window before returning its availability error" -f $name)
  }
  Write-Output ("AVAILABILITY_PROBE_PASS name={0} exit_code=1 window_created=false" -f $name)
}

try {
  if (-not (Test-Path -LiteralPath $exe)) { throw "staged host is missing: $exe" }
  $stale = Get-ProfileProcesses
  if ($stale.Count -gt 0) { throw "profile already has $($stale.Count) WebView2 processes: $profile" }

  if ($UseOptionsArguments) {
    [Environment]::SetEnvironmentVariable('WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS', $null, 'Process')
    $arguments = @('desktop', "--url=$url", "--title=$title", "--user-data-dir=$profile", '--native-bridge', '--additional-browser-arguments=--mute-audio')
  } else {
    $env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS = '--mute-audio'
    $arguments = @('desktop', "--url=$url", "--title=$title", "--user-data-dir=$profile", '--native-bridge')
  }

  if ($CheckShippingFeatures) {
    Invoke-RuntimeVersionProbe
    $missingRuntime = Join-Path $stage ("missing-fixed-runtime-{0}" -f $RunId)
    Invoke-AvailabilityFailureProbe 'missing-loader' 'loader unavailable' '' $false
    Invoke-AvailabilityFailureProbe 'missing-runtime' 'runtime unavailable' $missingRuntime $true
  }

  $process = Start-Process -FilePath $exe -ArgumentList $arguments -WorkingDirectory $stage `
    -RedirectStandardOutput $stdout -RedirectStandardError $stderr -PassThru
  $processHandle = $process.Handle
  Write-Output ("HOST pid={0} path={1} profile={2}" -f $process.Id, $exe, $profile)

  $aliveSince = $null
  $aliveSamples = 0
  $additionalArgumentObserved = $false
  for ($i = 0; $i -lt 10 -and $null -eq $aliveSince; $i++) {
    Start-Sleep -Milliseconds 500
    $process.Refresh()
    $children = Get-ProfileProcesses
    if ($children | Where-Object { $_.CommandLine -match '(^|\s)--mute-audio(\s|$)' }) {
      $additionalArgumentObserved = $true
    }
    $handle = $process.MainWindowHandle
    if ($children.Count -gt 0) { $aliveSince = Get-Date; $aliveSamples = 1 }
    $types = ($children | ForEach-Object {
      if ($_.CommandLine -match '--type=([a-z-]+)') { $matches[1] } else { 'browser' }
    }) -join ','
    Write-Output ("STARTUP t={0:N1}s host_alive={1} webview2_count={2} types=[{3}] hwnd=0x{4:X}" -f ((Get-Date)-$startedAt).TotalSeconds, (-not $process.HasExited), $children.Count, $types, $handle.ToInt64())
    if ($process.HasExited) { throw "host exited during startup with code $($process.ExitCode)" }
  }

  if ($null -eq $aliveSince) {
    $lifetimeFailure = $true
    $failures.Add('PROFILE_LIFETIME: no WebView2 process appeared for the smoke profile')
  } else {
    for ($i = 0; $i -lt 15; $i++) {
      Start-Sleep -Seconds 1
      $process.Refresh()
      $children = Get-ProfileProcesses
      if ($children | Where-Object { $_.CommandLine -match '(^|\s)--mute-audio(\s|$)' }) {
        $additionalArgumentObserved = $true
      }
      $types = ($children | ForEach-Object {
        if ($_.CommandLine -match '--type=([a-z-]+)') { $matches[1] } else { 'browser' }
      }) -join ','
      $elapsed = ((Get-Date) - $aliveSince).TotalSeconds
      Write-Output ("LIFETIME t={0:N1}s host_alive={1} webview2_count={2} types=[{3}]" -f $elapsed, (-not $process.HasExited), $children.Count, $types)
      if ($process.HasExited) { throw "host exited during lifetime check with code $($process.ExitCode)" }
      if ($children.Count -eq 0) {
        $lifetimeFailure = $true
        $failures.Add(("PROFILE_LIFETIME: WebView2 processes for the profile exited after {0:N1}s" -f $elapsed))
        break
      }
    }
  }

  if ($ExpectLifetimeFailure) {
    if (-not $lifetimeFailure) { $failures.Add('EXPECTED_BASELINE_FAILURE_MISSING: profile survived the 15 second lifetime check') }
    else { $failures.Clear() }
  } elseif ($lifetimeFailure) {
    # Continue to WM_CLOSE and process cleanup so the expected failure is safe to inspect.
  } else {
    if ($UseOptionsArguments -and -not $additionalArgumentObserved) {
      $failures.Add('BROWSER_ARGUMENT_ASSERTION: --mute-audio was not present in the WebView2 process command line')
    } elseif ($UseOptionsArguments) {
      Write-Output 'BROWSER_ARGUMENTS_PASS --mute-audio reached a WebView2 process'
    }
    $report = $null
    for ($i = 0; $i -lt 10 -and $null -eq $report; $i++) {
      try {
        $results = Get-SmokeResults
        if ($results.report) { $report = $results.report }
      } catch { Start-Sleep -Milliseconds 500 }
      if ($null -eq $report) { Start-Sleep -Milliseconds 500 }
    }
    if ($null -eq $report) {
      $failures.Add('PAGE_REPORT: the probe page did not post a report')
    } else {
      Write-Output ("PAGE_REPORT webgl2={0} chromeWebview={1} bridgeRoundTrip={2} title={3}" -f $report.webgl2, $report.chromeWebview, $report.bridgeRoundTrip, $report.bridgeTitle)
      if ($report.webgl2 -ne $true) { $failures.Add('PAGE_ASSERTION: WebGL2 was unavailable') }
      if ($report.chromeWebview -ne $true) { $failures.Add('PAGE_ASSERTION: chrome.webview was unavailable') }
      if ($report.bridgeRoundTrip -ne $true) { $failures.Add('BRIDGE_ASSERTION: JS -> Go -> JS round trip failed') }
    }

    $process.Refresh()
    $handle = $process.MainWindowHandle
    if ($handle -eq [IntPtr]::Zero) {
      $failures.Add('WINDOW_ASSERTION: host window handle was not available for PrintWindow capture')
    } else {
      [void][WbDesktopSmokeNative]::ShowWindow($handle, 9)
      [void][WbDesktopSmokeNative]::SetForegroundWindow($handle)
      Start-Sleep -Milliseconds 300
      $initialRect = Save-OwnWindowCapture $handle

      if ($CheckShippingFeatures) {
        $iconLarge = [IntPtr]::Zero
        $iconSmall = [IntPtr]::Zero
        $iconCount = [WbDesktopSmokeNative]::ExtractIconExW($exe, 0, [ref]$iconLarge, [ref]$iconSmall, 1)
        if ($iconCount -gt 0) {
          $windowLarge = [WbDesktopSmokeNative]::SendMessageW($handle, 0x007F, [IntPtr]1, [IntPtr]::Zero)
          $windowSmall = [WbDesktopSmokeNative]::SendMessageW($handle, 0x007F, [IntPtr]0, [IntPtr]::Zero)
          if ($windowLarge -eq [IntPtr]::Zero -or $windowSmall -eq [IntPtr]::Zero) {
            $failures.Add('ICON_ASSERTION: executable has an icon resource but the window is missing a big or small icon')
          }
          Write-Output ("ICON_CHECK resources={0} large_set={1} small_set={2}" -f $iconCount, ($windowLarge -ne [IntPtr]::Zero), ($windowSmall -ne [IntPtr]::Zero))
        } else {
          Write-Output 'ICON_CHECK skipped: executable has no icon resource'
        }
        if ($iconLarge -ne [IntPtr]::Zero) { [void][WbDesktopSmokeNative]::DestroyIcon($iconLarge) }
        if ($iconSmall -ne [IntPtr]::Zero -and $iconSmall -ne $iconLarge) { [void][WbDesktopSmokeNative]::DestroyIcon($iconSmall) }

        if (-not (Activate-SmokeWindow $handle)) {
          throw 'FOREGROUND_ASSERTION: could not activate the GoSX window before browser input'
        }
        $rect = Get-WindowRect $handle
        $centerX = [int](($rect.Left + $rect.Right) / 2)
        $centerY = [int](($rect.Top + $rect.Bottom) / 2)
        [void][WbDesktopSmokeNative]::SetCursorPos($centerX, $centerY)
        Start-Sleep -Milliseconds 200

        $before = $results
        $pageLoads = [int]$before.probe_gets
        Send-Control-R
        Start-Sleep -Milliseconds 1400
        Send-Key 0x74
        Start-Sleep -Milliseconds 1400
        $beforeZoom = Get-LastObservation $before
        [WbDesktopSmokeNative]::keybd_event(0x11, 0, 0, [UIntPtr]::Zero)
        [WbDesktopSmokeNative]::mouse_event(0x0800, 0, 0, 120, [UIntPtr]::Zero)
        [WbDesktopSmokeNative]::keybd_event(0x11, 0, 2, [UIntPtr]::Zero)
        Start-Sleep -Milliseconds 1600
        $afterZoom = Get-SmokeResults
        $afterObservation = Get-LastObservation $afterZoom
        if ([int]$afterZoom.probe_gets -ne $pageLoads) { $failures.Add('ACCELERATOR_ASSERTION: Ctrl+R, F5, or Ctrl+wheel caused a browser reload') }
        if ($null -eq $beforeZoom -or $null -eq $afterObservation -or
            [math]::Abs([double]$afterObservation.dpr - [double]$beforeZoom.dpr) -gt 0.01 -or
            [int]$afterObservation.width -ne [int]$beforeZoom.width) {
          $failures.Add('ZOOM_ASSERTION: Ctrl+wheel changed the WebView scale')
        }
        Write-Output ("PRODUCTION_KEYS ctrlR_checked=true f5_checked=true loads_before={0} loads_after={1} zoom_before={2} zoom_after={3}" -f $pageLoads, $afterZoom.probe_gets, $beforeZoom.dpr, $afterObservation.dpr)

        if (-not (Activate-SmokeWindow $handle)) {
          throw 'FOREGROUND_ASSERTION: could not reactivate the GoSX window before fullscreen input'
        }
        $beforeFull = Get-WindowRect $handle
        $beforeFullSize = Get-RectSize $beforeFull
        $clickPoint = New-Object WbDesktopSmokeNative+POINT
        $clickPoint.X = [int][math]::Round([double]$report.fullscreenButtonX)
        $clickPoint.Y = [int][math]::Round([double]$report.fullscreenButtonY)
        if (-not [WbDesktopSmokeNative]::ClientToScreen($handle, [ref]$clickPoint)) {
          throw 'FULLSCREEN_ASSERTION: could not map the probe button into screen coordinates'
        }
        [void][WbDesktopSmokeNative]::SetCursorPos($clickPoint.X, $clickPoint.Y)
        Start-Sleep -Milliseconds 200
        $hitWindow = [WbDesktopSmokeNative]::WindowFromPoint($clickPoint)
        $hitClass = New-Object System.Text.StringBuilder 128
        [void][WbDesktopSmokeNative]::GetClassName($hitWindow, $hitClass, $hitClass.Capacity)
        $hitProcessId = [uint32]0
        [void][WbDesktopSmokeNative]::GetWindowThreadProcessId($hitWindow, [ref]$hitProcessId)
        Write-Output ("FULLSCREEN_CLICK point={0},{1} hit={2} class={3} pid={4} foreground={5}" -f `
          $clickPoint.X, $clickPoint.Y, $hitWindow, $hitClass, $hitProcessId, [WbDesktopSmokeNative]::GetForegroundWindow())
        if ($hitClass.ToString() -notlike 'Chrome_*') {
          throw ("FOREGROUND_ASSERTION: fullscreen click resolved to {0}, not a WebView window" -f $hitClass)
        }
        [WbDesktopSmokeNative]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
        [WbDesktopSmokeNative]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
        $entered = $false
        for ($i = 0; $i -lt 20; $i++) {
          Start-Sleep -Milliseconds 250
          $fullRect = Get-WindowRect $handle
          $fullSize = Get-RectSize $fullRect
          if ($fullSize.Width -gt ($beforeFullSize.Width + 100) -and $fullSize.Height -gt ($beforeFullSize.Height + 100)) { $entered = $true; break }
        }
        if (-not $entered) { $failures.Add('FULLSCREEN_ASSERTION: HTML request did not expand the native window') }
        Start-Sleep -Seconds 3
        $restored = $false
        for ($i = 0; $i -lt 20; $i++) {
          $restoredRect = Get-WindowRect $handle
          if ($restoredRect.Left -eq $beforeFull.Left -and $restoredRect.Top -eq $beforeFull.Top -and
              $restoredRect.Right -eq $beforeFull.Right -and $restoredRect.Bottom -eq $beforeFull.Bottom) {
            $restored = $true; break
          }
          Start-Sleep -Milliseconds 250
        }
        if (-not $restored) { $failures.Add('FULLSCREEN_ASSERTION: window did not restore its earlier bounds') }
        $fullscreenReport = (Get-SmokeResults).report
        if ($fullscreenReport.fullscreenEntered -ne $true -or $fullscreenReport.fullscreenExited -ne $true) {
          $failures.Add('FULLSCREEN_ASSERTION: HTML fullscreen enter/exit events were not both observed')
        }
        $observedFullRect = Get-WindowRect $handle
        Write-Output ("HTML_FULLSCREEN entered={0} restored={1} events={2}/{3} before={4},{5},{6},{7} after={8},{9},{10},{11}" -f `
          $entered, $restored, $fullscreenReport.fullscreenEntered, $fullscreenReport.fullscreenExited, `
          $beforeFull.Left, $beforeFull.Top, $beforeFull.Right, $beforeFull.Bottom, `
          $observedFullRect.Left, $observedFullRect.Top, $observedFullRect.Right, $observedFullRect.Bottom)
      }
    }
  }
} catch {
  $failures.Add($_.Exception.Message)
  Write-Output ("SMOKE_EXCEPTION_LOCATION line={0} source={1}" -f $_.InvocationInfo.ScriptLineNumber, $_.InvocationInfo.Line)
  Write-Output ("SMOKE_EXCEPTION: {0}" -f $_.Exception.ToString())
} finally {
  if ($null -ne $process) {
    $process.Refresh()
    if (-not (Test-Path -LiteralPath $capture) -and -not $process.HasExited) {
      $captureHandle = $process.MainWindowHandle
      if ($captureHandle -ne [IntPtr]::Zero) {
        try { $null = Save-OwnWindowCapture $captureHandle }
        catch { Write-Output ("CAPTURE_UNAVAILABLE: {0}" -f $_.Exception.Message) }
      }
    }
    if ($process.HasExited) {
      $failures.Add('CLOSE_ASSERTION: host exited before the smoke sent WM_CLOSE')
    } else {
      $handle = $process.MainWindowHandle
      if ($handle -ne [IntPtr]::Zero) {
        Write-Output ("WM_CLOSE hwnd=0x{0:X}" -f $handle.ToInt64())
        [void][WbDesktopSmokeNative]::PostMessageW($handle, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero)
      } else {
        [void]$process.CloseMainWindow()
      }
      if (-not $process.WaitForExit(10000)) {
        $hostInfo = Get-CimInstance Win32_Process -Filter ("ProcessId={0}" -f $process.Id)
        if ($hostInfo.ExecutablePath -and $hostInfo.ExecutablePath.StartsWith($stage, [StringComparison]::OrdinalIgnoreCase)) {
          Write-Output ("STOP_OWN_HOST pid={0} path={1}" -f $process.Id, $hostInfo.ExecutablePath)
          Stop-Process -Id $process.Id -Force
        }
        $failures.Add('CLOSE_ASSERTION: host did not exit within 10 seconds of WM_CLOSE')
      }
    }
    $null = $process.WaitForExit()
    $process.Refresh()
    if (-not $process.HasExited) {
      $failures.Add('CLOSE_ASSERTION: host process remains after cleanup')
    } else {
      $hostExited = $true
      [uint32]$nativeExitCode = 0
      if ([WbDesktopSmokeNative]::GetExitCodeProcess($processHandle, [ref]$nativeExitCode)) {
        $hostExitCode = $nativeExitCode
      }
      Write-Output ("HOST_EXIT code={0}" -f $hostExitCode)
      if ($null -eq $hostExitCode -or [uint32]$hostExitCode -ne 0) {
        $failures.Add(("CLOSE_ASSERTION: host exited with code {0}" -f $hostExitCode))
      }
    }
  } else {
    $hostExited = $true
    Write-Output 'HOST_NOT_STARTED: preflight ended before the desktop window launch'
  }

  $remaining = @()
  for ($i = 0; $i -lt 10; $i++) {
    $remaining = Get-ProfileProcesses
    if ($remaining.Count -eq 0) { break }
    Start-Sleep -Seconds 1
  }
  if ($remaining.Count -gt 0) {
    $failures.Add(("CLOSE_ASSERTION: {0} WebView2 process(es) remain for profile {1}" -f $remaining.Count, $profile))
    foreach ($child in $remaining) { Write-Output ("REMAINING_WEBVIEW2 pid={0} path={1} cmd={2}" -f $child.ProcessId, $child.ExecutablePath, $child.CommandLine) }
  } else {
    $profileProcessesClear = $true
    if ($null -eq $process) {
      Write-Output 'CLEANUP_PASS host_started=false no_profile_webview2_processes=true'
    } elseif ($hostExited -and $null -ne $hostExitCode -and [uint32]$hostExitCode -eq 0) {
      Write-Output 'CLOSE_PASS host_exit_code=0 host_process_gone=true no_profile_webview2_processes=true'
    } else {
      Write-Output ("CLOSE_FAIL host_exit_code={0} host_process_gone={1} no_profile_webview2_processes=true" -f $hostExitCode, $hostExited)
    }
  }
  $cleanupPassed = ($null -eq $process -or $hostExited) -and $profileProcessesClear
}

$elapsedTotal = ((Get-Date) - $startedAt).TotalSeconds
Write-Output ("RUN_COMPLETE elapsed_seconds={0:N1} cleanup_passed={1}" -f $elapsedTotal, $cleanupPassed)
if ($elapsedTotal -ge 120) { $failures.Add('TIME_LIMIT_ASSERTION: Windows smoke run reached 2 minutes') }

if ($ExpectLifetimeFailure -and $lifetimeFailure -and $cleanupPassed) {
  Write-Output ("EXPECTED_BLOCKER_CAUGHT: origin/main failed the profile lifetime check; host exit code={0}" -f $hostExitCode)
  exit 0
}
if ($failures.Count -gt 0) {
  foreach ($failure in $failures) { Write-Output ("ASSERTION_FAILED: {0}" -f $failure) }
  exit 1
}
Write-Output 'WINDOWS_DESKTOP_SMOKE_PASS'
exit 0
