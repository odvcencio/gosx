#!/usr/bin/env sh
set -eu

usage() {
	echo "usage: check-repo-hygiene.sh [--root DIR]" >&2
}

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
repo_root="."
while [ "$#" -gt 0 ]; do
	case "$1" in
		--root)
			if [ "$#" -lt 2 ]; then
				usage
				exit 2
			fi
			repo_root="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			usage
			echo "check-repo-hygiene: unexpected argument: $1" >&2
			exit 2
			;;
	esac
done

repo_root="$(git -C "$repo_root" rev-parse --show-toplevel)"

failures=0

fail() {
	echo "check-repo-hygiene: $*" >&2
	failures=$((failures + 1))
}

assert_untracked() {
	path="$1"
	if git -C "$repo_root" ls-files --error-unmatch -- ":(top)$path" >/dev/null 2>&1; then
		fail "generated payload must not be tracked: $path"
	fi
}

assert_tracked_mode() {
	path="$1"
	want="$2"
	entry="$(git -C "$repo_root" ls-files -s -- ":(top)$path")"
	if [ -z "$entry" ]; then
		fail "required tracked file missing: $path"
		return
	fi
	actual="$(printf '%s\n' "$entry" | awk '{print $1}')"
	if [ "$actual" != "$want" ]; then
		fail "$path has mode $actual; want $want"
	fi
}

assert_ignored_rule() {
	rule="$1"
	if ! grep -Fx -- "$rule" "$repo_root/.gitignore" >/dev/null; then
		fail ".gitignore missing exact rule: $rule"
	fi
}

assert_ignored_effective() {
	path="$1"
	if ! git -C "$repo_root" check-ignore --no-index -q -- "$path"; then
		fail "path is not effectively ignored by git check-ignore --no-index: $path"
	fi
}

assert_bootstrap_artifact() {
	path="$1"
	assert_tracked_mode "$path" 100644
}

for path in \
	gosx-docs \
	gosx-docs.exe \
	cmd/buildbootstrap/buildbootstrap \
	cmd/buildbootstrap/buildbootstrap.exe
do
	assert_untracked "$path"
done

tmp_paths="$(git -C "$repo_root" ls-files -- ':(top)tmp/**')"
if [ -n "$tmp_paths" ]; then
	fail "top-level tmp paths must not be tracked: $(printf '%s' "$tmp_paths" | tr '\n' ' ')"
fi

for rule in \
	/gosx-docs \
	/gosx-docs.exe \
	/cmd/buildbootstrap/buildbootstrap \
	/cmd/buildbootstrap/buildbootstrap.exe \
	/tmp/
do
	assert_ignored_rule "$rule"
done

for path in \
	gosx-docs \
	gosx-docs.exe \
	cmd/buildbootstrap/buildbootstrap \
	cmd/buildbootstrap/buildbootstrap.exe \
	tmp/water-verify-desktop.png \
	tmp/water-verify-mobile.png \
	tmp/water-codex-verify.png \
	tmp/future.bin
do
	assert_ignored_effective "$path"
done

assert_tracked_mode "examples/gosx-docs/app/demos/beacon/contract.go" 100644
assert_tracked_mode "examples/gosx-docs/app/demos/beacon/evidence_test.go" 100644
assert_tracked_mode "editor/intelligenceassets/assets/gotreesitter.wasm" 100644

assert_tracked_mode "editor/intelligenceassets/assets/go.bin" 100644
assert_tracked_mode "editor/intelligenceassets/assets/wasm_exec.js" 100644

if ! "${GO:-go}" run "$script_dir/check-repo-hygiene-manifest.go" --root "$repo_root"; then
	fail "generated bootstrap manifest/artifact check failed"
fi

if [ "$failures" -ne 0 ]; then
	exit 1
fi

echo "check-repo-hygiene: repository hygiene checks passed"
