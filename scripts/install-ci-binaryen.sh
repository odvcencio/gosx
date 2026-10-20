#!/usr/bin/env bash
set -euo pipefail

# Use the production builder's existing wasm-opt pass with reproducible tooling.
binaryen_version="108"
binaryen_sha256="7bb8a2d97214f40bf34abc31d49b34aa5deab10b25d6d13c5f72cb395cf142fb"
work_dir="$(mktemp -d)"
trap 'rm -rf -- "${work_dir}"' EXIT
install_dir="${RUNNER_TEMP:-/tmp}/gosx-binaryen-${binaryen_version}"

curl --fail --location --silent --show-error \
	--retry 5 --retry-delay 2 --retry-all-errors \
	--output "${work_dir}/binaryen.tar.gz" \
	"https://github.com/WebAssembly/binaryen/releases/download/version_${binaryen_version}/binaryen-version_${binaryen_version}-x86_64-linux.tar.gz"
printf '%s  %s\n' "${binaryen_sha256}" "${work_dir}/binaryen.tar.gz" | sha256sum --check --status
mkdir -p "${install_dir}"
tar -C "${install_dir}" --strip-components=1 -xzf "${work_dir}/binaryen.tar.gz"
version_output="$("${install_dir}/bin/wasm-opt" --version)"
if [[ "${version_output}" != "wasm-opt version ${binaryen_version} (version_${binaryen_version})" ]]; then
	echo "install-ci-binaryen: unexpected optimizer: ${version_output}" >&2
	exit 1
fi
if [[ -n "${GITHUB_PATH:-}" ]]; then
	printf '%s\n' "${install_dir}/bin" >>"${GITHUB_PATH}"
fi
printf '%s\n' "${version_output}"
