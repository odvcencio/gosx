#!/usr/bin/env python3
"""Exercise the published CLI and module through the documented starter path."""

import argparse
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


def run(command, *, cwd, env, capture=False):
    print("+", " ".join(map(str, command)), flush=True)
    return subprocess.run(command, cwd=cwd, env=env, check=True, text=True,
                          stdout=subprocess.PIPE if capture else None).stdout


def serve_and_check(command, *, cwd, env, version, log):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    url = f"http://127.0.0.1:{port}"
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    server_env = env | {
        "PORT": str(port), "GOSX_LISTEN_ADDR": f"127.0.0.1:{port}",
        "PUBLIC_URL": url, "SESSION_SECRET": "local-smoke-secret-not-for-production",
    }
    print("+", " ".join(map(str, command)), flush=True)
    with log.open("w") as output:
        server = subprocess.Popen(command, cwd=cwd, env=server_env,
                                  stdout=output, stderr=subprocess.STDOUT,
                                  start_new_session=True)
        try:
            deadline = time.monotonic() + 120
            while True:
                if server.poll() is not None:
                    raise RuntimeError(f"server exited; see {log}")
                try:
                    with http.open(url + "/api/health", timeout=2) as response:
                        health = json.load(response)
                    break
                except (OSError, urllib.error.URLError):
                    if time.monotonic() >= deadline:
                        raise RuntimeError(f"server did not become ready; see {log}")
                    time.sleep(0.25)
            if health.get("ok") is not True or health.get("version") != version[1:]:
                raise RuntimeError(f"unexpected health response: {health}")
            with http.open(url + "/", timeout=10) as response:
                page = response.read().decode("utf-8")
            if "Starter form" not in page:
                raise RuntimeError("starter page did not contain the expected form")
            print(f"  health and starter page passed at {url}", flush=True)
        finally:
            # go run starts a child executable; both belong to this new session.
            try:
                os.killpg(server.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                server.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(server.pid, signal.SIGKILL)
                server.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="published stable tag, e.g. v0.59.1")
    parser.add_argument("--cli", type=Path, help="published CLI binary; default: go install the tag")
    args = parser.parse_args()
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", args.version):
        parser.error("version must be a stable tag such as v0.59.1")
    if args.cli and (not args.cli.is_absolute() or not os.access(args.cli, os.X_OK)):
        parser.error("--cli must be an absolute path to an executable")

    work = Path(tempfile.mkdtemp(prefix="gosx-published-starter."))
    print(f"Evidence directory: {work}", flush=True)
    env = os.environ.copy()
    env.update({"GOWORK": "off", "GOPROXY": "https://proxy.golang.org",
                "GOSUMDB": "sum.golang.org", "GOPRIVATE": "none",
                "GONOPROXY": "none", "GONOSUMDB": "none",
                "GOSX_RUNTIME_CACHE": str(work / "runtime-cache")})
    for key in ("GOSX_SKIP_VERSION_CHECK", "GOSX_RUNTIME_RELEASE_REPO"):
        env.pop(key, None)
    if args.cli:
        cli = args.cli
    else:
        bindir = work / "bin"
        bindir.mkdir()
        env["GOBIN"] = str(bindir)
        run(["go", "install", f"m31labs.dev/gosx/cmd/gosx@{args.version}"], cwd=work, env=env)
        cli = bindir / "gosx"
    if run([cli, "version"], cwd=work, env=env, capture=True).strip() != f"gosx {args.version}":
        raise RuntimeError("CLI reports the wrong release")

    app = work / "app"
    run([cli, "init", app, "--module", "example.com/published-starter"], cwd=work, env=env)
    module = json.loads(run(["go", "list", "-m", "-json", "m31labs.dev/gosx"], cwd=app, env=env, capture=True))
    if module.get("Version") != args.version or module.get("Replace") or not module.get("Sum"):
        raise RuntimeError(f"starter did not resolve the published module: {module}")
    print(f"  published module {module['Version']} {module['Sum']}", flush=True)
    run([cli, "check", app / "app/page.gsx"], cwd=app, env=env)
    serve_and_check(["go", "run", "."], cwd=app, env=env, version=args.version, log=work / "development.log")
    run([cli, "build", "--prod", "."], cwd=app, env=env)
    serve_and_check([app / "dist/run.sh"], cwd=app / "dist", env=env | {"GOSX_ENV": "production"},
                    version=args.version, log=work / "production.log")
    print(f"Published starter passed; evidence: {work}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Published starter failed: {error}", file=sys.stderr)
        sys.exit(1)
