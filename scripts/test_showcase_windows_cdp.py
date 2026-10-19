import base64
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


HELPER = Path(__file__).with_name("showcase-windows-cdp.sh")
POWERSHELL = Path("/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe")


class CleanupTransportTests(unittest.TestCase):
    def test_verified_results_and_bounded_transport_retries(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            native = root / "powershell"
            counter = root / "calls"
            native.write_text("""#!/usr/bin/env python3
import os, pathlib, sys
counter = pathlib.Path(os.environ['WCDP_TEST_COUNTER'])
n = int(counter.read_text()) + 1 if counter.exists() else 1
counter.write_text(str(n))
mode = os.environ['WCDP_TEST_MODE']
if mode == 'uncertain' or (mode == 'retry' and n == 1):
    print('native transport unavailable', file=sys.stderr)
    sys.exit(255)
if mode == 'no-marker': sys.exit(0)
code = int(mode) if mode.isdigit() else 0
print('WCDP_STOP_RESULT=' + str(code))
sys.exit(code)
""")
            native.chmod(0o755)
            for mode, success, calls in [("0", True, 1), ("20", False, 1),
                    ("21", False, 1), ("22", False, 1), ("23", False, 1),
                    ("retry", True, 2), ("uncertain", False, 3), ("no-marker", False, 3)]:
                with self.subTest(mode=mode):
                    counter.unlink(missing_ok=True)
                    result = subprocess.run(["bash", "-c",
                        'source "$1"; wcdp_stop_pid_checked 456', "test", str(HELPER)],
                        env={**os.environ, "WCDP_POWERSHELL": str(native),
                             "WCDP_TEST_MODE": mode, "WCDP_TEST_COUNTER": str(counter)},
                        capture_output=True, text=True, timeout=12)
                    self.assertEqual(result.returncode == 0, success, result.stderr)
                    self.assertEqual(int(counter.read_text()), calls)
                    self.assertIn("attempt 1 exit", result.stderr)
            counter.unlink()
            result = subprocess.run(["bash", "-c",
                'source "$1"; wcdp_stop_pid_checked "456;other"', "test", str(HELPER)],
                env={**os.environ, "WCDP_POWERSHELL": str(native)}, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(counter.exists(), "invalid PID reached native transport")
            # A trap must not fall back to taskkill after identity rejection.
            (root / "process-456.json").write_text("{}")
            unchecked = root / "unchecked"
            fallback = root / "taskkill"
            fallback.write_text(f"#!/bin/sh\ntouch '{unchecked}'\n")
            fallback.chmod(0o755)
            subprocess.run(["bash", "-c", 'source "$1"; WCDP_DIR_WSL="$2"; wcdp_stop_pid 456',
                            "test", str(HELPER), str(root)],
                           env={**os.environ, "WCDP_POWERSHELL": str(native), "WCDP_TASKKILL": str(fallback),
                                "WCDP_TEST_MODE": "21", "WCDP_TEST_COUNTER": str(counter)},
                           capture_output=True, check=True)
            self.assertEqual(counter.read_text(), "1")
            self.assertFalse(unchecked.exists(), "trap bypassed ownership rejection")


@unittest.skipUnless(POWERSHELL.exists() and shutil.which("wslpath"), "requires Windows PowerShell interop")
class NativeCleanupPolicyTests(unittest.TestCase):
    def test_owned_identity_exit_and_failure_policy(self):
        # Real PowerShell executes the policy with process/taskkill stubs. No
        # browser or user process is created, terminated or otherwise changed.
        with tempfile.TemporaryDirectory(prefix="gosx-wcdp-test-", dir="/mnt/c/Temp") as temporary:
            native_root = subprocess.check_output(["wslpath", "-w", temporary], text=True).strip()
            subprocess.run(["bash", "-c", 'source "$1"; WCDP_DIR_WSL="$2"; wcdp_init',
                            "test", str(HELPER), temporary], check=True)
            script = r'''
$ErrorActionPreference='Stop'
. '__ROOT__\stop-owned-process.ps1'
$script:lane = '__ROOT__'
$script:date = [DateTime]'2026-10-19T00:00:00Z'
$script:disposed=0
$script:fake = [pscustomobject]@{ Id=456; StartTime=$script:date; Path='C:\owned\chrome.exe'; Handle=[IntPtr]1; HasExited=$false }
$script:fake | Add-Member ScriptMethod WaitForExit { param($milliseconds) return $script:mode -notin @('never-exits','kill-fails') }
$script:fake | Add-Member ScriptMethod Dispose { $script:disposed++ }
function Get-CimInstance {
  param($ClassName, $Filter, $ErrorAction)
  if ($script:mode -eq 'absent') { return $null }
  if ($script:mode -eq 'query-fails') { throw 'native process query failed' }
  return [pscustomobject]@{ CommandLine=$script:command }
}
function Get-Process { param($Id, $ErrorAction) return $script:fake }
function Invoke-WcdpTaskkill {
  param($TargetId)
  $script:kills++
  $script:lastKilled=$TargetId
  return @{ code=if($script:mode -in @('kill-fails','already-exited')){128}else{0}; message='stub taskkill result' }
}
$rows = @()
foreach ($case in @('absent','live','foreign','reused','missing-owner','never-exits','kill-fails','already-exited','query-fails')) {
  $script:mode=$case; $script:kills=0
  $script:command='chrome.exe --user-data-dir=' + $script:lane + '\profile'
  if ($case -eq 'foreign') { $script:command='chrome.exe --user-data-dir=C:\user-profile' }
  $script:fake.StartTime=$script:date
  if ($case -eq 'reused') { $script:fake.StartTime=$script:date.AddSeconds(1) }
  $owner=@{id=456;started=$script:date.ToUniversalTime().Ticks.ToString();executable=$script:fake.Path;command=$script:command}
  $owner | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $script:lane 'process-456.json')
  if ($case -eq 'missing-owner') { Remove-Item -LiteralPath (Join-Path $script:lane 'process-456.json') }
  try { $result=Stop-WcdpOwnedProcess 456 5 } catch { $result=@{code=22;message="$_"} }
  $rows += @{case=$case;code=$result.code;kills=$script:kills;message=$result.message}
}
# Run the actual startup failure body with injected process/query operations;
# omit only its dependency loader, keeping our safe taskkill stub in scope.
function Start-Process { param($FilePath, $ArgumentList, [switch]$PassThru) return $script:fake }
$start=Get-Content -Raw -LiteralPath (Join-Path $script:lane 'start-chrome.ps1')
$start=$start.Replace('. (Join-Path $PSScriptRoot ''stop-owned-process.ps1'')', '')
$argsPath=Join-Path $script:lane 'args-test.txt'
'about:blank' | Set-Content -LiteralPath $argsPath
$script:mode='query-fails'; $script:kills=0; $before=$script:disposed; $thrown=$false
try { & ([ScriptBlock]::Create($start)) -ChromePath 'unused' -ArgsFile $argsPath } catch { $thrown=$true }
$rows += @{case='startup-failure';code=if($thrown){22}else{0};kills=$script:kills;message="disposed:$($script:disposed -gt $before);target:$script:lastKilled"}
ConvertTo-Json -Depth 3 -Compress -InputObject $rows
'''.replace("__ROOT__", native_root.replace("'", "''"))
            encoded = base64.b64encode(script.encode("utf-16le")).decode()
            result = subprocess.run([str(POWERSHELL), "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encoded],
                                    capture_output=True, text=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stderr)
            rows = {row["case"]: row for row in json.loads(result.stdout)}
            expected = {"absent": (0, 0), "live": (0, 1), "foreign": (21, 0),
                        "reused": (21, 0), "missing-owner": (21, 0), "never-exits": (20, 1),
                        "kill-fails": (23, 1), "already-exited": (0, 1), "query-fails": (22, 0),
                        "startup-failure": (22, 1)}
            for case, (code, kills) in expected.items():
                self.assertEqual((rows[case]["code"], rows[case]["kills"]), (code, kills), rows[case])
            self.assertIn("taskkill exit 128", rows["already-exited"]["message"])
            self.assertEqual(rows["startup-failure"]["message"], "disposed:True;target:456")


if __name__ == "__main__":
    unittest.main()
