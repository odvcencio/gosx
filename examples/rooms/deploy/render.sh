#!/usr/bin/env bash
# Render the runtime manifests for one image digest. Never contacts the
# Kubernetes API (KUBECONFIG=/dev/null).
set -euo pipefail

image=${1:?Usage: bash render.sh harbor.draco.quest/orchard/gosx-rooms@sha256:DIGEST}
if [[ ! "$image" =~ ^harbor\.draco\.quest/orchard/gosx-rooms@sha256:[a-f0-9]{64}$ ]]; then
  echo 'Expected the gosx-rooms Harbor image with a SHA-256 digest.' >&2
  exit 1
fi

source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
render_dir=$(mktemp -d)
trap 'rm -rf -- "$render_dir"' EXIT
cp "$source_dir/k8s.yaml" "$render_dir/k8s.yaml"
cat > "$render_dir/kustomization.yaml" <<KUST
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - k8s.yaml
images:
  - name: harbor.draco.quest/orchard/gosx-rooms
    newName: harbor.draco.quest/orchard/gosx-rooms
    digest: ${image##*@}
KUST
KUBECONFIG=/dev/null kubectl kustomize "$render_dir"
