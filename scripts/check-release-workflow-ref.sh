#!/usr/bin/env sh
set -eu

usage() {
	echo "usage: check-release-workflow-ref.sh [--workflow-ref-type TYPE] [--workflow-ref-name NAME] [--workflow-ref REF] [--default-branch BRANCH]" >&2
}

workflow_ref_type="${GITHUB_REF_TYPE:-}"
workflow_ref_name="${GITHUB_REF_NAME:-}"
workflow_ref="${GITHUB_REF:-}"
default_branch="${RELEASE_DEFAULT_BRANCH:-${GITHUB_DEFAULT_BRANCH:-}}"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--workflow-ref-type)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			workflow_ref_type="$2"
			shift 2
			;;
		--workflow-ref-name)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			workflow_ref_name="$2"
			shift 2
			;;
		--workflow-ref)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			workflow_ref="$2"
			shift 2
			;;
		--default-branch)
			if [ "$#" -lt 2 ]; then usage; exit 2; fi
			default_branch="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			usage
			echo "check-release-workflow-ref: unexpected argument: $1" >&2
			exit 2
			;;
	esac
done

if [ -z "$workflow_ref_type" ]; then
	echo "check-release-workflow-ref: workflow ref type is required" >&2
	exit 1
fi
if [ -z "$workflow_ref_name" ]; then
	echo "check-release-workflow-ref: workflow ref name is required" >&2
	exit 1
fi
if [ -z "$workflow_ref" ]; then
	echo "check-release-workflow-ref: full workflow ref is required" >&2
	exit 1
fi
if [ -z "$default_branch" ]; then
	echo "check-release-workflow-ref: default branch is required" >&2
	exit 1
fi
if [ "$workflow_ref_type" != "branch" ]; then
	echo "check-release-workflow-ref: release workflow must run from a branch ref, got type ${workflow_ref_type}" >&2
	exit 1
fi
if [ "$workflow_ref_name" != "$default_branch" ]; then
	echo "check-release-workflow-ref: release workflow ref name must be ${default_branch}, got ${workflow_ref_name}" >&2
	exit 1
fi
expected_ref="refs/heads/${default_branch}"
if [ "$workflow_ref" != "$expected_ref" ]; then
	echo "check-release-workflow-ref: release workflow ref must be ${expected_ref}, got ${workflow_ref}" >&2
	exit 1
fi

echo "check-release-workflow-ref: release workflow ref ${workflow_ref_type} ${workflow_ref_name} ${workflow_ref} matches default branch"
