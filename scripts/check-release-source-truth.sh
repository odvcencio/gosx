#!/usr/bin/env sh
set -eu

usage() {
	echo "usage: check-release-source-truth.sh --root DIR --tag vX.Y.Z" >&2
}

repo_root="."
release_tag=""

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
		-h|--help)
			usage
			exit 0
			;;
		*)
			usage
			echo "check-release-source-truth: unexpected argument: $1" >&2
			exit 2
			;;
	esac
done

if [ -z "$release_tag" ]; then
	echo "check-release-source-truth: release tag is required" >&2
	exit 1
fi
script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
sh "${script_dir}/check-release-tag-grammar.sh" "$release_tag" || exit 1

release_number="${release_tag#v}"

if ! grep -E '^module m31labs\.dev/gosx$' "${repo_root}/go.mod" >/dev/null; then
	echo "check-release-source-truth: go.mod must declare module m31labs.dev/gosx" >&2
	exit 1
fi
if ! grep -F "const Current = \"${release_tag}\"" "${repo_root}/internal/version/version.go" >/dev/null; then
	echo "check-release-source-truth: internal/version.Current must equal ${release_tag}" >&2
	exit 1
fi
if ! grep -F "const Number = \"${release_number}\"" "${repo_root}/internal/version/version.go" >/dev/null; then
	echo "check-release-source-truth: internal/version.Number must equal ${release_number}" >&2
	exit 1
fi
readme_releases="$(grep -Eio 'current release([[:space:]]+is|:)?[[:space:]]+\*\*v[0-9]+\.[0-9]+\.[0-9]+\*\*' "${repo_root}/README.md" || true)"
if [ -z "$readme_releases" ]; then
	echo "check-release-source-truth: README.md must state current release ${release_tag}" >&2
	exit 1
fi
stale_readme="$(printf '%s\n' "$readme_releases" | grep -Fv "**${release_tag}**" || true)"
if [ -n "$stale_readme" ]; then
	echo "check-release-source-truth: README.md contains a stale current-release statement" >&2
	printf '%s\n' "$stale_readme" >&2
	exit 1
fi
if ! grep -E "^##[[:space:]]+\\[?${release_tag}\\]?([[:space:]]|$)" "${repo_root}/CHANGELOG.md" >/dev/null; then
	echo "check-release-source-truth: CHANGELOG.md must contain section ${release_tag}" >&2
	exit 1
fi

echo "check-release-source-truth: ${release_tag} source truth passed"
