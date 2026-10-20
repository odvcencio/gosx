#!/usr/bin/env bash
# Called only by the governed release workflow, on a provisioned Linux runner.
set -euo pipefail

if [[ -z "${GOSX_ARTIFACT_SIGNING_ENDPOINT:-}" ||
      -z "${GOSX_ARTIFACT_SIGNING_ACCOUNT:-}" ||
      -z "${GOSX_ARTIFACT_SIGNING_PROFILE:-}" ]]; then
  echo "Azure Artifact Signing skipped: endpoint, account or profile is absent."
  exit 0
fi

artifact="$1"
version="$2"
signing_work_dir="$(mktemp -d)"
trap 'rm -rf "${signing_work_dir}"' EXIT
mkdir -p "${signing_work_dir}/stage"
cp "${artifact}" "${signing_work_dir}/stage/app.exe"

# The installer is a packaging smoke artifact, never published or installed.
# Its update key is a placeholder; shipping apps must use their own Ed25519 key.
python3 - "${signing_work_dir}/package.json" "${version}" <<'PY'
import base64
import json
import sys

with open(sys.argv[1], "w", encoding="utf-8") as config:
    json.dump({
        "app_id": "gosx-signing-smoke",
        "name": "GoSX Signing Smoke",
        "publisher": "GoSX",
        "version": sys.argv[2],
        "host_exe": "app.exe",
        "data_dir": "%LOCALAPPDATA%\\GoSX Signing Smoke",
        "update_public_key": base64.b64encode(bytes(32)).decode("ascii"),
        "download_page": "https://example.com/download",
        "notes": "Governed release signing check"
    }, config)
PY

GOWORK=off go run ./cmd/gosx desktop package \
  --input "${signing_work_dir}/stage" \
  --config "${signing_work_dir}/package.json" \
  --output "${signing_work_dir}/output" \
  --sign-provider azure-artifact-signing --verify-signatures

# Publish the verified, signed CLI PE in place of the unsigned build. The
# temporary Setup and its uninstaller exercise the complete packaging path.
python3 - "${signing_work_dir}/output/GoSX-Signing-Smoke-${version}-portable.zip" "${artifact}" <<'PY'
import sys
import zipfile

with zipfile.ZipFile(sys.argv[1]) as portable:
    signed = portable.read("app.exe")
with open(sys.argv[2], "wb") as artifact:
    artifact.write(signed)
PY

echo "Azure Artifact Signing: Windows CLI, staged PE, uninstaller and Setup verified."
