#!/usr/bin/env python3
"""Package and verify the complete, versioned GoSX browser runtime release asset."""

import argparse
import gzip
import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import sys
import tarfile

WASM_FILES = (
    "gosx-runtime-core.wasm",
    "gosx-runtime-engine.wasm",
    "gosx-runtime-collab.wasm",
    "gosx-runtime.wasm",
    "gosx-runtime-islands.wasm",
    "wasm_exec.js",
    "standard-go-wasm_exec.js",
)
EXTRA_JS = ("vendor/hls.min.js",)
TAG_PATTERN = re.compile(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\Z")
SHA_PATTERN = re.compile(r"[0-9a-f]{40}\Z")
SCHEMA = "gosx.browser-runtime.v1"


def digest(data):
    return hashlib.sha256(data).hexdigest()


def read_regular(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"missing or nonregular runtime asset: {path}")
    return path.read_bytes()


def browser_files(source, wasm_dir):
    chunks = json.loads(read_regular(source / "client/js/bootstrap-src/chunks.json"))
    names = [entry["name"] for entry in chunks["chunks"]]
    if not names or len(names) != len(set(names)):
        raise ValueError("empty or duplicate browser chunk names")
    if any(not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*\.js", name) for name in names):
        raise ValueError("unsafe browser chunk name")
    files = {f"wasm/{name}": read_regular(wasm_dir / name) for name in WASM_FILES}
    for name in (*names, *EXTRA_JS):
        files[f"client/js/{name}"] = read_regular(source / "client/js" / name)
    return files


def tar_entry(tar, name, data):
    info = tarfile.TarInfo(name)
    info.size = len(data)
    info.mode = 0o644
    info.mtime = 0
    info.uid = info.gid = 0
    info.uname = info.gname = ""
    tar.addfile(info, io.BytesIO(data))


def pack(args):
    if not TAG_PATTERN.fullmatch(args.tag) or not SHA_PATTERN.fullmatch(args.commit):
        raise ValueError("expected stable vMAJOR.MINOR.PATCH tag and full commit SHA")
    source = Path(args.source).resolve(strict=True)
    wasm_dir = Path(args.wasm_dir).resolve(strict=True)
    actual = subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip()
    if actual != args.commit:
        raise ValueError(f"source checkout {actual} does not match release commit {args.commit}")
    files = browser_files(source, wasm_dir)
    manifest = {
        "schema": SCHEMA,
        "tag": args.tag,
        "commit": args.commit,
        "repository": "odvcencio/gosx",
        "files": {name: {"sha256": digest(data), "bytes": len(data)} for name, data in sorted(files.items())},
    }
    manifest_data = (json.dumps(manifest, sort_keys=True, indent=2) + "\n").encode()
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    if output.exists() or output.is_symlink():
        raise ValueError(f"refusing to replace runtime bundle: {output}")
    try:
        with output.open("xb") as raw, gzip.GzipFile(fileobj=raw, mode="wb", mtime=0, filename="") as zipped:
            with tarfile.open(fileobj=zipped, mode="w") as tar:
                for name, data in sorted(files.items()):
                    tar_entry(tar, name, data)
                tar_entry(tar, "manifest.json", manifest_data)
        verify(argparse.Namespace(archive=str(output), tag=args.tag, commit=args.commit))
    except Exception:
        output.unlink(missing_ok=True)
        raise
    print(f"{output}: {len(files)} runtime files, sha256 {digest(output.read_bytes())}")


def verify(args):
    archive = Path(args.archive)
    with tarfile.open(archive, "r:gz") as tar:
        members = tar.getmembers()
        names = [member.name for member in members]
        if len(names) != len(set(names)) or "manifest.json" not in names:
            raise ValueError("runtime bundle has duplicate names or no manifest")
        if any(not member.isfile() or member.issym() or member.islnk() or member.name.startswith("/") or ".." in Path(member.name).parts for member in members):
            raise ValueError("runtime bundle contains an unsafe member")
        manifest = json.load(tar.extractfile("manifest.json"))
        if manifest.get("schema") != SCHEMA or manifest.get("tag") != args.tag or manifest.get("commit") != args.commit:
            raise ValueError("runtime bundle release identity mismatch")
        expected = set(manifest.get("files", {})) | {"manifest.json"}
        if set(names) != expected or not set(f"wasm/{name}" for name in WASM_FILES) <= expected:
            raise ValueError("runtime bundle file set mismatch")
        if not any(name.startswith("client/js/bootstrap-feature-scene3d") for name in expected):
            raise ValueError("runtime bundle is missing Scene3D chunks")
        for name, entry in manifest["files"].items():
            data = tar.extractfile(name).read()
            if len(data) != entry["bytes"] or digest(data) != entry["sha256"]:
                raise ValueError(f"runtime bundle checksum mismatch: {name}")
    print(f"verified {archive}: {len(manifest['files'])} files for {args.tag} @ {args.commit}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    p = sub.add_parser("pack")
    p.add_argument("--source", required=True)
    p.add_argument("--wasm-dir", required=True)
    p.add_argument("--tag", required=True)
    p.add_argument("--commit", required=True)
    p.add_argument("--output", required=True)
    v = sub.add_parser("verify")
    v.add_argument("--archive", required=True)
    v.add_argument("--tag", required=True)
    v.add_argument("--commit", required=True)
    args = parser.parse_args()
    try:
        (pack if args.action == "pack" else verify)(args)
    except (ValueError, OSError, KeyError, json.JSONDecodeError, tarfile.TarError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"package-browser-runtime: {error}\n")


if __name__ == "__main__":
    main()
