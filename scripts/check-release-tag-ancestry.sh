#!/usr/bin/env sh
set -eu

usage() {
	echo "usage: check-release-tag-ancestry.sh [--root DIR] [--tag vX.Y.Z] [--default-branch BRANCH] [--remote REMOTE]" >&2
}

repo_root="."
release_tag=""
default_branch="${RELEASE_DEFAULT_BRANCH:-${GITHUB_DEFAULT_BRANCH:-}}"
remote_name="${GOSX_RELEASE_REMOTE:-origin}"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--root)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			repo_root="$2"
			shift 2
			;;
		--tag)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			release_tag="$2"
			shift 2
			;;
		--default-branch)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			default_branch="$2"
			shift 2
			;;
		--remote)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			remote_name="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			usage
			echo "check-release-tag-ancestry: unexpected argument: $1" >&2
			exit 2
			;;
	esac
done

if [ -z "$release_tag" ]; then
	if [ "${GITHUB_REF_TYPE:-}" = "tag" ]; then
		release_tag="${GITHUB_REF_NAME:-}"
	fi
fi
if [ -z "$release_tag" ] && [ -n "${GITHUB_REF:-}" ]; then
	case "$GITHUB_REF" in
		refs/tags/*) release_tag="${GITHUB_REF#refs/tags/}" ;;
	esac
fi
if [ -z "$release_tag" ]; then
	release_tag="${RELEASE_TAG:-${CI_COMMIT_TAG:-}}"
fi
if [ -z "$release_tag" ]; then
	echo "check-release-tag-ancestry: no release tag in this environment; skipping"
	exit 0
fi

if [ -z "$default_branch" ]; then
	default_ref="$(git -C "$repo_root" symbolic-ref -q --short "refs/remotes/${remote_name}/HEAD" || true)"
	if [ -n "$default_ref" ]; then
		default_branch="${default_ref#${remote_name}/}"
	fi
fi
if [ -z "$default_branch" ] && command -v gh >/dev/null 2>&1; then
	default_branch="$(gh repo view --json defaultBranchRef --jq '.defaultBranchRef.name' 2>/dev/null || true)"
fi
if [ -z "$default_branch" ]; then
	echo "check-release-tag-ancestry: could not determine the repository default branch" >&2
	echo "set RELEASE_DEFAULT_BRANCH or pass --default-branch" >&2
	exit 1
fi

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
sh "${script_dir}/check-release-tag-grammar.sh" "$release_tag" || exit 1

tag_commit="$(git -C "$repo_root" rev-parse -q --verify "refs/tags/${release_tag}^{commit}" || true)"
if [ -z "$tag_commit" ]; then
	echo "check-release-tag-ancestry: tag does not resolve to a commit: ${release_tag}" >&2
	exit 1
fi

default_remote_ref="refs/remotes/${remote_name}/${default_branch}"
if [ "${GOSX_RELEASE_ANCESTRY_FETCH:-1}" != "0" ]; then
	if ! git -C "$repo_root" fetch --no-tags "$remote_name" "+refs/heads/${default_branch}:${default_remote_ref}"; then
		echo "check-release-tag-ancestry: could not fetch ${remote_name}/${default_branch}" >&2
		echo "release ancestry must be checked against a fresh default-branch ref" >&2
		exit 1
	fi
fi

default_commit="$(git -C "$repo_root" rev-parse -q --verify "${default_remote_ref}^{commit}" || true)"
if [ -z "$default_commit" ]; then
	echo "check-release-tag-ancestry: default branch ref is missing: ${default_remote_ref}" >&2
	exit 1
fi

if ! git -C "$repo_root" merge-base --is-ancestor "$tag_commit" "$default_commit"; then
	echo "check-release-tag-ancestry: ${release_tag} (${tag_commit}) is not reachable from ${remote_name}/${default_branch} (${default_commit})" >&2
	echo "merge the exact tag commit into the default branch before creating or rerunning the release" >&2
	exit 1
fi

echo "check-release-tag-ancestry: ${release_tag} (${tag_commit}) is reachable from ${remote_name}/${default_branch} (${default_commit})"
