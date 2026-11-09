#!/usr/bin/env bash
# Build, push and deploy the room server, following the Dominoes flow: build
# on this workstation (never on the cluster host), push git-<commit> to
# Harbor, then roll the Deployment to the digest Harbor acknowledged.
#
#   bash examples/rooms/deploy/release.sh            # build, push, apply
#   bash examples/rooms/deploy/release.sh -n         # build and print only
set -euo pipefail

context=${KUBE_CONTEXT:-default}
repo=harbor.draco.quest/orchard/gosx-rooms
apply=1
[[ ${1:-} == -n ]] && apply=0

root=$(git -C "$(dirname -- "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)
cd "$root"
if [[ -n $(git status --porcelain -- . ':!examples/rooms/deploy/*.md') ]]; then
  echo 'Commit first: the image is tagged with the commit it was built from.' >&2
  exit 1
fi
rev=$(git rev-parse HEAD)
tag=$repo:git-$rev

docker buildx build --load --file examples/rooms/deploy/Dockerfile \
  --tag "$tag" --label "org.opencontainers.image.revision=$rev" .
(( apply )) || { echo "Built $tag (no push)"; exit 0; }

push=$(docker push "$tag")
printf '%s\n' "$push"
digest=$(printf '%s\n' "$push" | sed -nE 's/^[^ ]+: digest: (sha256:[0-9a-f]{64}) size: [0-9]+$/\1/p')
[[ $digest =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'Push did not report one digest.' >&2; exit 1; }
image=$repo@$digest

kubectl --context "$context" apply -f examples/rooms/deploy/namespace.yaml
bash examples/rooms/deploy/render.sh "$image" | kubectl --context "$context" apply -f -
kubectl --context "$context" -n gosx-rooms rollout status deployment/gosx-rooms --timeout=180s
docker image rm "$tag" >/dev/null
echo "Deployed $image"
