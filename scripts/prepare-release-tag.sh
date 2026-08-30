#!/usr/bin/env sh
set -eu

usage() {
	echo "usage: prepare-release-tag.sh --tag vX.Y.Z --default-branch BRANCH [--root DIR] [--remote REMOTE] [--create --expected-commit SHA] [--github-output FILE]" >&2
}

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
repo_root=""
release_tag=""
default_branch="${RELEASE_DEFAULT_BRANCH:-${GITHUB_DEFAULT_BRANCH:-}}"
remote_name="${GOSX_RELEASE_REMOTE:-origin}"
create_tag=0
github_output=""
expected_commit=""

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
		--create)
			create_tag=1
			shift
			;;
		--expected-commit)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			expected_commit="$2"
			shift 2
			;;
		--github-output)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			github_output="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			usage
			echo "prepare-release-tag: unexpected argument: $1" >&2
			exit 2
			;;
	esac
done

if [ -z "$release_tag" ]; then
	echo "prepare-release-tag: release tag is required" >&2
	exit 1
fi
if [ -z "$default_branch" ]; then
	echo "prepare-release-tag: default branch is required" >&2
	exit 1
fi
sh "${script_dir}/check-release-tag-grammar.sh" "$release_tag" || exit 1
if [ "$create_tag" -eq 1 ] && [ -z "$expected_commit" ]; then
	echo "prepare-release-tag: --expected-commit is required with --create" >&2
	exit 1
fi

git_root="${repo_root:-.}"
default_remote_ref="refs/remotes/${remote_name}/${default_branch}"
if ! git -C "$git_root" fetch --tags --force "$remote_name" "+refs/heads/${default_branch}:${default_remote_ref}"; then
	echo "prepare-release-tag: could not fetch ${remote_name}/${default_branch} and tags" >&2
	echo "release tag preparation must use fresh remote refs" >&2
	exit 1
fi

default_commit="$(git -C "$git_root" rev-parse -q --verify "${default_remote_ref}^{commit}" || true)"
if [ -z "$default_commit" ]; then
	echo "prepare-release-tag: default branch ref is missing: ${default_remote_ref}" >&2
	exit 1
fi

tag_commit="$(git -C "$git_root" rev-parse -q --verify "refs/tags/${release_tag}^{commit}" || true)"
mode="create"
source_ref="$default_commit"
release_commit="$default_commit"

if [ -n "$tag_commit" ]; then
	if [ -n "$expected_commit" ] && [ "$tag_commit" != "$expected_commit" ]; then
		echo "prepare-release-tag: ${release_tag} now resolves to ${tag_commit}, expected ${expected_commit}" >&2
		echo "release tag state changed after preparation" >&2
		exit 1
	fi
	if ! git -C "$git_root" merge-base --is-ancestor "$tag_commit" "$default_commit"; then
		echo "prepare-release-tag: ${release_tag} (${tag_commit}) is not reachable from ${remote_name}/${default_branch} (${default_commit})" >&2
		echo "release tags must belong to canonical default-branch lineage" >&2
		exit 1
	fi
	mode="existing"
	source_ref="$tag_commit"
	release_commit="$tag_commit"
elif [ "$create_tag" -eq 1 ]; then
	if [ -z "$repo_root" ]; then
		echo "prepare-release-tag: --root is required when creating a tag" >&2
		exit 1
	fi
	source_head="$(git -C "$repo_root" rev-parse -q --verify HEAD || true)"
	if [ "$source_head" != "$default_commit" ]; then
		echo "prepare-release-tag: new tag ${release_tag} must be created at ${remote_name}/${default_branch} HEAD ${default_commit}, got ${source_head}" >&2
		exit 1
	fi
	if [ "$expected_commit" != "$default_commit" ]; then
		echo "prepare-release-tag: fresh ${remote_name}/${default_branch} HEAD ${default_commit} does not match prepared commit ${expected_commit}" >&2
		exit 1
	fi
	sh "${script_dir}/check-release-source-truth.sh" --root "$repo_root" --tag "$release_tag" >/dev/null
	git -C "$repo_root" tag -a "$release_tag" "$default_commit" -m "Release ${release_tag}"
	git -C "$repo_root" push "$remote_name" "refs/tags/${release_tag}"
	mode="created"
	source_ref="$default_commit"
	release_commit="$default_commit"
fi

if [ -n "$github_output" ]; then
	{
		printf 'mode=%s\n' "$mode"
		printf 'source_ref=%s\n' "$source_ref"
		printf 'commit=%s\n' "$release_commit"
	} >>"$github_output"
fi

echo "prepare-release-tag: mode=${mode} tag=${release_tag} commit=${release_commit} source_ref=${source_ref}"
