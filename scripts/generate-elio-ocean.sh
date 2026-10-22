#!/usr/bin/env bash
# Writes client/runtime/scene3d/webgpu-ocean-kernels.ts from an Elio checkout.
#
# usage: scripts/generate-elio-ocean.sh <elio-dir>
#
# The file is the output of `elio emit ts --set ocean --global
# __gosx_scene3d_elio_ocean`: one classic script that assigns a frozen object
# holding every ocean kernel's WGSL, GLSL, bindings and uniform layouts. Do not
# edit it by hand. The drift test (client/js/scene3d-elio-ocean-kernels.test.mjs)
# checks the embedded SHA-256 against the sources in the file; set ELIO_DIR to
# also have it regenerate and compare byte for byte.
set -euo pipefail

if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  echo "usage: $0 <elio-dir>" >&2
  exit 2
fi
elio_dir=$(cd "$1" && pwd)
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
out="$root/client/runtime/scene3d/webgpu-ocean-kernels.ts"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

(cd "$elio_dir" && GOWORK=off go run ./cmd/elio emit ts --set ocean --global __gosx_scene3d_elio_ocean) >"$tmp"
chmod 0644 "$tmp"
mv "$tmp" "$out"
trap - EXIT
echo "wrote $out"
