#!/usr/bin/env bash
set -euo pipefail

tinygo_version="${TINYGO_VERSION:-0.41.1}"
compat_go_version="${TINYGO_GO_VERSION:-1.25.5}"

if [[ "${tinygo_version}" != "0.41.1" || "${compat_go_version}" != "1.25.5" ]]; then
	echo "install-ci-tinygo: unsupported toolchain TinyGo ${tinygo_version} / Go ${compat_go_version}" >&2
	exit 1
fi

tinygo_sha256="901fbccffc61adb111656d1f907dfc21b6c499ca841b115866a0d6de2d835fbe"
compat_go_sha256="9e9b755d63b36acf30c12a9a3fc379243714c1c6d3dd72861da637f336ebb35b"
work_dir="$(mktemp -d)"
cleanup() {
	rm -rf -- "${work_dir}"
}
trap cleanup EXIT

curl --fail --location --silent --show-error \
	--retry 5 --retry-delay 2 --retry-all-errors \
	--output "${work_dir}/tinygo.deb" \
	"https://github.com/tinygo-org/tinygo/releases/download/v${tinygo_version}/tinygo_${tinygo_version}_amd64.deb"
printf '%s  %s\n' "${tinygo_sha256}" "${work_dir}/tinygo.deb" | sha256sum --check --status
sudo dpkg -i "${work_dir}/tinygo.deb"

compat_root="${HOME}/sdk/go${compat_go_version}"
if [[ ! -x "${compat_root}/bin/go" ]]; then
	if [[ -e "${compat_root}" ]]; then
		echo "install-ci-tinygo: refusing an incomplete compatibility root: ${compat_root}" >&2
		exit 1
	fi
	curl --fail --location --silent --show-error \
		--retry 5 --retry-delay 2 --retry-all-errors \
		--output "${work_dir}/go.tar.gz" \
		"https://dl.google.com/go/go${compat_go_version}.linux-amd64.tar.gz"
	printf '%s  %s\n' "${compat_go_sha256}" "${work_dir}/go.tar.gz" | sha256sum --check --status
	mkdir -p "${HOME}/sdk"
	tar -C "${work_dir}" -xzf "${work_dir}/go.tar.gz"
	mv "${work_dir}/go" "${compat_root}"
fi

compat_version_output="$(GOTOOLCHAIN=local "${compat_root}/bin/go" version)"
if [[ "${compat_version_output}" != "go version go${compat_go_version} linux/amd64" ]]; then
	echo "install-ci-tinygo: unexpected compatibility toolchain: ${compat_version_output}" >&2
	exit 1
fi

tinygo_version_output="$(tinygo version)"
if [[ "${tinygo_version_output}" != tinygo\ version\ ${tinygo_version}\ * ]]; then
	echo "install-ci-tinygo: unexpected TinyGo toolchain: ${tinygo_version_output}" >&2
	exit 1
fi

if [[ -n "${GITHUB_ENV:-}" ]]; then
	printf 'GOSX_TINYGO_GOROOT=%s\n' "${compat_root}" >>"${GITHUB_ENV}"
fi

printf '%s\n' "${tinygo_version_output}"
printf '%s\n' "compatibility GOROOT: ${compat_root} (${compat_version_output})"
