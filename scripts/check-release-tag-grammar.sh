#!/usr/bin/env sh
set -eu

usage() {
	echo "usage: check-release-tag-grammar.sh TAG" >&2
}

if [ "$#" -ne 1 ]; then
	usage
	exit 2
fi

release_tag="$1"

reject() {
	echo "release tag must be canonical stable vMAJOR.MINOR.PATCH with no leading numeric zeroes" >&2
	exit 1
}

case "$release_tag" in
	*'
'*) reject ;;
esac
carriage_return="$(printf '\r')"
case "$release_tag" in
	*"$carriage_return"*) reject ;;
esac

case "$release_tag" in
	v*.*.*) ;;
	*) reject ;;
esac

version="${release_tag#v}"
major="${version%%.*}"
rest="${version#*.}"
minor="${rest%%.*}"
patch="${rest#*.}"

case "$patch" in
	*.*) reject ;;
esac

check_component() {
	component="$1"
	case "$component" in
		"") reject ;;
		*[!0123456789]*) reject ;;
		0) return 0 ;;
		0*) reject ;;
	esac
}

check_component "$major"
check_component "$minor"
check_component "$patch"
