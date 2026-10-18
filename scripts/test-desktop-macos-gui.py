#!/usr/bin/env python3
"""Run the desktop GUI contracts through LaunchServices, failing on skips."""

import argparse
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import uuid


TESTS = (
    "TestDarwinFilePanelsConfigureAndCancel",
    "TestDarwinFinderLaunchServicesDelivery",
    "TestDarwinWebMessagesRoundTrip",
    "TestDarwinWebFileInputSelection",
    "TestDarwinWebFileInputCancelAndReselect",
    "TestDarwinWebDirectoryInputSelection",
    "TestDarwinJavaScriptDialogs",
)


def command(args, log, **kwargs):
    with log.open("x") as output:
        output.write("$ " + repr(args) + "\n")
        output.flush()
        subprocess.run(args, stdout=output, stderr=subprocess.STDOUT,
                       check=True, **kwargs)


def stop_owned_executable(executable):
    """Never terminate another app: match this unique bundle's exact path."""
    processes = subprocess.check_output(
        ["/bin/ps", "-ww", "-axo", "pid=,comm="], text=True)
    for line in processes.splitlines():
        fields = line.strip().split(None, 1)
        if len(fields) == 2 and fields[1] == str(executable):
            try:
                os.kill(int(fields[0]), signal.SIGKILL)
            except ProcessLookupError:
                pass


def cleanup_bundle_storage(bundle_id):
    # The random ID is generated here; no shared WebKit or application data.
    library = Path.home() / "Library"
    for parent, name in (
        ("Caches", bundle_id),
        ("WebKit", bundle_id),
        ("Application Support", bundle_id),
        ("Preferences", bundle_id + ".plist"),
        ("Saved Application State", bundle_id + ".savedState"),
    ):
        path = library / parent / name
        if path.is_dir() and not path.is_symlink():
            shutil.rmtree(path)
        else:
            path.unlink(missing_ok=True)


def run_gui(repo, artifacts):
    artifacts.mkdir(parents=True, exist_ok=True)
    logs = Path(tempfile.mkdtemp(prefix="run-", dir=artifacts))
    report = {"platform": platform.platform(), "tests": [], "passed": False}
    env = dict(os.environ, CGO_ENABLED="0", GOWORK="off", GOOS="darwin",
               GOARCH="arm64" if platform.machine() == "arm64" else "amd64")
    try:
        with tempfile.TemporaryDirectory(prefix="gosx-native-gui-") as temporary:
            work = Path(temporary).resolve()
            binary = work / "desktop.test"
            command(["go", "test", "-c", "-o", str(binary), "./desktop"],
                    logs / "build.log", cwd=repo, env=env, timeout=300)
            for test in TESTS:
                bundle_id = "dev.gosx.native-ci." + uuid.uuid4().hex
                app = work / (bundle_id + ".app")
                macos = app / "Contents" / "MacOS"
                macos.mkdir(parents=True)
                executable = macos / "gosx-desktop-tests"
                shutil.copy2(binary, executable)
                with (app / "Contents" / "Info.plist").open("wb") as plist:
                    # No document types, URL handlers or microphone purpose key.
                    plistlib.dump({
                        "CFBundleExecutable": executable.name,
                        "CFBundleIdentifier": bundle_id,
                        "CFBundleName": "GoSX native CI",
                        "CFBundlePackageType": "APPL",
                        "CFBundleVersion": "1",
                        "LSUIElement": True,
                        "NSHighResolutionCapable": True,
                    }, plist)
                stdout = logs / (test + ".stdout.log")
                stderr = logs / (test + ".stderr.log")
                stdout.touch(exist_ok=False)
                stderr.touch(exist_ok=False)
                result = {"name": test, "bundle_id": bundle_id, "passed": False}
                report["tests"].append(result)
                try:
                    command(["/usr/bin/codesign", "--force", "--sign", "-", str(app)],
                            logs / (test + ".codesign.log"), timeout=30)
                    command(["/usr/bin/open", "-W", "-n", "--stdout", str(stdout),
                             "--stderr", str(stderr), str(app), "--args", "-test.v",
                             "-test.run", "^" + test + "$", "-test.timeout=45s"],
                            logs / (test + ".launch.log"), timeout=90)
                    text = stdout.read_text(errors="replace")
                    passed = re.search(r"^--- PASS: " + re.escape(test) + r" \(",
                                       text, re.MULTILINE)
                    complete = re.search(r"^PASS$", text, re.MULTILINE)
                    failed = re.search(r"^(?:--- (?:FAIL|SKIP):|FAIL\b|panic:)",
                                       text, re.MULTILINE)
                    if not passed or not complete or failed:
                        raise RuntimeError(f"{test} did not explicitly pass; see {stdout}")
                    result["passed"] = True
                    print(f"PASS {test}", flush=True)
                finally:
                    stop_owned_executable(executable)
                    cleanup_bundle_storage(bundle_id)
            report["passed"] = True
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        (logs / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        print(f"Native GUI reports: {logs}", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parent.parent)
    parser.add_argument("--artifacts", type=Path, required=True)
    args = parser.parse_args()
    if sys.platform != "darwin":
        parser.error("the native GUI proof requires macOS and an Aqua login session")
    run_gui(args.repo.resolve(), args.artifacts.resolve())


if __name__ == "__main__":
    main()
