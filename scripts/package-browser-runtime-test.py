#!/usr/bin/env python3
"""Release bundle contract tests using tiny fixture payloads."""

import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

MODULE_PATH = Path(__file__).with_name("package-browser-runtime.py")
spec = importlib.util.spec_from_file_location("browser_runtime_package", MODULE_PATH)
package = importlib.util.module_from_spec(spec)
spec.loader.exec_module(package)


class BundleTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / "source"
        self.wasm = self.root / "wasm"
        self.wasm.mkdir()
        js = self.source / "client/js"
        (js / "bootstrap-src").mkdir(parents=True)
        (js / "vendor").mkdir()
        (js / "bootstrap-src/chunks.json").write_text(json.dumps({"chunks": [
            {"name": "bootstrap-runtime.js"},
            {"name": "bootstrap-feature-scene3d.js"},
        ]}))
        (js / "bootstrap-runtime.js").write_text("// runtime\n")
        (js / "bootstrap-feature-scene3d.js").write_text("// scene3d\n")
        (js / "vendor/hls.min.js").write_text("// vendor\n")
        for name in package.WASM_FILES:
            (self.wasm / name).write_bytes(name.encode())
        subprocess.run(["git", "init", "-q", str(self.source)], check=True)
        subprocess.run(["git", "-C", str(self.source), "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-q", "--allow-empty", "-m", "fixture"], check=True)
        self.commit = subprocess.check_output(["git", "-C", str(self.source), "rev-parse", "HEAD"], text=True).strip()
        self.archive = self.root / "bundle.tar.gz"

    def pack(self):
        package.pack(type("Args", (), {"source": str(self.source), "wasm_dir": str(self.wasm), "tag": "v1.2.3", "commit": self.commit, "output": str(self.archive)})())

    def test_complete_bundle_round_trip(self):
        self.pack()
        package.verify(type("Args", (), {"archive": str(self.archive), "tag": "v1.2.3", "commit": self.commit})())
        with tarfile.open(self.archive, "r:gz") as tar:
            names = {member.name for member in tar.getmembers()}
        self.assertEqual(names, {"manifest.json", "client/js/bootstrap-runtime.js", "client/js/bootstrap-feature-scene3d.js", "client/js/vendor/hls.min.js"} | {f"wasm/{name}" for name in package.WASM_FILES})

    def test_reject_missing_runtime_file(self):
        (self.wasm / "gosx-runtime-engine.wasm").unlink()
        with self.assertRaisesRegex(ValueError, "missing or nonregular runtime asset"):
            self.pack()
        self.assertFalse(self.archive.exists())

    def test_reject_wrong_commit(self):
        with self.assertRaisesRegex(ValueError, "does not match release commit"):
            package.pack(type("Args", (), {"source": str(self.source), "wasm_dir": str(self.wasm), "tag": "v1.2.3", "commit": "0" * 40, "output": str(self.archive)})())

    def test_reject_modified_content(self):
        self.pack()
        altered = self.root / "altered.tar.gz"
        with tarfile.open(self.archive, "r:gz") as original, tarfile.open(altered, "w:gz") as rewritten:
            for member in original.getmembers():
                data = original.extractfile(member).read()
                if member.name == "client/js/bootstrap-runtime.js":
                    data += b"changed"
                member.size = len(data)
                rewritten.addfile(member, io.BytesIO(data))
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            package.verify(type("Args", (), {"archive": str(altered), "tag": "v1.2.3", "commit": self.commit})())


if __name__ == "__main__":
    unittest.main()
