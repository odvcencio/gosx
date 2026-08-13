#!/usr/bin/env sh
set -eu

registry="harbor.draco.quest/orchard/gosx-docs"
namespace="draco-quest"
deployment="gosx-docs"
container="gosx-docs"
init_container="stage-isr"
secret="gosx-docs"
public_url="https://gosx.m31labs.dev"
go_cmd="${GO:-go}"
docker_cmd="${DOCKER:-docker}"
kubectl_cmd="${KUBECTL:-kubectl}"
tinygo_cmd="${TINYGO:-tinygo}"
expected_tinygo="0.41.1"
expected_tinygo_go="go1.25.5"
tinygo_go_root="${GOSX_TINYGO_GOROOT:-}"
curl_cmd="${CURL:-curl}"

require_command() {
	command_name="$1"
	if ! command -v "$command_name" >/dev/null 2>&1; then
		echo "gosx docs deploy: required command is unavailable: ${command_name}" >&2
		exit 1
	fi
}

worktree_changes() {
	git status --porcelain --untracked-files=normal -- . ':(exclude)examples/gosx-docs/dist'
}

for command_name in git "$go_cmd" "$docker_cmd" "$kubectl_cmd" "$tinygo_cmd" "$curl_cmd" \
	base64 date dirname find grep jq mktemp sed tail tr; do
	require_command "$command_name"
done

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

host_goos="$($go_cmd env GOHOSTOS)"
host_goarch="$($go_cmd env GOHOSTARCH)"
if [ "$host_goos/$host_goarch" != "linux/amd64" ]; then
	echo "gosx docs deploy: this deployment currently requires a linux/amd64 builder; got ${host_goos}/${host_goarch}" >&2
	exit 1
fi
docker_platform="$($docker_cmd version --format '{{.Server.Os}}/{{.Server.Arch}}' 2>/dev/null || true)"
if [ "$docker_platform" != "linux/amd64" ]; then
	echo "gosx docs deploy: the Docker builder must be linux/amd64; got ${docker_platform:-unavailable}" >&2
	exit 1
fi

if [ -n "$(worktree_changes)" ]; then
	echo "gosx docs deploy: refusing a dirty worktree" >&2
	exit 1
fi

tinygo_path="$(command -v "$tinygo_cmd" 2>/dev/null || true)"
tinygo_version="$($tinygo_path version 2>/dev/null || true)"
case "$tinygo_version" in
	*"tinygo version ${expected_tinygo}"*) ;;
	*)
		echo "gosx docs deploy: TinyGo ${expected_tinygo} is required; got ${tinygo_version:-unavailable}" >&2
		exit 1
		;;
esac
if [ -z "$tinygo_go_root" ] || [ ! -x "$tinygo_go_root/bin/go" ]; then
	echo "gosx docs deploy: GOSX_TINYGO_GOROOT must name the ${expected_tinygo_go} compatibility toolchain" >&2
	exit 1
fi
compat_go_version="$(GOTOOLCHAIN=local "$tinygo_go_root/bin/go" version 2>/dev/null || true)"
case "$compat_go_version" in
	*"${expected_tinygo_go} "*) ;;
	*)
		echo "gosx docs deploy: expected ${expected_tinygo_go} at ${tinygo_go_root}; got ${compat_go_version:-unavailable}" >&2
		exit 1
		;;
esac

revision="$(git rev-parse HEAD)"
built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
tag="${GOSX_DOCS_TAG:-git-${revision}}"
image="${registry}:${tag}"

"$kubectl_cmd" get -n "$namespace" "deployment/${deployment}" >/dev/null
previous_deployment_json="$($kubectl_cmd get -n "$namespace" "deployment/${deployment}" -o json)"
previous_template="$(printf '%s\n' "$previous_deployment_json" | jq -c '.spec.template')"
previous_image="$(printf '%s\n' "$previous_deployment_json" | jq -r --arg container "$container" '.spec.template.spec.containers[] | select(.name == $container) | .image')"
session_secret_b64="$($kubectl_cmd get -n "$namespace" "secret/${secret}" -o json | jq -er '.data["session-secret"]')"
session_secret="$(printf '%s' "$session_secret_b64" | base64 --decode)"
session_secret_b64=""
if [ -z "$session_secret" ]; then
	echo "gosx docs deploy: secret/${secret} has an empty session-secret" >&2
	exit 1
fi

dist_dir="${repo_root}/examples/gosx-docs/dist"
if [ "$dist_dir" != "${repo_root}/examples/gosx-docs/dist" ]; then
	echo "gosx docs deploy: refusing unexpected dist path ${dist_dir}" >&2
	exit 1
fi
if [ -L "$dist_dir" ]; then
	echo "gosx docs deploy: refusing symlinked dist path ${dist_dir}" >&2
	exit 1
fi
if [ ! -f "${repo_root}/examples/gosx-docs/main.go" ] || [ ! -f "${repo_root}/examples/gosx-docs/Dockerfile.runtime" ]; then
	echo "gosx docs deploy: expected docs application markers are missing" >&2
	exit 1
fi
if [ -n "$(git ls-files -- examples/gosx-docs/dist)" ]; then
	echo "gosx docs deploy: refusing to clear tracked files under examples/gosx-docs/dist" >&2
	exit 1
fi
gosx_cli="$(mktemp)"
manifest=""
release_template=""
rollback_armed=0
cleanup() {
	session_secret=""
	if [ -n "$manifest" ]; then
		rm -f "$manifest"
	fi
	rm -f "$gosx_cli"
}
on_exit() {
	exit_status=$?
	trap - EXIT INT TERM
	if [ "$exit_status" -ne 0 ] && [ "${rollback_armed:-0}" -eq 1 ]; then
		if ! rollback; then
			echo "gosx docs deploy: automatic rollback did not restore the captured template" >&2
		fi
	fi
	cleanup
	exit "$exit_status"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [ -d "$dist_dir" ]; then
	find "$dist_dir" -mindepth 1 -delete
fi
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 "$go_cmd" build -o "$gosx_cli" ./cmd/gosx
framework_version="$("$gosx_cli" version | sed -n 's/^gosx \(v[0-9][0-9A-Za-z.+-]*\)$/\1/p')"
if [ -z "$framework_version" ]; then
	echo "gosx docs deploy: could not determine the framework version from the built CLI" >&2
	exit 1
fi
tinygo_dir="$(dirname "$tinygo_path")"
PATH="${tinygo_dir}:${PATH}" \
	GOSX_TINYGO_GOROOT="$tinygo_go_root" \
	PUBLIC_URL="$public_url" \
	GOSX_DOCS_REVISION="$revision" \
	GOSX_DOCS_BUILT_AT="$built_at" \
	SESSION_SECRET="$session_secret" \
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
	"$gosx_cli" build --prod ./examples/gosx-docs
session_secret=""
for required_output in build.json run.sh server/app; do
	if [ ! -s "${dist_dir}/${required_output}" ]; then
		echo "gosx docs deploy: production build did not create dist/${required_output}" >&2
		exit 1
	fi
done
if [ -n "$(worktree_changes)" ]; then
	echo "gosx docs deploy: production build changed the worktree" >&2
	exit 1
fi

"$docker_cmd" build \
	--platform linux/amd64 \
	--file examples/gosx-docs/Dockerfile.runtime \
	--label org.opencontainers.image.revision="$revision" \
	--label org.opencontainers.image.created="$built_at" \
	--tag "$image" \
	examples/gosx-docs
built_platform="$($docker_cmd image inspect "$image" --format '{{.Os}}/{{.Architecture}}' 2>/dev/null || true)"
if [ "$built_platform" != "linux/amd64" ]; then
	echo "gosx docs deploy: built image must be linux/amd64; got ${built_platform:-unavailable}" >&2
	exit 1
fi

push_output="$("$docker_cmd" push "$image" 2>&1)"
printf '%s\n' "$push_output"
push_digest="$(printf '%s\n' "$push_output" | sed -n 's/.*digest: sha256:\([0-9a-f]\{64\}\).*/\1/p' | tail -n 1)"
digest="$("$docker_cmd" buildx imagetools inspect "$image" --format '{{json .Manifest.Digest}}' 2>/dev/null | tr -d '"' | sed -n 's/^sha256://p')"
case "$digest" in
	*[!0-9a-f]*) digest="" ;;
esac
if [ "${#digest}" -ne 64 ]; then
	echo "gosx docs deploy: registry inspection did not return a sha256 digest for ${image}" >&2
	exit 1
fi
if [ -n "$push_digest" ] && [ "$push_digest" != "$digest" ]; then
	echo "gosx docs deploy: pushed digest sha256:${push_digest} does not match registry sha256:${digest}" >&2
	exit 1
fi
immutable_image="${registry}@sha256:${digest}"

manifest="$(mktemp)"

sed \
	-e "s/__IMAGE_DIGEST__/${digest}/g" \
	-e "s/__GIT_REVISION__/${revision}/g" \
	-e "s/__BUILT_AT__/${built_at}/g" \
	deploy/gosx-docs.yaml >"$manifest"
if grep -q '__[A-Z][A-Z_]*__' "$manifest"; then
	echo "gosx docs deploy: rendered manifest still contains placeholders" >&2
	exit 1
fi

rollback() {
	if [ "${rollback_armed:-0}" -ne 1 ]; then
		return
	fi
	rollback_armed=0
	echo "gosx docs deploy: rolling back failed rollout" >&2
	current_deployment_json="$($kubectl_cmd get -n "$namespace" "deployment/${deployment}" -o json 2>/dev/null || true)"
	if [ -z "$current_deployment_json" ] || [ -z "$release_template" ]; then
		echo "gosx docs deploy: cannot verify the failed release before rollback" >&2
		return 1
	fi
	if ! printf '%s\n' "$current_deployment_json" | jq -e --argjson expected "$release_template" '.spec.template == $expected' >/dev/null; then
		echo "gosx docs deploy: Deployment template changed concurrently; refusing to overwrite it" >&2
		return 1
	fi
	rollback_generation="$(printf '%s\n' "$current_deployment_json" | jq -r '.metadata.generation')"
	case "$rollback_generation" in
		''|*[!0-9]*)
			echo "gosx docs deploy: current Deployment generation is invalid" >&2
			return 1
			;;
	esac
	rollback_patch="$(jq -cn \
		--argjson generation "$rollback_generation" \
		--argjson template "$previous_template" \
		'[
			{"op":"test","path":"/metadata/generation","value":$generation},
			{"op":"replace","path":"/spec/template","value":$template}
		]')"
	if ! "$kubectl_cmd" patch -n "$namespace" "deployment/${deployment}" --type=json -p "$rollback_patch" >/dev/null; then
		echo "gosx docs deploy: exact rollback patch was rejected (possibly a concurrent deployment)" >&2
		return 1
	fi
	if ! "$kubectl_cmd" rollout status -n "$namespace" "deployment/${deployment}" --timeout=5m; then
		return 1
	fi
	rolled_back_json="$($kubectl_cmd get -n "$namespace" "deployment/${deployment}" -o json 2>/dev/null || true)"
	if ! printf '%s\n' "$rolled_back_json" | jq -e --argjson expected "$previous_template" '.spec.template == $expected' >/dev/null; then
		rolled_back_image="$(printf '%s\n' "$rolled_back_json" | jq -r --arg container "$container" '.spec.template.spec.containers[]? | select(.name == $container) | .image' 2>/dev/null || true)"
		echo "gosx docs deploy: rollback image is ${rolled_back_image:-unavailable}; expected ${previous_image}" >&2
		return 1
	fi
	if ! "$curl_cmd" --fail --silent --show-error --connect-timeout 10 --max-time 30 "${public_url}/healthz" >/dev/null; then
		return 1
	fi
	return 0
}

dry_run_json="$($kubectl_cmd apply --server-side --field-manager=gosx-docs-deploy --force-conflicts \
	--dry-run=server -f "$manifest" -o json)"
release_template="$(printf '%s\n' "$dry_run_json" | jq -c '
	if .kind == "List" then .items[] else . end
	| select(.kind == "Deployment")
	| .spec.template')"
if [ -z "$release_template" ]; then
	echo "gosx docs deploy: server dry-run did not return the Deployment template" >&2
	exit 1
fi
set +e
"$kubectl_cmd" diff --server-side --field-manager=gosx-docs-deploy --force-conflicts -f "$manifest"
diff_status=$?
set -e
if [ "$diff_status" -gt 1 ]; then
	echo "gosx docs deploy: kubectl diff failed with status ${diff_status}" >&2
	exit "$diff_status"
fi
rollback_armed=1
"$kubectl_cmd" apply --server-side --field-manager=gosx-docs-deploy --force-conflicts -f "$manifest"
"$kubectl_cmd" rollout status -n "$namespace" "deployment/${deployment}" --timeout=5m

deployment_json="$($kubectl_cmd get -n "$namespace" "deployment/${deployment}" -o json)"
template_image="$(printf '%s\n' "$deployment_json" | jq -r --arg container "$container" '.spec.template.spec.containers[] | select(.name == $container) | .image')"
template_init_image="$(printf '%s\n' "$deployment_json" | jq -r --arg container "$init_container" '.spec.template.spec.initContainers[] | select(.name == $container) | .image')"
template_revision="$(printf '%s\n' "$deployment_json" | jq -r --arg container "$container" '.spec.template.spec.containers[] | select(.name == $container) | .env[] | select(.name == "GOSX_DOCS_REVISION") | .value')"
template_built_at="$(printf '%s\n' "$deployment_json" | jq -r --arg container "$container" '.spec.template.spec.containers[] | select(.name == $container) | .env[] | select(.name == "GOSX_DOCS_BUILT_AT") | .value')"
if [ "$template_image" != "$immutable_image" ] || [ "$template_init_image" != "$immutable_image" ] || \
	[ "$template_revision" != "$revision" ] || [ "$template_built_at" != "$built_at" ]; then
	echo "gosx docs deploy: Deployment template identity does not match the rendered release" >&2
	echo "  image: ${template_image:-unavailable}" >&2
	echo "  init image: ${template_init_image:-unavailable}" >&2
	echo "  revision: ${template_revision:-unavailable}" >&2
	echo "  built at: ${template_built_at:-unavailable}" >&2
	exit 1
fi

desired_replicas="$(printf '%s\n' "$deployment_json" | jq -r '.spec.replicas // 1')"
identity_ok=0
identity_attempt=0
while [ "$identity_attempt" -lt 30 ]; do
	identity_attempt=$((identity_attempt + 1))
	pods_json="$($kubectl_cmd get pods -n "$namespace" -l "app=${deployment}" -o json)"
	ready_count="$(printf '%s\n' "$pods_json" | jq '[.items[] | select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] | length')"
	bad_ready="$(printf '%s\n' "$pods_json" | jq -r \
		--arg container "$container" \
		--arg init "$init_container" \
		--arg image "$immutable_image" \
		--arg digest "sha256:${digest}" '
		[.items[]
		 | select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))
		 | .metadata.name as $pod
		 | ([.spec.containers[]? | select(.name == $container)][0] // {image:""}) as $spec
		 | ([.status.containerStatuses[]? | select(.name == $container)][0] // {imageID:""}) as $status
		 | ([.spec.initContainers[]? | select(.name == $init)][0] // {image:""}) as $initSpec
		 | ([.status.initContainerStatuses[]? | select(.name == $init)][0] // {imageID:""}) as $initStatus
		 | select(
			$spec.image != $image
			or (($status.imageID | endswith($digest)) | not)
			or $initSpec.image != $image
			or (($initStatus.imageID | endswith($digest)) | not)
		 )
		 | "\($pod) spec=\($spec.image) imageID=\($status.imageID) initImageID=\($initStatus.imageID)"]
		 | .[]?')"
	if [ "$ready_count" -eq "$desired_replicas" ] && [ -z "$bad_ready" ]; then
		identity_ok=1
		break
	fi
	sleep 2
done
if [ "$identity_ok" -ne 1 ]; then
	echo "gosx docs deploy: every ready pod must run ${immutable_image}; ready=${ready_count}/${desired_replicas}" >&2
	if [ -n "$bad_ready" ]; then
		printf '%s\n' "$bad_ready" >&2
	fi
	exit 1
fi

GOSX_DOCS_EXPECT_FRAMEWORK_VERSION="$framework_version" \
	GOSX_DOCS_EXPECT_REVISION="$revision" \
	GOSX_DOCS_EXPECT_BUILT_AT="$built_at" \
	GOSX_DOCS_EXPECT_PUBLIC_URL="$public_url" \
	scripts/smoke-gosx-docs.sh "$public_url"
rollback_armed=0
printf '%s\n' "gosx docs deploy: ${immutable_image} (${framework_version}, ${revision}) is live at ${public_url}"
