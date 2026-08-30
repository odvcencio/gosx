#!/usr/bin/env sh
set -eu

usage() {
	echo "usage: check-gosx-docs-deploy-source.sh [--root DIR] [--remote REMOTE] [--branch BRANCH] [--expected-revision SHA] [--allow-fetch 0|1]" >&2
}

repo_root="."
remote_name="${GOSX_DOCS_DEPLOY_REMOTE:-origin}"
default_branch="${GOSX_DOCS_DEPLOY_BRANCH:-main}"
expected_revision="${GOSX_DOCS_DEPLOY_EXPECT_REVISION:-}"
allow_fetch="${GOSX_DOCS_DEPLOY_ALLOW_FETCH:-1}"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--root)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			repo_root="$2"
			shift 2
			;;
		--remote)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			remote_name="$2"
			shift 2
			;;
		--branch)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			default_branch="$2"
			shift 2
			;;
		--expected-revision)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			expected_revision="$2"
			shift 2
			;;
		--allow-fetch)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			allow_fetch="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			usage
			echo "gosx docs deploy source: unexpected argument: $1" >&2
			exit 2
			;;
	esac
done

reject_control() {
	label="$1"
	value="$2"
	case "$value" in
		*'
'*)
			echo "gosx docs deploy source: ${label} must not contain a newline" >&2
			exit 1
			;;
	esac
	cr="$(printf '\r')"
	case "$value" in
		*"$cr"*)
			echo "gosx docs deploy source: ${label} must not contain a carriage return" >&2
			exit 1
			;;
	esac
}

for pair in "root:$repo_root" "remote:$remote_name" "branch:$default_branch"; do
	label="${pair%%:*}"
	value="${pair#*:}"
	if [ -z "$value" ]; then
		echo "gosx docs deploy source: ${label} is required" >&2
		exit 1
	fi
	reject_control "$label" "$value"
done
case "$remote_name" in
	-*)
		echo "gosx docs deploy source: remote must not be option-like" >&2
		exit 1
		;;
esac
case "$default_branch" in
	-*|*..*|*//*|/*|*/)
		echo "gosx docs deploy source: branch must be a simple branch name" >&2
		exit 1
		;;
esac

case "$allow_fetch" in
	0|1) ;;
	*)
		echo "gosx docs deploy source: --allow-fetch must be 0 or 1" >&2
		exit 2
		;;
esac

if [ -n "$expected_revision" ]; then
	reject_control "expected revision" "$expected_revision"
	case "$expected_revision" in
		????????????????????????????????????????) ;;
		*)
			echo "gosx docs deploy source: expected revision must be a full 40-hex commit SHA" >&2
			exit 1
			;;
	esac
	case "$expected_revision" in
		*[!0123456789abcdefABCDEF]*)
			echo "gosx docs deploy source: expected revision must be a full 40-hex commit SHA" >&2
			exit 1
			;;
	esac
fi

if ! git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	echo "gosx docs deploy source: root is not a git worktree: ${repo_root}" >&2
	exit 1
fi

worktree_changes="$(git -C "$repo_root" status --porcelain --untracked-files=normal -- . ':(exclude)examples/gosx-docs/dist')"
if [ -n "$worktree_changes" ]; then
	echo "gosx docs deploy source: refusing a dirty worktree outside examples/gosx-docs/dist" >&2
	printf '%s\n' "$worktree_changes" >&2
	exit 1
fi

current_branch="$(git -C "$repo_root" symbolic-ref -q --short HEAD || true)"
if [ "$current_branch" != "$default_branch" ]; then
	echo "gosx docs deploy source: deployment must run from branch ${default_branch}, got ${current_branch:-detached HEAD}" >&2
	exit 1
fi

remote_ref="refs/remotes/${remote_name}/${default_branch}"
if [ "$allow_fetch" -eq 1 ]; then
	if ! git -C "$repo_root" fetch --no-tags "$remote_name" "+refs/heads/${default_branch}:${remote_ref}"; then
		echo "gosx docs deploy source: could not fetch ${remote_name}/${default_branch}" >&2
		echo "gosx docs deploy source: deployment source must be checked against a fresh default-branch ref" >&2
		exit 1
	fi
fi

head_commit="$(git -C "$repo_root" rev-parse -q --verify HEAD^{commit} || true)"
remote_commit="$(git -C "$repo_root" rev-parse -q --verify "${remote_ref}^{commit}" || true)"
if [ -z "$head_commit" ] || [ -z "$remote_commit" ]; then
	echo "gosx docs deploy source: could not resolve HEAD or ${remote_ref}" >&2
	exit 1
fi

if [ "$head_commit" != "$remote_commit" ]; then
	echo "gosx docs deploy source: HEAD ${head_commit} must equal fresh ${remote_name}/${default_branch} ${remote_commit}" >&2
	exit 1
fi

if [ -n "$expected_revision" ] && [ "$expected_revision" != "$head_commit" ]; then
	echo "gosx docs deploy source: HEAD ${head_commit} does not match expected revision ${expected_revision}" >&2
	exit 1
fi

printf 'revision=%s\n' "$head_commit"
