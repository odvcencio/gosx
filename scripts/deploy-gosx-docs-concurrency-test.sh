#!/usr/bin/env sh
set -eu

for command_name in dirname jq; do
	if ! command -v "$command_name" >/dev/null 2>&1; then
		echo "deploy concurrency test: required command is unavailable: ${command_name}" >&2
		exit 1
	fi
done

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
# shellcheck source=deploy-gosx-docs-transaction.sh
. "${script_dir}/deploy-gosx-docs-transaction.sh"

apply_patch() {
	printf '%s\n' "$1" | jq -ce --argjson patch "$2" '
		def pointer:
			ltrimstr("/") | split("/") | map(gsub("~1"; "/") | gsub("~0"; "~"));
		reduce $patch[] as $operation (.;
			($operation.path | pointer) as $path
			| if $operation.op == "test" then
				if getpath($path) == $operation.value then .
				else error("JSON Patch test rejected at " + $operation.path)
				end
			elif $operation.op == "replace" or $operation.op == "add" then
				setpath($path; $operation.value)
			elif $operation.op == "remove" then delpaths([$path])
			else error("unsupported JSON Patch operation " + $operation.op)
			end
		)'
}

expect_rejected() {
	if apply_patch "$2" "$3" >/dev/null 2>&1; then
		echo "deploy concurrency test: $1 unexpectedly succeeded" >&2
		exit 1
	fi
}

uid="deployment-uid"
previous_generation=7
release_generation=8
previous_resource_version="100"
previous_spec='{"replicas":1,"template":{"metadata":{"labels":{"app":"gosx-docs"}},"spec":{"containers":[{"name":"gosx-docs","image":"registry/docs@sha256:previous"}]}}}'
release_spec='{"replicas":1,"template":{"metadata":{"labels":{"app":"gosx-docs"}},"spec":{"containers":[{"name":"gosx-docs","image":"registry/docs@sha256:release"}]}}}'
newer_spec='{"replicas":1,"template":{"metadata":{"labels":{"app":"gosx-docs"}},"spec":{"containers":[{"name":"gosx-docs","image":"registry/docs@sha256:newer"}]}}}'
previous_change_cause="previous"
release_change_cause="release"
release_transaction="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
other_transaction="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

previous_document="$(jq -cn \
	--arg uid "$uid" \
	--arg resourceVersion "$previous_resource_version" \
	--argjson generation "$previous_generation" \
	--argjson spec "$previous_spec" \
	--arg cause "$previous_change_cause" \
	'{metadata:{uid:$uid,resourceVersion:$resourceVersion,generation:$generation,annotations:{"kubernetes.io/change-cause":$cause}},spec:$spec}')"

owner() {
	gosx_docs_deployment_owner \
		"$1" "$uid" "$previous_spec" true "$previous_change_cause" false "" \
		"$release_spec" "$release_change_cause" "$release_transaction"
}

expect_owner() {
	actual="$(owner "$2")"
	if [ "$actual" != "$1" ]; then
		echo "deploy concurrency test: owner=${actual}; expected $1" >&2
		exit 1
	fi
}

expect_owner base "$previous_document"
gosx_docs_deployment_matches_base \
	"$previous_document" "$uid" "$previous_generation" "$previous_spec" \
	true "$previous_change_cause" false ""

release_patch="$(gosx_docs_release_patch \
	"$uid" "$previous_resource_version" "$previous_generation" \
	"$previous_spec" "$(printf '%s\n' "$previous_spec" | jq -c '.template')" \
	"$release_spec" "$release_change_cause" "$release_transaction")"
released_document="$(apply_patch "$previous_document" "$release_patch")"
released_document="$(printf '%s\n' "$released_document" | jq -c \
	--argjson generation "$release_generation" \
	'.metadata.resourceVersion = "101" | .metadata.generation = $generation')"
expect_owner release "$released_document"

# A newer release that lands after this build began is rejected before support
# validation and again by the short resourceVersion CAS.
newer_document="$(printf '%s\n' "$previous_document" | jq -c \
	--argjson spec "$newer_spec" --arg transaction "$other_transaction" \
	'.metadata.resourceVersion = "101" | .metadata.generation = 8 |
	 .metadata.annotations["kubernetes.io/change-cause"] = "newer" |
	 .metadata.annotations["gosx.m31labs.dev/deploy-transaction"] = $transaction |
	 .spec = $spec')"
if gosx_docs_deployment_matches_base \
	"$newer_document" "$uid" "$previous_generation" "$previous_spec" \
	true "$previous_change_cause" false ""; then
	echo "deploy concurrency test: stale build adopted a newer release" >&2
	exit 1
fi
expect_rejected "stale release transaction" "$newer_document" "$release_patch"

# Controller metadata churn does not invalidate long-build ownership, but the
# final resourceVersion CAS still notices it.
controller_document="$(printf '%s\n' "$previous_document" | jq -c \
	'.metadata.resourceVersion = "101" | .metadata.annotations["deployment.kubernetes.io/revision"] = "41"')"
gosx_docs_deployment_matches_base \
	"$controller_document" "$uid" "$previous_generation" "$previous_spec" \
	true "$previous_change_cause" false ""
expect_rejected "metadata-only concurrent write" "$controller_document" "$release_patch"

# If a request may still be in flight while recovery observes the base, the
# fence wins the same resourceVersion and makes a later release commit fail.
fence_patch="$(gosx_docs_recovery_fence_patch \
	"$uid" "$previous_resource_version" "$previous_generation" \
	"$previous_spec" "$(printf '%s\n' "$previous_spec" | jq -c '.template')" \
	true "$previous_change_cause" false "" "$release_transaction")"
fenced_document="$(apply_patch "$previous_document" "$fence_patch")"
fenced_document="$(printf '%s\n' "$fenced_document" | jq -c '.metadata.resourceVersion = "101"')"
expect_rejected "release after recovery fence" "$fenced_document" "$release_patch"

# Two same-SHA/same-second runs have identical spec and change-cause, but their
# unique transaction IDs keep one from rolling back the other.
other_identical_release="$(printf '%s\n' "$released_document" | jq -c \
	--arg transaction "$other_transaction" \
	'.metadata.annotations["gosx.m31labs.dev/deploy-transaction"] = $transaction')"
expect_owner other "$other_identical_release"

rollback_patch="$(gosx_docs_rollback_patch \
	"$uid" "$release_generation" "$release_spec" \
	"$(printf '%s\n' "$release_spec" | jq -c '.template')" \
	"$release_change_cause" "$release_transaction" "$previous_spec" \
	true "$previous_change_cause" false "")"

# Controller annotations survive rollback; release ownership annotations and
# spec return exactly to the captured base.
released_document="$(printf '%s\n' "$released_document" | jq -c \
	'.metadata.annotations["deployment.kubernetes.io/revision"] = "42"')"
rolled_back_document="$(apply_patch "$released_document" "$rollback_patch")"
printf '%s\n' "$rolled_back_document" | jq -e \
	--argjson spec "$previous_spec" --arg cause "$previous_change_cause" '
	.spec == $spec and
	.metadata.annotations["kubernetes.io/change-cause"] == $cause and
	((.metadata.annotations // {}) | has("gosx.m31labs.dev/deploy-transaction") | not) and
	.metadata.annotations["deployment.kubernetes.io/revision"] == "42"' >/dev/null

# A newer owner can never be rolled back by this invocation.
expect_rejected "stale rollback transaction" "$other_identical_release" "$rollback_patch"

printf '%s\n' "deploy concurrency test: release, recovery, and rollback fences passed"
