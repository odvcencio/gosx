#!/usr/bin/env sh
set -eu

workflow="${1:-.github/workflows/release-governed.yml}"

if grep -n 'ssh-key:' "$workflow" >/dev/null; then
	echo "check-release-workflow-topology: release workflow checkout must not use ssh-key" >&2
	exit 1
fi
if ! grep -n 'environment: governed-release' "$workflow" >/dev/null; then
	echo "check-release-workflow-topology: publish job must use governed-release environment" >&2
	exit 1
fi

tag_step="$(awk '
	/- name: Ensure release tag exists/ { in_step = 1 }
	/- uses: actions\/download-artifact/ { in_step = 0 }
	in_step { print }
' "$workflow")"
if [ -z "$tag_step" ]; then
	echo "check-release-workflow-topology: could not locate tag creation step" >&2
	exit 1
fi

total_secret_count="$(grep -c 'GOSX_RELEASE_TAG_DEPLOY_KEY' "$workflow" || true)"
tag_secret_count="$(printf '%s\n' "$tag_step" | grep -c 'GOSX_RELEASE_TAG_DEPLOY_KEY' || true)"
if [ "$total_secret_count" -eq 0 ] || [ "$total_secret_count" -ne "$tag_secret_count" ]; then
	echo "check-release-workflow-topology: deploy-key secret must occur only in the tag step" >&2
	exit 1
fi
environment_line="$(grep -n 'environment: governed-release' "$workflow" | cut -d: -f1 | head -n 1)"
first_secret_line="$(grep -n 'GOSX_RELEASE_TAG_DEPLOY_KEY' "$workflow" | cut -d: -f1 | head -n 1)"
if [ -z "$environment_line" ] || [ -z "$first_secret_line" ] || [ "$environment_line" -ge "$first_secret_line" ]; then
	echo "check-release-workflow-topology: deploy-key secret must be inside environment-gated publish job" >&2
	exit 1
fi

for pattern in \
	'mktemp -d "${RUNNER_TEMP}/gosx-release-ssh.' \
	'chmod 700 "${release_ssh_dir}"' \
	'chmod 600 "${key_file}"' \
	'https://api.github.com/meta' \
	'test -s "${known_hosts_file}"' \
	'remote add release-origin' \
	'remote remove release-origin' \
	'shred -u "${key_file}"' \
	'GIT_SSH_COMMAND=' \
	'--remote release-origin'
do
	if ! printf '%s\n' "$tag_step" | grep -F -- "$pattern" >/dev/null; then
		echo "check-release-workflow-topology: missing tag-step pattern: $pattern" >&2
		exit 1
	fi
done

download_line="$(grep -n -- '- uses: actions/download-artifact' "$workflow" | cut -d: -f1 | head -n 1)"
trap_line="$(grep -n 'trap cleanup_release_ssh EXIT' "$workflow" | cut -d: -f1 | head -n 1)"
remote_remove_line="$(grep -n 'remote remove release-origin' "$workflow" | cut -d: -f1 | head -n 1)"
if [ -z "$download_line" ] || [ -z "$trap_line" ] || [ -z "$remote_remove_line" ]; then
	echo "check-release-workflow-topology: could not prove cleanup ordering" >&2
	exit 1
fi
if [ "$trap_line" -ge "$download_line" ] || [ "$remote_remove_line" -ge "$download_line" ]; then
	echo "check-release-workflow-topology: cleanup must be defined before download/release actions" >&2
	exit 1
fi

echo "check-release-workflow-topology: governed workflow topology passed"
