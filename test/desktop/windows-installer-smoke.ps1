param(
    [Parameter(Mandatory = $true)][string]$SetupV1,
    [Parameter(Mandatory = $true)][string]$SetupV2
)

$ErrorActionPreference = 'Stop'
$testRoot = 'C:\Temp\wb-rel-installer'
$installRoot = Join-Path $testRoot 'install'
$startMenu = Join-Path $testRoot 'start-menu'
$workDir = Join-Path $testRoot 'workers'
$dataDir = Join-Path $testRoot 'player-data'
$uninstallKeyName = 'GoSXTest-WBInstaller'
$registryPath = "Software\Microsoft\Windows\CurrentVersion\Uninstall\$uninstallKeyName"
$registryKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($registryPath)
$stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
$processIds = New-Object 'System.Collections.Generic.HashSet[int]'

Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;

public static class InstallerSmokeWindows {
    public delegate bool EnumWindowsProc(IntPtr hwnd, IntPtr parameter);
    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc callback, IntPtr parameter);
    [DllImport("user32.dll")] public static extern bool EnumChildWindows(IntPtr parent, EnumWindowsProc callback, IntPtr parameter);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hwnd, out uint processId);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowTextW(IntPtr hwnd, StringBuilder text, int maxCount);
    [DllImport("user32.dll")] public static extern int GetWindowTextLengthW(IntPtr hwnd);
    [DllImport("user32.dll")] public static extern bool PostMessageW(IntPtr hwnd, uint message, IntPtr wParam, IntPtr lParam);

    public static IntPtr FindDialog(int processId, string needle, out string contents) {
        IntPtr found = IntPtr.Zero;
        string foundText = "";
        EnumWindows((hwnd, ignored) => {
            uint owner;
            GetWindowThreadProcessId(hwnd, out owner);
            if (owner != (uint)processId) return true;
            var pieces = new List<string>();
            AddText(hwnd, pieces);
            EnumChildWindows(hwnd, (child, ignoredChild) => { AddText(child, pieces); return true; }, IntPtr.Zero);
            string text = String.Join(" ", pieces);
            if (text.IndexOf(needle, StringComparison.OrdinalIgnoreCase) >= 0) {
                found = hwnd;
                foundText = text;
                return false;
            }
            return true;
        }, IntPtr.Zero);
        contents = foundText;
        return found;
    }

    private static void AddText(IntPtr hwnd, List<string> pieces) {
        int length = GetWindowTextLengthW(hwnd);
        if (length <= 0) return;
        var text = new StringBuilder(length + 1);
        GetWindowTextW(hwnd, text, text.Capacity);
        if (text.Length > 0) pieces.Add(text.ToString());
    }
}
'@

function Assert-WithinLimit {
    if ($stopwatch.Elapsed.TotalSeconds -ge 170) {
        throw "Windows installer smoke reached the 170 second safety limit."
    }
}

function Trace([string]$Message) {
    Write-Host ("TRACE {0:n1}s {1}" -f $stopwatch.Elapsed.TotalSeconds, $Message)
}

function Assert-Equal([string]$Actual, [string]$Expected, [string]$Message) {
    if ($Actual -cne $Expected) { throw "$Message (actual '$Actual', expected '$Expected')" }
}

function Assert-TestInstallOverrides {
    if (-not $installRoot.StartsWith($testRoot, [StringComparison]::OrdinalIgnoreCase) -or
        -not $startMenu.StartsWith($testRoot, [StringComparison]::OrdinalIgnoreCase) -or
        -not $uninstallKeyName.StartsWith('GoSXTest-', [StringComparison]::Ordinal)) {
        throw 'Windows test overrides do not point into the reserved GoSX test area.'
    }
}

function Get-Dialog([System.Diagnostics.Process]$Process, [string]$Needle, [int]$Seconds = 20, [string]$RequiredText = '') {
    Trace "waiting for dialog '$Needle' (pid $($Process.Id))"
    $until = [DateTime]::UtcNow.AddSeconds($Seconds)
    do {
        Assert-WithinLimit
        $Process.Refresh()
        $text = ''
        $handle = [InstallerSmokeWindows]::FindDialog($Process.Id, $Needle, [ref]$text)
        if ($handle -ne [IntPtr]::Zero -and ($RequiredText -eq '' -or $text -match [regex]::Escape($RequiredText))) {
            Trace "found dialog '$Needle'"
            return @{ Handle = $handle; Text = $text }
        }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $until -and -not $Process.HasExited)
    throw "Installer dialog '$Needle' did not appear."
}

function Click-Dialog([IntPtr]$Handle, [int]$Command) {
    if (-not [InstallerSmokeWindows]::PostMessageW($Handle, 0x0111, [IntPtr]$Command, [IntPtr]::Zero)) {
        throw "Could not answer installer dialog with command $Command."
    }
}

function Wait-TestProcess([System.Diagnostics.Process]$Process) {
    Trace "waiting for process exit (pid $($Process.Id))"
    while (-not $Process.HasExited) {
        Assert-WithinLimit
        Start-Sleep -Milliseconds 100
        $Process.Refresh()
    }
    return $Process.ExitCode
}

function Get-RegisteredVersion {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($registryPath)
    if ($null -eq $key) { throw 'HKCU Uninstall key is missing.' }
    try { return [string]$key.GetValue('DisplayVersion') }
    finally { $key.Close() }
}

function Stop-TestHost([System.Diagnostics.Process]$Process) {
    if ($null -eq $Process -or $Process.HasExited) { return }
    $Process.Refresh()
    $image = $Process.MainModule.FileName
    if (-not $image.StartsWith($installRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to stop a process outside the test install root: $image"
    }
    [void]$Process.CloseMainWindow()
    if (-not $Process.WaitForExit(5000)) {
        Stop-Process -Id $Process.Id -Force
        [void]$Process.WaitForExit(5000)
    }
    if (-not $Process.HasExited) { throw "Test app process $($Process.Id) did not exit." }
}

function Start-TestHost([string]$ExpectedVersion) {
	$evidence = Join-Path $testRoot "app-evidence-$ExpectedVersion"
	$env:GOSX_INSTALLER_TEST_EVIDENCE = $evidence
	$env:GOSX_INSTALLER_TEST_WEBVIEW = Join-Path $dataDir 'webview2'
	$process = Start-Process -FilePath (Join-Path $installRoot 'wb-smoke.exe') -ArgumentList '--mute-audio' -PassThru
    [void]$processIds.Add($process.Id)
    $until = [DateTime]::UtcNow.AddSeconds(20)
    do {
        Assert-WithinLimit
        $process.Refresh()
        if ($process.MainWindowTitle -like "*installer smoke $ExpectedVersion*") { break }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $until -and -not $process.HasExited)
	if ($process.HasExited -or $process.MainWindowTitle -notlike "*installer smoke $ExpectedVersion*") {
		throw "Test app did not open its GoSX desktop window for version $ExpectedVersion."
	}
	$versionFile = Join-Path $evidence 'launched-version.txt'
	if (-not (Test-Path -LiteralPath $versionFile)) { throw 'Test app did not write its launch evidence.' }
	Assert-Equal (Get-Content -LiteralPath $versionFile -Raw).Trim() $ExpectedVersion 'Launched app version is wrong'
	Write-Host "PASS: launched muted GoSX desktop page version $ExpectedVersion."
    return $process
}

function Assert-InstallRemoved([bool]$ExpectData) {
    $until = [DateTime]::UtcNow.AddSeconds(25)
    do {
        Assert-WithinLimit
        $diagnostic = Join-Path $workDir 'installer-error.log'
        if (Test-Path -LiteralPath $diagnostic) {
            $detail = (Get-Content -LiteralPath $diagnostic -Raw).Trim()
            throw "Uninstaller worker failed: $detail"
        }
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($registryPath)
        if ($null -ne $key) { $key.Close() }
        if (-not (Test-Path -LiteralPath $installRoot) -and $null -eq $key) { break }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $until)
    if (Test-Path -LiteralPath $installRoot) { throw 'Installer uninstall left the install root.' }
    if (Test-Path -LiteralPath $startMenu) { throw 'Installer uninstall left the Start menu test folder.' }
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($registryPath)
    if ($null -ne $key) { $key.Close(); throw 'Installer uninstall left its HKCU Uninstall key.' }
    if ($ExpectData -and -not (Test-Path -LiteralPath (Join-Path $dataDir 'save.dat'))) {
        throw 'Uninstall did not keep the player data file.'
    }
    if (-not $ExpectData -and (Test-Path -LiteralPath $dataDir)) {
        throw 'Uninstall did not remove the player data directory.'
    }
    Write-Host "PASS: uninstall removed install files, shortcuts and HKCU entry; player data kept=$ExpectData."
}

function Invoke-SilentSetup([string]$Setup, [string[]]$Extra = @()) {
    Trace "starting silent setup $(Split-Path -Leaf $Setup)"
    New-Item -ItemType Directory -Force -Path $workDir | Out-Null
    Remove-Item -LiteralPath (Join-Path $workDir 'installer-error.log') -Force -ErrorAction SilentlyContinue
    $args = @('/S', '--test-install-root', $installRoot, '--test-start-menu', $startMenu,
        '--test-uninstall-key', $uninstallKeyName, '--test-work-dir', $workDir) + $Extra
    $process = Start-Process -FilePath $Setup -ArgumentList $args -PassThru
    [void]$processIds.Add($process.Id)
    return Wait-TestProcess $process
}

function Invoke-Uninstall([bool]$RemoveData) {
    Trace "starting uninstall remove-data=$RemoveData"
    Remove-Item -LiteralPath (Join-Path $workDir 'installer-error.log') -Force -ErrorAction SilentlyContinue
    $args = @('--uninstall', '/S', '--test-install-root', $installRoot, '--test-start-menu', $startMenu,
        '--test-uninstall-key', $uninstallKeyName, '--test-work-dir', $workDir)
    if ($RemoveData) { $args += '/delete-data' }
    $parent = Start-Process -FilePath (Join-Path $installRoot 'uninstall.exe') -ArgumentList $args -PassThru
    [void]$processIds.Add($parent.Id)
    if ((Wait-TestProcess $parent) -ne 0) { throw "Uninstaller launcher returned $($parent.ExitCode)." }
    Assert-InstallRemoved (-not $RemoveData)
}

function Invoke-DowngradePrompt {
    Trace 'starting downgrade prompt check'
    $args = @('--test-install-root', $installRoot, '--test-start-menu', $startMenu,
        '--test-uninstall-key', $uninstallKeyName, '--test-work-dir', $workDir)
    $process = Start-Process -FilePath $SetupV1 -ArgumentList $args -PassThru
    [void]$processIds.Add($process.Id)
    $dialog = Get-Dialog $process 'Install older version?'
    if ($dialog.Text -notmatch '1\.0\.0' -or $dialog.Text -notmatch '1\.0\.1') {
        throw 'Downgrade prompt did not identify both versions.'
    }
    Click-Dialog $dialog.Handle 7
    if ((Wait-TestProcess $process) -ne 0) { throw 'Declining a downgrade should cancel without changing the installation.' }
    Assert-Equal (Get-RegisteredVersion) '1.0.1' 'Declined downgrade changed the installed version'
    Write-Host 'PASS: lower version asked first; declining kept version 2.'
}

try {
    Assert-TestInstallOverrides
    if (-not (Test-Path -LiteralPath $SetupV1) -or -not (Test-Path -LiteralPath $SetupV2)) {
        throw 'Setup input files are missing.'
    }
    Remove-Item -LiteralPath $installRoot, $startMenu, $workDir, $dataDir -Recurse -Force -ErrorAction SilentlyContinue

    Trace 'install version 1'
    $exitCode = Invoke-SilentSetup $SetupV1
    if ($exitCode -ne 0) {
        $diagnostic = Join-Path $workDir 'installer-error.log'
        $detail = if (Test-Path -LiteralPath $diagnostic) { (Get-Content -LiteralPath $diagnostic -Raw).Trim() } else { 'no installer diagnostic was written' }
        throw "Version 1 silent install failed with exit code ${exitCode}: $detail"
    }
    if (-not (Test-Path -LiteralPath (Join-Path $installRoot 'wb-smoke.exe'))) { throw 'Install did not extract the host executable.' }
    $uninstallerSize = (Get-Item -LiteralPath (Join-Path $installRoot 'uninstall.exe')).Length
    $setupSize = (Get-Item -LiteralPath $SetupV1).Length
    if ($uninstallerSize -ge $setupSize) { throw 'Installed uninstaller unexpectedly contains the full Setup payload.' }
    if (-not (Test-Path -LiteralPath (Join-Path $startMenu 'WELDBREAKERS Smoke.lnk'))) { throw 'Install did not create the Start menu shortcut.' }
    $uninstall = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($registryPath)
    if ($null -eq $uninstall) { throw 'Install did not create the HKCU Uninstall key.' }
    Assert-Equal ([string]$uninstall.GetValue('DisplayVersion')) '1.0.0' 'Uninstall DisplayVersion is wrong'
    Assert-Equal ([string]$uninstall.GetValue('InstallLocation')) $installRoot 'Uninstall InstallLocation is wrong'
    if ([int]$uninstall.GetValue('NoModify') -ne 1 -or [int]$uninstall.GetValue('NoRepair') -ne 1) {
        $uninstall.Close(); throw 'Uninstall NoModify/NoRepair values are wrong.'
    }
    $uninstall.Close()
    Write-Host 'PASS: per-user install, Start menu shortcut and HKCU Uninstall metadata.'

    Trace 'launch version 1'
    $app = Start-TestHost '1.0.0'
    Trace 'start version 2 upgrade while app runs'
    $setupArgs = @('--test-install-root', $installRoot, '--test-start-menu', $startMenu,
        '--test-uninstall-key', $uninstallKeyName, '--test-work-dir', $workDir)
    $upgrade = Start-Process -FilePath $SetupV2 -ArgumentList $setupArgs -PassThru
    [void]$processIds.Add($upgrade.Id)
    $runningDialog = Get-Dialog $upgrade 'App is running' 20 'close'
    Stop-TestHost $app
    Click-Dialog $runningDialog.Handle 4
    $launchDialog = Get-Dialog $upgrade 'Launch'
    Click-Dialog $launchDialog.Handle 7
    if (-not $upgrade.WaitForExit(30000)) { throw 'Version 2 upgrade did not finish within 30 seconds.' }
    if ($upgrade.ExitCode -ne 0) { throw "Version 2 upgrade failed with exit code $($upgrade.ExitCode)." }
    Assert-Equal (Get-RegisteredVersion) '1.0.1' 'Upgraded DisplayVersion is wrong'
    Write-Host 'PASS: version 2 upgrade waited for the player to close the running app.'

    $interrupted = Invoke-SilentSetup $SetupV2 @('--test-fail-after-extract')
    if ($interrupted -eq 0) { throw 'Interrupted install unexpectedly succeeded.' }
    Assert-Equal (Get-RegisteredVersion) '1.0.1' 'Interrupted install changed the registered version'
    $app = Start-TestHost '1.0.1'
    Stop-TestHost $app
    Write-Host 'PASS: interrupted install left version 2 installed and launchable.'

    Trace 'check downgrade prompt'
    Invoke-DowngradePrompt

    Trace 'uninstall while keeping data'
    New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
    Set-Content -LiteralPath (Join-Path $dataDir 'save.dat') -Value 'keep this player save'
    Invoke-Uninstall $false

    Trace 'reinstall version 2'
    $exitCode = Invoke-SilentSetup $SetupV2
    if ($exitCode -ne 0) { throw "Second version 2 install failed with exit code $exitCode." }
    New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
    Set-Content -LiteralPath (Join-Path $dataDir 'save.dat') -Value 'remove this player save'
    Trace 'uninstall while removing data'
    Invoke-Uninstall $true

    if ($stopwatch.Elapsed.TotalSeconds -ge 180) { throw 'Windows installer smoke exceeded its 3 minute limit.' }
    Write-Host ("PASS: installer matrix completed in {0:n1} seconds." -f $stopwatch.Elapsed.TotalSeconds)
}
finally {
    foreach ($processId in $processIds) {
        try {
            $process = [System.Diagnostics.Process]::GetProcessById($processId)
            if ($process.HasExited) { continue }
            $process.Refresh()
            $image = $process.MainModule.FileName
            if ($image.StartsWith($testRoot, [StringComparison]::OrdinalIgnoreCase)) {
                [void]$process.CloseMainWindow()
                if (-not $process.WaitForExit(1500)) { Stop-Process -Id $processId -Force -ErrorAction SilentlyContinue }
            }
        } catch { }
    }
    Get-Process | ForEach-Object {
        try {
            $_.Refresh()
            if (-not $_.HasExited -and $_.MainModule.FileName.StartsWith($testRoot, [StringComparison]::OrdinalIgnoreCase)) {
                Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
            }
        } catch { }
    }
    if (Test-Path -LiteralPath $startMenu) { Remove-Item -LiteralPath $startMenu -Recurse -Force -ErrorAction SilentlyContinue }
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($registryPath)
    if ($null -ne $key) {
        $key.Close()
        [Microsoft.Win32.Registry]::CurrentUser.DeleteSubKeyTree($registryPath, $false)
    }
}
