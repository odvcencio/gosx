param(
    [Parameter(Mandatory = $true)][string]$Client,
    [Parameter(Mandatory = $true)][string]$PublicKey,
    [Parameter(Mandatory = $true)][string]$FixtureHost
)

$ErrorActionPreference = 'Stop'
$testRoot = 'C:\Temp\wb-rel-installer'
$uninstallKeyNames = @('GoSXTest-WBInstaller', 'GoSXTest-WBUpdateCheck')
$stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
$processes = New-Object 'System.Collections.Generic.List[System.Diagnostics.Process]'
$loopbackProxy = $null

Add-Type -TypeDefinition @'
using System;
using System.Net;
using System.Net.Sockets;
using System.Threading;
using System.Threading.Tasks;

public sealed class WslUpdateLoopbackProxy : IDisposable {
    private readonly TcpListener listener;
    private readonly CancellationTokenSource stop = new CancellationTokenSource();
    private readonly Task acceptTask;
    private readonly System.Collections.Generic.List<Task> relayTasks = new System.Collections.Generic.List<Task>();
    private readonly string fixtureHost;
    private readonly int fixturePort;

    public WslUpdateLoopbackProxy(string host, int port) {
        fixtureHost = host;
        fixturePort = port;
        listener = new TcpListener(IPAddress.Loopback, port);
        listener.Start();
        acceptTask = Task.Run((Func<Task>)AcceptLoop);
    }

    private async Task AcceptLoop() {
        while (!stop.IsCancellationRequested) {
            TcpClient client = null;
            try {
                client = await listener.AcceptTcpClientAsync().ConfigureAwait(false);
            } catch (ObjectDisposedException) {
                if (stop.IsCancellationRequested) break;
                throw;
            } catch (SocketException) {
                if (stop.IsCancellationRequested) break;
                throw;
            }
            if (client != null) {
                TcpClient accepted = client;
                Task relay = Task.Run(() => Relay(accepted, stop.Token));
                lock (relayTasks) { relayTasks.Add(relay); }
            }
        }
    }

    private async Task Relay(TcpClient client, CancellationToken token) {
        using (client)
        using (var upstream = new TcpClient())
        using (var linked = CancellationTokenSource.CreateLinkedTokenSource(token)) {
            try {
                await upstream.ConnectAsync(fixtureHost, fixturePort).ConfigureAwait(false);
                NetworkStream clientStream = client.GetStream();
                NetworkStream upstreamStream = upstream.GetStream();
                Task toFixture = clientStream.CopyToAsync(upstreamStream, 81920, linked.Token);
                Task toClient = upstreamStream.CopyToAsync(clientStream, 81920, linked.Token);
                await Task.WhenAll(toFixture, toClient).ConfigureAwait(false);
            } catch (OperationCanceledException) {
            } catch (ObjectDisposedException) {
            } catch (SocketException) {
            } catch (System.IO.IOException) {
            }
        }
    }

    public void Dispose() {
        stop.Cancel();
        listener.Stop();
        try { acceptTask.Wait(TimeSpan.FromSeconds(2)); } catch { }
        Task[] active;
        lock (relayTasks) { active = relayTasks.ToArray(); }
        try { Task.WaitAll(active, TimeSpan.FromSeconds(2)); } catch { }
        stop.Dispose();
    }
}
'@

function Assert-WithinLimit {
    if ($stopwatch.Elapsed.TotalSeconds -ge 170) {
        throw 'Windows update-check smoke reached its 170 second safety limit.'
    }
}

function Assert-TestEnvironment {
    if ((Get-Location).Path -ne 'C:\Temp') { throw "PowerShell must run from C:\Temp; got $((Get-Location).Path)." }
    if (-not $Client.StartsWith($testRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Update test executable is outside the reserved test root: $Client"
    }
    if (-not (Test-Path -LiteralPath $Client -PathType Leaf)) { throw "Update test executable is missing: $Client" }
    $fixtureAddress = $null
    if (-not [System.Net.IPAddress]::TryParse($FixtureHost, [ref]$fixtureAddress) -or
        $fixtureAddress.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetwork) {
        throw "WSL fixture host is not an IPv4 address: $FixtureHost"
    }
    Assert-TestUninstallKeysAbsent
}

function Assert-TestUninstallKeysAbsent {
    foreach ($name in $uninstallKeyNames) {
        $path = "Software\Microsoft\Windows\CurrentVersion\Uninstall\$name"
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($path)
        if ($null -ne $key) { $key.Close(); throw "Unexpected test Uninstall key exists: $name" }
    }
}

function Quote-Argument([string]$Value) {
    return '"' + $Value.Replace('"', '\"') + '"'
}

function Invoke-Client([string]$Case, [string]$Manifest, [string]$StateFile, [bool]$Online) {
    Assert-WithinLimit
    $onlineFlag = '-online=' + $Online.ToString().ToLowerInvariant()
    $arguments = @(
        '-manifest', $Manifest,
        '-app', 'wb.test',
        '-channel', 'stable',
        '-version', '1.0.0',
        '-public-key', $PublicKey,
        '-state-file', $StateFile,
        '-enabled=true',
        '-startup-complete=true',
        $onlineFlag,
        '-allow-loopback-http-for-tests=true'
    )
    $info = New-Object System.Diagnostics.ProcessStartInfo
    $info.FileName = $Client
    $info.Arguments = (($arguments | ForEach-Object { Quote-Argument ([string]$_) }) -join ' ')
    $info.WorkingDirectory = $testRoot
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $process = [System.Diagnostics.Process]::Start($info)
    if ($null -eq $process) { throw "Could not start update client for $Case." }
    [void]$processes.Add($process)
    $stdoutTask = $process.StandardOutput.ReadToEndAsync()
    $stderrTask = $process.StandardError.ReadToEndAsync()
    if (-not $process.WaitForExit(25000)) {
        $image = $process.MainModule.FileName
        if (-not $image.StartsWith($testRoot, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Refusing to stop process outside test root: $image"
        }
        $process.Kill()
        throw "Update client exceeded 25 seconds for $Case."
    }
    $stdout = $stdoutTask.Result.Trim()
    $stderr = $stderrTask.Result.Trim()
    if ($process.ExitCode -ne 0) { throw "Update client $Case exited $($process.ExitCode): $stderr $stdout" }
    try { return $stdout | ConvertFrom-Json }
    catch { throw "Update client $Case did not return JSON: $stdout; $stderr" }
}

function Test-NativeIPv4LoopbackForwarding {
    $request = [System.Net.HttpWebRequest]::Create('http://127.0.0.1:8210/health')
    $request.Timeout = 1200
    $request.ReadWriteTimeout = 1200
    $request.KeepAlive = $false
    try {
        $response = $request.GetResponse()
        try { return $response.StatusCode -eq [System.Net.HttpStatusCode]::NoContent }
        finally { $response.Close() }
    } catch [System.Net.WebException] {
        if ($_.Exception.Status -eq [System.Net.WebExceptionStatus]::ConnectFailure -or
            $_.Exception.Status -eq [System.Net.WebExceptionStatus]::Timeout) {
            return $false
        }
        throw
    }
}

function Start-FixtureIPv4Loopback {
    if (Test-NativeIPv4LoopbackForwarding) {
        Write-Host 'PASS: native Windows IPv4 loopback reaches the WSL fixture.'
        return $null
    }
    $probe = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Loopback, 8210)
    try { $probe.Start() }
    catch { throw 'IPv4 loopback port 8210 is occupied; refusing to replace its listener.' }
    finally { $probe.Stop() }

    $proxy = New-Object WslUpdateLoopbackProxy($FixtureHost, 8210)
    $until = [DateTime]::UtcNow.AddSeconds(5)
    do {
        if (Test-NativeIPv4LoopbackForwarding) {
            Write-Host "PASS: temporary IPv4 loopback relay reached the WSL fixture at ${FixtureHost}:8210."
            return $proxy
        }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $until)
    $proxy.Dispose()
    throw "Temporary IPv4 loopback relay could not reach the WSL fixture at ${FixtureHost}:8210."
}

try {
    Assert-TestEnvironment
    $loopbackProxy = Start-FixtureIPv4Loopback
    $stateRoot = Join-Path $testRoot 'update-check-state'
    New-Item -ItemType Directory -Force -Path $stateRoot | Out-Null

    $newUrl = 'http://127.0.0.1:8210/new/latest.json'
    $newState = Join-Path $stateRoot 'new.json'
    $new = Invoke-Client 'newer version' $newUrl $newState $true
    if ($new.error -or $new.status -ne 'available' -or $new.version -ne '2.0.0' -or
        $new.download_page -ne 'https://example.invalid/wb-release' -or
        $new.notes -ne 'WELDBREAKERS update fixture 2.0.0.') {
        throw "Newer-version check failed: $($new | ConvertTo-Json -Compress)"
    }
    Write-Host "PASS: newer version 2.0.0, notes, and HTTPS download page returned."

    $newAgain = Invoke-Client 'daily limit' $newUrl $newState $true
    if ($newAgain.error -or $newAgain.status -ne 'skipped-daily-limit') {
        throw "Daily-limit check failed: $($newAgain | ConvertTo-Json -Compress)"
    }
    Write-Host 'PASS: second check on the same state file skipped within 24 hours.'

    $same = Invoke-Client 'same version' 'http://127.0.0.1:8210/same/latest.json' (Join-Path $stateRoot 'same.json') $true
    if ($same.error -or $same.status -ne 'up-to-date' -or $same.version -ne '1.0.0') {
        throw "Same-version check failed: $($same | ConvertTo-Json -Compress)"
    }
    Write-Host 'PASS: same version reported up-to-date.'

    $bad = Invoke-Client 'bad signature' 'http://127.0.0.1:8210/bad/latest.json' (Join-Path $stateRoot 'bad.json') $true
    if (-not $bad.error -or $bad.error -notlike '*signature is invalid*' -or $bad.status -ne 'check-failed') {
        throw "Bad-signature check was not rejected: $($bad | ConvertTo-Json -Compress)"
    }
    Write-Host 'PASS: invalid detached signature rejected.'

    $offline = Invoke-Client 'offline' 'http://127.0.0.1:8211/offline/latest.json' (Join-Path $stateRoot 'offline.json') $false
    if ($offline.error -or $offline.status -ne 'skipped-offline' -or $offline.online -ne $false) {
        throw "Offline check attempted a request or returned the wrong status: $($offline | ConvertTo-Json -Compress)"
    }
    Write-Host 'PASS: offline state skipped without a request.'
    Assert-TestUninstallKeysAbsent
    Write-Host 'PASS: no test-only HKCU Uninstall keys were created.'

    Write-Host ("PASS: Windows update-check matrix completed in {0:n1} seconds." -f $stopwatch.Elapsed.TotalSeconds)
}
finally {
    foreach ($process in $processes) {
        try {
            $process.Refresh()
            if (-not $process.HasExited) {
                $image = $process.MainModule.FileName
                if (-not $image.StartsWith($testRoot, [StringComparison]::OrdinalIgnoreCase)) {
                    throw "Refusing to stop process outside test root: $image"
                }
                if ($process.MainWindowHandle -ne [IntPtr]::Zero) { [void]$process.CloseMainWindow() }
                if (-not $process.WaitForExit(3000)) { $process.Kill(); [void]$process.WaitForExit(3000) }
            }
        }
        finally { $process.Dispose() }
    }
    if ($null -ne $loopbackProxy) { $loopbackProxy.Dispose() }
    Assert-TestUninstallKeysAbsent
}
