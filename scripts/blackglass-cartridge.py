#!/usr/bin/env python3
"""Stage and verify an optional deployment cartridge, outside Git/public assets."""
import argparse
import hashlib
import json
from pathlib import Path
import urllib.request

MAX_BYTES = 8 * 1024 * 1024


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True)
    parser.add_argument("--source")
    parser.add_argument("--sha256")
    parser.add_argument("--verify-base")
    args = parser.parse_args()
    root = Path(args.root).resolve()
    target = root / "runtime-assets" / "blackglass-cartridge.gbc"
    receipt = target.with_suffix(".json")
    if args.source:
        source = Path(args.source).resolve()
        repo = Path(__file__).resolve().parent.parent
        if source.is_relative_to(repo) or source == target:
            parser.error("cartridge source must remain outside the repository and dist")
        if not args.sha256 or len(args.sha256) != 64:
            parser.error("--sha256 must pin the deployment input")
        with source.open("rb") as stream:
            data = stream.read(MAX_BYTES + 1)
        if len(data) < 32768 or len(data) > MAX_BYTES or len(data) % 16384:
            parser.error("expected a GB/GBC cartridge between 32 KiB and 8 MiB")
        checksum = 0
        for value in data[0x134:0x14d]:
            checksum = (checksum - value - 1) & 255
        if checksum != data[0x14d]:
            parser.error("cartridge header checksum is invalid")
        digest = hashlib.sha256(data).hexdigest()
        if digest != args.sha256:
            parser.error("cartridge bytes differ from the pinned SHA-256")
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        target.chmod(0o644)
        receipt.write_text(json.dumps({"sha256": digest, "bytes": len(data)}, indent=2) + "\n")
        print(f"Blackglass cartridge staged: {len(data)} bytes, SHA-256 {digest}")
    elif not args.verify_base:
        parser.error("choose --source or --verify-base")
    if args.verify_base:
        expected = json.loads(receipt.read_text())
        base = args.verify_base.rstrip("/")
        def fetch(url):
            request = urllib.request.Request(url, headers={"Cache-Control": "no-cache", "User-Agent": "GoSX-Docs-Deployment-Verify/1.0"})
            with urllib.request.urlopen(request, timeout=15) as response:
                return response.read(MAX_BYTES + 1)
        manifest = json.loads(fetch(base + "/api/blackglass-cartridge"))
        if not manifest.get("available") or any(manifest.get(k) != v for k, v in expected.items()):
            parser.error("served cartridge manifest differs from the staged receipt")
        url = manifest.get("url", "")
        required_url = "/api/blackglass-cartridge/rom?sha256=" + expected["sha256"]
        if url != required_url:
            parser.error("served cartridge URL is not the expected same-origin revision")
        data = fetch(base + url)
        if len(data) != expected["bytes"] or hashlib.sha256(data).hexdigest() != expected["sha256"]:
            parser.error("served cartridge bytes differ from the staged receipt")
        print("Blackglass deployment cartridge verified against its staged receipt")


if __name__ == "__main__":
    main()
