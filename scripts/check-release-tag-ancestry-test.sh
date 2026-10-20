#!/usr/bin/env sh
set -eu

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
ancestry_script="${script_dir}/check-release-tag-ancestry.sh"
prepare_script="${script_dir}/prepare-release-tag.sh"
workflow_ref_script="${script_dir}/check-release-workflow-ref.sh"
tag_grammar_script="${script_dir}/check-release-tag-grammar.sh"
tmp_dir="$(mktemp -d)"
cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

assert_grammar_rejects() {
	label="$1"
	tag="$2"
	out="${tmp_dir}/grammar-${label}.out"
	if "$tag_grammar_script" "$tag" >"$out" 2>&1; then
		echo "check-release-tag-ancestry-test: ${label} unexpectedly passed canonical tag grammar" >&2
		cat "$out" >&2
		exit 1
	fi
	if ! grep -F "canonical stable vMAJOR.MINOR.PATCH" "$out" >/dev/null; then
		echo "check-release-tag-ancestry-test: ${label} failure did not explain canonical stable grammar" >&2
		cat "$out" >&2
		exit 1
	fi
}

for valid_tag in v0.0.0 v0.2.3 v1.0.3 v1.2.0; do
	"$tag_grammar_script" "$valid_tag"
done
for invalid_tag in v01.2.3 v1.02.3 v1.2.03 v2.0 v2.0.0-rc.1 v2.0.0+build.1; do
	assert_grammar_rejects "$invalid_tag" "$invalid_tag"
done
assert_grammar_rejects "lf-suffix" "$(printf 'v1.2.3\njunk')"
assert_grammar_rejects "lf-prefix" "$(printf 'junk\nv1.2.3')"
assert_grammar_rejects "lf-two-tags" "$(printf 'v1.2.3\nv2.3.4')"
assert_grammar_rejects "cr-suffix" "$(printf 'v1.2.3\rjunk')"
assert_grammar_rejects "cr-prefix" "$(printf 'junk\rv1.2.3')"
assert_grammar_rejects "cr-trailing" "$(printf 'v1.2.3\r')"
assert_grammar_rejects "leading-space" " v1.2.3"
assert_grammar_rejects "trailing-space" "v1.2.3 "
assert_grammar_rejects "tab-in-tag" "$(printf 'v1.2\t.3')"
assert_grammar_rejects "escape-in-tag" "$(printf 'v1.2.3\033')"

remote_dir="${tmp_dir}/remote.git"
repo_dir="${tmp_dir}/repo"

git init --bare --initial-branch=trunk "$remote_dir" >/dev/null
git init --initial-branch=trunk "$repo_dir" >/dev/null
git -C "$repo_dir" config user.email "release-test@example.invalid"
git -C "$repo_dir" config user.name "Release Test"

printf '%s\n' "base" >"${repo_dir}/file.txt"
git -C "$repo_dir" add file.txt
git -C "$repo_dir" commit -m "base" >/dev/null
git -C "$repo_dir" tag v1.0.0
v1_commit="$(git -C "$repo_dir" rev-parse HEAD)"

printf '%s\n' "default branch continues" >>"${repo_dir}/file.txt"
git -C "$repo_dir" commit -am "default branch commit" >/dev/null
git -C "$repo_dir" remote add origin "$remote_dir"
git -C "$repo_dir" push -u origin trunk --tags >/dev/null

assert_prepare_create_rejects_without_refs() {
	label="$1"
	tag="$2"
	local_before="$(git -C "$repo_dir" tag -l | LC_ALL=C sort)"
	remote_before="$(git --git-dir="$remote_dir" for-each-ref --format='%(refname)' refs/tags | LC_ALL=C sort)"
	out="${tmp_dir}/prepare-invalid-${label}.out"
	if "$prepare_script" --root "$repo_dir" --tag "$tag" --default-branch trunk --create --expected-commit "$v1_commit" >"$out" 2>&1; then
		echo "check-release-tag-ancestry-test: ${label} unexpectedly passed create preparation" >&2
		cat "$out" >&2
		exit 1
	fi
	if ! grep -F "canonical stable vMAJOR.MINOR.PATCH" "$out" >/dev/null; then
		echo "check-release-tag-ancestry-test: ${label} failure did not explain canonical stable grammar" >&2
		cat "$out" >&2
		exit 1
	fi
	local_after="$(git -C "$repo_dir" tag -l | LC_ALL=C sort)"
	remote_after="$(git --git-dir="$remote_dir" for-each-ref --format='%(refname)' refs/tags | LC_ALL=C sort)"
	if [ "$local_before" != "$local_after" ] || [ "$remote_before" != "$remote_after" ]; then
		echo "check-release-tag-ancestry-test: ${label} mutated local or remote tags before canonical validation rejected it" >&2
		exit 1
	fi
}

assert_prepare_create_rejects_without_refs "lf-suffix" "$(printf 'v1.2.3\njunk')"
assert_prepare_create_rejects_without_refs "lf-prefix" "$(printf 'junk\nv1.2.3')"
assert_prepare_create_rejects_without_refs "lf-two-tags" "$(printf 'v1.2.3\nv2.3.4')"
assert_prepare_create_rejects_without_refs "cr-suffix" "$(printf 'v1.2.3\rjunk')"
assert_prepare_create_rejects_without_refs "cr-prefix" "$(printf 'junk\rv1.2.3')"
assert_prepare_create_rejects_without_refs "cr-trailing" "$(printf 'v1.2.3\r')"
assert_prepare_create_rejects_without_refs "leading-space" " v1.2.3"
assert_prepare_create_rejects_without_refs "trailing-space" "v1.2.3 "
assert_prepare_create_rejects_without_refs "tab-in-tag" "$(printf 'v1.2\t.3')"
assert_prepare_create_rejects_without_refs "escape-in-tag" "$(printf 'v1.2.3\033')"

git -C "$repo_dir" remote set-url origin "${tmp_dir}/missing-remote-before-grammar.git"
assert_prepare_create_rejects_without_refs "invalid-before-fetch" "$(printf 'v1.2.3\njunk')"
git -C "$repo_dir" remote set-url origin "$remote_dir"

output="$("$ancestry_script" --root "$repo_dir" --tag v1.0.0 --default-branch trunk)"
case "$output" in
	*"v1.0.0"*"reachable from origin/trunk"*) ;;
	*)
		echo "check-release-tag-ancestry-test: ancestor tag did not report success" >&2
		echo "$output" >&2
		exit 1
		;;
esac

output="$("$prepare_script" --root "$repo_dir" --tag v1.0.0 --default-branch trunk)"
case "$output" in
	*"mode=existing"*"tag=v1.0.0"*"source_ref=${v1_commit}"*) ;;
	*)
		echo "check-release-tag-ancestry-test: historical in-line tag was not accepted as immutable commit source" >&2
		echo "$output" >&2
		exit 1
		;;
esac

for valid_tag in v0.0.0 v0.2.3 v1.0.3 v1.2.0; do
	git -C "$repo_dir" tag "$valid_tag" "$v1_commit"
	output="$("$ancestry_script" --root "$repo_dir" --tag "$valid_tag" --default-branch trunk)"
	case "$output" in
		*"${valid_tag}"*"reachable from origin/trunk"*) ;;
		*)
			echo "check-release-tag-ancestry-test: canonical edge tag ${valid_tag} did not pass ancestry" >&2
			echo "$output" >&2
			exit 1
			;;
	esac
	output="$("$prepare_script" --root "$repo_dir" --tag "$valid_tag" --default-branch trunk)"
	case "$output" in
		*"mode=existing"*"tag=${valid_tag}"*"source_ref=${v1_commit}"*) ;;
		*)
			echo "check-release-tag-ancestry-test: canonical edge tag ${valid_tag} did not pass prepare" >&2
			echo "$output" >&2
			exit 1
			;;
	esac
done

git -C "$repo_dir" checkout --orphan release-only >/dev/null
git -C "$repo_dir" rm -rf . >/dev/null 2>&1 || true
printf '%s\n' "off default branch" >"${repo_dir}/release.txt"
git -C "$repo_dir" add release.txt
git -C "$repo_dir" commit -m "off-main release" >/dev/null
git -C "$repo_dir" tag v2.0.0

if "$ancestry_script" --root "$repo_dir" --tag v2.0.0 --default-branch trunk >"${tmp_dir}/off-main.out" 2>&1; then
	echo "check-release-tag-ancestry-test: off-default-branch tag unexpectedly passed" >&2
	cat "${tmp_dir}/off-main.out" >&2
	exit 1
fi
if ! grep -F "is not reachable from origin/trunk" "${tmp_dir}/off-main.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: off-default-branch failure did not explain ancestry" >&2
	cat "${tmp_dir}/off-main.out" >&2
	exit 1
fi

if "$prepare_script" --root "$repo_dir" --tag v2.0.0 --default-branch trunk >"${tmp_dir}/prepare-off-main.out" 2>&1; then
	echo "check-release-tag-ancestry-test: prepare accepted off-default-branch tag" >&2
	cat "${tmp_dir}/prepare-off-main.out" >&2
	exit 1
fi
if ! grep -F "canonical default-branch lineage" "${tmp_dir}/prepare-off-main.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: prepare off-branch failure did not explain lineage" >&2
	cat "${tmp_dir}/prepare-off-main.out" >&2
	exit 1
fi

for invalid_tag in v01.2.3 v1.02.3 v1.2.03 v2.0 v2.0.0-rc.1 v2.0.0+build.1; do
	if "$ancestry_script" --root "$repo_dir" --tag "$invalid_tag" --default-branch trunk >"${tmp_dir}/ancestry-invalid-${invalid_tag}.out" 2>&1; then
		echo "check-release-tag-ancestry-test: ${invalid_tag} unexpectedly passed ancestry grammar" >&2
		cat "${tmp_dir}/ancestry-invalid-${invalid_tag}.out" >&2
		exit 1
	fi
	if ! grep -F "canonical stable vMAJOR.MINOR.PATCH" "${tmp_dir}/ancestry-invalid-${invalid_tag}.out" >/dev/null; then
		echo "check-release-tag-ancestry-test: ${invalid_tag} ancestry failure did not explain canonical stable grammar" >&2
		cat "${tmp_dir}/ancestry-invalid-${invalid_tag}.out" >&2
		exit 1
	fi
	if "$prepare_script" --root "$repo_dir" --tag "$invalid_tag" --default-branch trunk --create --expected-commit "$v1_commit" >"${tmp_dir}/invalid-${invalid_tag}.out" 2>&1; then
		echo "check-release-tag-ancestry-test: ${invalid_tag} unexpectedly passed create preparation" >&2
		cat "${tmp_dir}/invalid-${invalid_tag}.out" >&2
		exit 1
	fi
	if ! grep -F "canonical stable vMAJOR.MINOR.PATCH" "${tmp_dir}/invalid-${invalid_tag}.out" >/dev/null; then
		echo "check-release-tag-ancestry-test: ${invalid_tag} failure did not explain canonical stable grammar" >&2
		cat "${tmp_dir}/invalid-${invalid_tag}.out" >&2
		exit 1
	fi
	if git -C "$repo_dir" rev-parse -q --verify "refs/tags/${invalid_tag}" >/dev/null; then
		echo "check-release-tag-ancestry-test: invalid tag ${invalid_tag} was created before canonical validation rejected it" >&2
		exit 1
	fi
done

git -C "$repo_dir" checkout trunk >/dev/null
git -C "$repo_dir" remote set-head origin -a >/dev/null
output="$("$ancestry_script" --root "$repo_dir" --tag v1.0.0)"
case "$output" in
	*"v1.0.0"*"reachable from origin/trunk"*) ;;
	*)
		echo "check-release-tag-ancestry-test: origin/HEAD discovery did not report success" >&2
		echo "$output" >&2
		exit 1
		;;
esac

git -C "$repo_dir" remote set-url origin "${tmp_dir}/missing-remote.git"
if "$ancestry_script" --root "$repo_dir" --tag v1.0.0 --default-branch trunk >"${tmp_dir}/fetch-fails.out" 2>&1; then
	echo "check-release-tag-ancestry-test: failed fetch with stale local ref unexpectedly passed" >&2
	cat "${tmp_dir}/fetch-fails.out" >&2
	exit 1
fi
if ! grep -F "could not fetch origin/trunk" "${tmp_dir}/fetch-fails.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: failed fetch did not explain fresh-ref requirement" >&2
	cat "${tmp_dir}/fetch-fails.out" >&2
	exit 1
fi

if "$prepare_script" --root "$repo_dir" --tag v1.0.0 --default-branch trunk >"${tmp_dir}/prepare-fetch-fails.out" 2>&1; then
	echo "check-release-tag-ancestry-test: prepare accepted stale ref after fetch failure" >&2
	cat "${tmp_dir}/prepare-fetch-fails.out" >&2
	exit 1
fi
if ! grep -F "must use fresh remote refs" "${tmp_dir}/prepare-fetch-fails.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: prepare fetch failure did not explain fresh-ref requirement" >&2
	cat "${tmp_dir}/prepare-fetch-fails.out" >&2
	exit 1
fi

output="$(GOSX_RELEASE_ANCESTRY_FETCH=0 "$ancestry_script" --root "$repo_dir" --tag v1.0.0 --default-branch trunk)"
case "$output" in
	*"v1.0.0"*"reachable from origin/trunk"*) ;;
	*)
		echo "check-release-tag-ancestry-test: explicit local-only mode did not use stale local ref" >&2
		echo "$output" >&2
		exit 1
		;;
esac

git -C "$repo_dir" remote set-url origin "$remote_dir"
mkdir -p "${repo_dir}/internal/version"
printf '%s\n' "module m31labs.dev/gosx" >"${repo_dir}/go.mod"
printf '%s\n' "package version" "" "const Current = \"v3.0.0\"" "const Number = \"3.0.0\"" >"${repo_dir}/internal/version/version.go"
printf '%s\n' "Current release: **v3.0.0**." >"${repo_dir}/README.md"
printf '%s\n' "## v3.0.0" >"${repo_dir}/CHANGELOG.md"
git -C "$repo_dir" add go.mod internal/version/version.go README.md CHANGELOG.md
git -C "$repo_dir" commit -m "prepare v3.0.0 release truth" >/dev/null
git -C "$repo_dir" push origin trunk >/dev/null
trunk_head="$(git -C "$repo_dir" rev-parse HEAD)"

output="$("$prepare_script" --root "$repo_dir" --tag v3.0.0 --default-branch trunk --create --expected-commit "$trunk_head")"
case "$output" in
	*"mode=created"*"tag=v3.0.0"*"source_ref=${trunk_head}"*) ;;
	*)
		echo "check-release-tag-ancestry-test: new default-head tag was not created" >&2
		echo "$output" >&2
		exit 1
		;;
esac
created_commit="$(git -C "$repo_dir" rev-parse "refs/tags/v3.0.0^{commit}")"
remote_created_commit="$(git --git-dir="$remote_dir" rev-parse "refs/tags/v3.0.0^{commit}")"
if [ "$created_commit" != "$trunk_head" ] || [ "$remote_created_commit" != "$trunk_head" ]; then
	echo "check-release-tag-ancestry-test: new tag was not created at fresh default HEAD" >&2
	exit 1
fi

output="$("$prepare_script" --root "$repo_dir" --tag v3.0.0 --default-branch trunk --create --expected-commit "$trunk_head")"
case "$output" in
	*"mode=existing"*"tag=v3.0.0"*) ;;
	*)
		echo "check-release-tag-ancestry-test: rerun did not treat existing tag idempotently" >&2
		echo "$output" >&2
		exit 1
		;;
esac

printf '%s\n' "default branch still advances" >>"${repo_dir}/file.txt"
git -C "$repo_dir" add file.txt
git -C "$repo_dir" commit -m "advance default after v3.0.0" >/dev/null
git -C "$repo_dir" push origin trunk >/dev/null
moved_tag_commit="$(git -C "$repo_dir" rev-parse HEAD)"
git -C "$repo_dir" tag -f -a v3.0.0 "$moved_tag_commit" -m "Moved v3.0.0" >/dev/null
git -C "$repo_dir" push --force origin "refs/tags/v3.0.0" >/dev/null
if "$prepare_script" --root "$repo_dir" --tag v3.0.0 --default-branch trunk --create --expected-commit "$trunk_head" >"${tmp_dir}/moved-tag.out" 2>&1; then
	echo "check-release-tag-ancestry-test: forced in-line tag move unexpectedly passed" >&2
	cat "${tmp_dir}/moved-tag.out" >&2
	exit 1
fi
if ! grep -F "release tag state changed after preparation" "${tmp_dir}/moved-tag.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: forced tag move failure did not explain prepared commit mismatch" >&2
	cat "${tmp_dir}/moved-tag.out" >&2
	exit 1
fi

printf '%s\n' "package version" "" "const Current = \"v4.0.0\"" "const Number = \"4.0.0\"" >"${repo_dir}/internal/version/version.go"
printf '%s\n' "Current release: **v4.0.0**." >"${repo_dir}/README.md"
printf '%s\n' "## v4.0.0" >"${repo_dir}/CHANGELOG.md"
git -C "$repo_dir" add internal/version/version.go README.md CHANGELOG.md
git -C "$repo_dir" commit -m "local unpushed v4.0.0 release truth" >/dev/null
if "$prepare_script" --root "$repo_dir" --tag v4.0.0 --default-branch trunk --create --expected-commit "$moved_tag_commit" >"${tmp_dir}/not-default-head.out" 2>&1; then
	echo "check-release-tag-ancestry-test: new tag from non-default HEAD unexpectedly passed" >&2
	cat "${tmp_dir}/not-default-head.out" >&2
	exit 1
fi
if ! grep -F "must be created at origin/trunk HEAD" "${tmp_dir}/not-default-head.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: non-default HEAD failure did not explain exact HEAD requirement" >&2
	cat "${tmp_dir}/not-default-head.out" >&2
	exit 1
fi

printf '%s\n' "Current release: **v4.0.0**." "Current release: **v3.0.0**." >"${repo_dir}/README.md"
if "$script_dir/check-release-source-truth.sh" --root "$repo_dir" --tag v4.0.0 >"${tmp_dir}/stale-readme.out" 2>&1; then
	echo "check-release-tag-ancestry-test: stale duplicate README release unexpectedly passed" >&2
	cat "${tmp_dir}/stale-readme.out" >&2
	exit 1
fi
if ! grep -F "stale current-release statement" "${tmp_dir}/stale-readme.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: stale README failure did not explain duplicate release truth" >&2
	cat "${tmp_dir}/stale-readme.out" >&2
	exit 1
fi

for valid_tag in v0.0.0 v0.2.3 v1.0.3 v1.2.0; do
	valid_number="${valid_tag#v}"
	printf '%s\n' "package version" "" "const Current = \"${valid_tag}\"" "const Number = \"${valid_number}\"" >"${repo_dir}/internal/version/version.go"
	printf '%s\n' "Current release: **${valid_tag}**." >"${repo_dir}/README.md"
	printf '%s\n' "## ${valid_tag}" >"${repo_dir}/CHANGELOG.md"
	"$script_dir/check-release-source-truth.sh" --root "$repo_dir" --tag "$valid_tag" >/dev/null
done
for invalid_tag in v01.2.3 v1.02.3 v1.2.03; do
	if "$script_dir/check-release-source-truth.sh" --root "$repo_dir" --tag "$invalid_tag" >"${tmp_dir}/source-invalid-${invalid_tag}.out" 2>&1; then
		echo "check-release-tag-ancestry-test: source truth accepted non-canonical tag ${invalid_tag}" >&2
		cat "${tmp_dir}/source-invalid-${invalid_tag}.out" >&2
		exit 1
	fi
	if ! grep -F "canonical stable vMAJOR.MINOR.PATCH" "${tmp_dir}/source-invalid-${invalid_tag}.out" >/dev/null; then
		echo "check-release-tag-ancestry-test: source truth ${invalid_tag} failure did not explain canonical grammar" >&2
		cat "${tmp_dir}/source-invalid-${invalid_tag}.out" >&2
		exit 1
	fi
done

if "$workflow_ref_script" --workflow-ref-type branch --workflow-ref-name release/v3 --workflow-ref refs/heads/release/v3 --default-branch trunk >"${tmp_dir}/workflow-ref-name.out" 2>&1; then
	echo "check-release-tag-ancestry-test: non-default workflow ref unexpectedly passed" >&2
	cat "${tmp_dir}/workflow-ref-name.out" >&2
	exit 1
fi
if ! grep -F "ref name must be trunk" "${tmp_dir}/workflow-ref-name.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: workflow ref failure did not explain default branch requirement" >&2
	cat "${tmp_dir}/workflow-ref-name.out" >&2
	exit 1
fi
if "$workflow_ref_script" --workflow-ref-type tag --workflow-ref-name main --workflow-ref refs/tags/main --default-branch main >"${tmp_dir}/workflow-ref-type.out" 2>&1; then
	echo "check-release-tag-ancestry-test: tag workflow ref named main unexpectedly passed" >&2
	cat "${tmp_dir}/workflow-ref-type.out" >&2
	exit 1
fi
if ! grep -F "must run from a branch ref" "${tmp_dir}/workflow-ref-type.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: tag ref failure did not explain branch type requirement" >&2
	cat "${tmp_dir}/workflow-ref-type.out" >&2
	exit 1
fi
if "$workflow_ref_script" --workflow-ref-type branch --workflow-ref-name main --workflow-ref refs/tags/main --default-branch main >"${tmp_dir}/workflow-ref-full.out" 2>&1; then
	echo "check-release-tag-ancestry-test: wrong full workflow ref unexpectedly passed" >&2
	cat "${tmp_dir}/workflow-ref-full.out" >&2
	exit 1
fi
if ! grep -F "ref must be refs/heads/main" "${tmp_dir}/workflow-ref-full.out" >/dev/null; then
	echo "check-release-tag-ancestry-test: full ref failure did not explain refs/heads requirement" >&2
	cat "${tmp_dir}/workflow-ref-full.out" >&2
	exit 1
fi
"$workflow_ref_script" --workflow-ref-type branch --workflow-ref-name trunk --workflow-ref refs/heads/trunk --default-branch trunk >/dev/null

echo "check-release-tag-ancestry-test: all checks passed"
