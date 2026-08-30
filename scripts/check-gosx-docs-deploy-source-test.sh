#!/usr/bin/env sh
set -eu

for command_name in git mktemp; do
	if ! command -v "$command_name" >/dev/null 2>&1; then
		echo "gosx docs deploy source test: required command is unavailable: ${command_name}" >&2
		exit 1
	fi
done

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
source_script="${script_dir}/check-gosx-docs-deploy-source.sh"
tmp_dir="$(mktemp -d)"
cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

remote_dir="${tmp_dir}/remote.git"
repo_dir="${tmp_dir}/repo"
git init --bare --initial-branch=main "$remote_dir" >/dev/null
git init --initial-branch=main "$repo_dir" >/dev/null
git -C "$repo_dir" config user.email "docs-deploy-test@example.invalid"
git -C "$repo_dir" config user.name "Docs Deploy Test"
git -C "$repo_dir" remote add origin "$remote_dir"

printf '%s\n' "base" >"${repo_dir}/file.txt"
git -C "$repo_dir" add file.txt
git -C "$repo_dir" commit -m "base" >/dev/null
git -C "$repo_dir" push -u origin main >/dev/null
base_commit="$(git -C "$repo_dir" rev-parse HEAD)"

output="$("$source_script" --root "$repo_dir" --branch main)"
case "$output" in
	"revision=${base_commit}") ;;
	*)
		echo "gosx docs deploy source test: exact main did not report the approved revision" >&2
		echo "$output" >&2
		exit 1
		;;
esac
"$source_script" --root "$repo_dir" --branch main --expected-revision "$base_commit" >/dev/null

expect_reject() {
	label="$1"
	want="$2"
	shift 2
	if "$source_script" "$@" >"${tmp_dir}/${label}.out" 2>&1; then
		echo "gosx docs deploy source test: ${label} unexpectedly passed" >&2
		cat "${tmp_dir}/${label}.out" >&2
		exit 1
	fi
	if ! grep -F -- "$want" "${tmp_dir}/${label}.out" >/dev/null; then
		echo "gosx docs deploy source test: ${label} failure did not explain ${want}" >&2
		cat "${tmp_dir}/${label}.out" >&2
		exit 1
	fi
}

expect_reject option-like-remote "remote must not be option-like" \
	--root "$repo_dir" --remote "-origin" --branch main
expect_reject option-like-branch "branch must be a simple branch name" \
	--root "$repo_dir" --branch "-main"
expect_reject parent-traversal-branch "branch must be a simple branch name" \
	--root "$repo_dir" --branch "release..main"
newline_branch="$(printf 'main\njunk')"
expect_reject newline-branch "branch must not contain a newline" \
	--root "$repo_dir" --branch "$newline_branch"
cr_branch="$(printf 'main\r')"
expect_reject carriage-return-branch "branch must not contain a carriage return" \
	--root "$repo_dir" --branch "$cr_branch"
newline_expected="$(printf '%s\njunk' "$base_commit")"
expect_reject newline-expected "expected revision must not contain a newline" \
	--root "$repo_dir" --branch main --expected-revision "$newline_expected"
expect_reject option-like-allow-fetch "--allow-fetch must be 0 or 1" \
	--root "$repo_dir" --branch main --allow-fetch "-1"

wrong_expected="2222222222222222222222222222222222222222"
expect_reject wrong-expected "does not match expected revision" \
	--root "$repo_dir" --branch main --expected-revision "$wrong_expected"

printf '%s\n' "remote advances" >>"${repo_dir}/file.txt"
git -C "$repo_dir" commit -am "remote advance" >/dev/null
git -C "$repo_dir" push origin main >/dev/null
remote_ahead_commit="$(git -C "$repo_dir" rev-parse HEAD)"
git -C "$repo_dir" reset --hard "$base_commit" >/dev/null
expect_reject behind-main "must equal fresh origin/main" --root "$repo_dir" --branch main

git -C "$repo_dir" reset --hard "$remote_ahead_commit" >/dev/null
git -C "$repo_dir" checkout -b docs-preview >/dev/null 2>&1
expect_reject wrong-branch "must run from branch main" --root "$repo_dir" --branch main
git -C "$repo_dir" checkout main >/dev/null 2>&1

printf '%s\n' "local only" >>"${repo_dir}/file.txt"
git -C "$repo_dir" commit -am "local ahead" >/dev/null
local_ahead_commit="$(git -C "$repo_dir" rev-parse HEAD)"
expect_reject ahead-main "must equal fresh origin/main" --root "$repo_dir" --branch main

git -C "$repo_dir" checkout --detach "$base_commit" >/dev/null 2>&1
expect_reject detached-history "must run from branch main" --root "$repo_dir" --branch main

git -C "$repo_dir" checkout main >/dev/null 2>&1
git -C "$repo_dir" reset --hard "$remote_ahead_commit" >/dev/null
printf '%s\n' "dirty" >>"${repo_dir}/file.txt"
expect_reject dirty-tracked "refusing a dirty worktree" --root "$repo_dir" --branch main
git -C "$repo_dir" checkout -- file.txt

printf '%s\n' "untracked" >"${repo_dir}/scratch.txt"
expect_reject dirty-untracked "refusing a dirty worktree" --root "$repo_dir" --branch main
rm -f "${repo_dir}/scratch.txt"

mkdir -p "${repo_dir}/examples/gosx-docs/dist"
printf '%s\n' "generated" >"${repo_dir}/examples/gosx-docs/dist/build.json"
"$source_script" --root "$repo_dir" --branch main >/dev/null
rm -rf "${repo_dir}/examples"

git -C "$repo_dir" remote set-url origin "${tmp_dir}/missing.git"
git -C "$repo_dir" reset --hard "$remote_ahead_commit" >/dev/null
expect_reject fetch-failure "must be checked against a fresh default-branch ref" --root "$repo_dir" --branch main
GOSX_DOCS_DEPLOY_ALLOW_FETCH=0 "$source_script" --root "$repo_dir" --branch main >/dev/null
git -C "$repo_dir" remote set-url origin "$remote_dir"

if "$source_script" --root "$repo_dir" --branch main --expected-revision "$local_ahead_commit" --allow-fetch 0 >"${tmp_dir}/local-wrong-expected.out" 2>&1; then
	echo "gosx docs deploy source test: local-only wrong expected revision unexpectedly passed" >&2
	cat "${tmp_dir}/local-wrong-expected.out" >&2
	exit 1
fi

echo "gosx docs deploy source test: all checks passed"
