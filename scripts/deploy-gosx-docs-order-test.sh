#!/usr/bin/env sh
set -eu

script="scripts/deploy-gosx-docs.sh"

line_of() {
	pattern="$1"
	line="$(grep -n -F "$pattern" "$script" | sed -n '1s/:.*//p')"
	if [ -z "$line" ]; then
		echo "gosx docs deploy order test: missing pattern: ${pattern}" >&2
		exit 1
	fi
	printf '%s\n' "$line"
}

require_before() {
	first_label="$1"
	first_line="$2"
	second_label="$3"
	second_line="$4"
	if [ "$first_line" -ge "$second_line" ]; then
		echo "gosx docs deploy order test: ${first_label} must run before ${second_label}" >&2
		echo "gosx docs deploy order test: ${first_label} line ${first_line}; ${second_label} line ${second_line}" >&2
		exit 1
	fi
}

source_gate_line="$(line_of 'check-gosx-docs-deploy-source.sh')"
deployment_read_line="$(line_of 'build_base_deployment_json="$($kubectl_cmd get')"
secret_read_line="$(line_of 'session_secret_present="$($kubectl_cmd get')"
cli_build_line="$(line_of 'build -o "$gosx_cli" ./cmd/gosx')"
docs_build_line="$(line_of '"$gosx_cli" build --prod ./examples/gosx-docs')"
identity_gate_line="$(line_of 'check-gosx-docs-built-identity.sh')"
docker_build_line="$(line_of '"$docker_cmd" build \')"
docker_push_line="$(line_of '"$docker_cmd" push "$image"')"

require_before "source authority gate" "$source_gate_line" "Deployment read" "$deployment_read_line"
require_before "source authority gate" "$source_gate_line" "secret read" "$secret_read_line"
require_before "source authority gate" "$source_gate_line" "CLI build" "$cli_build_line"
require_before "source authority gate" "$source_gate_line" "docs build" "$docs_build_line"
require_before "identity gate" "$identity_gate_line" "Docker build" "$docker_build_line"
require_before "identity gate" "$identity_gate_line" "Docker push" "$docker_push_line"
require_before "docs build" "$docs_build_line" "identity gate" "$identity_gate_line"

if ! grep -F 'namespace="draco-quest"' "$script" >/dev/null; then
	echo "gosx docs deploy order test: deployment namespace must remain draco-quest" >&2
	exit 1
fi
if ! grep -F 'deployment="gosx-docs"' "$script" >/dev/null; then
	echo "gosx docs deploy order test: deployment name must remain gosx-docs" >&2
	exit 1
fi
if grep -F 'm31labs' "$script" | grep -v -F 'gosx.m31labs.dev' >/dev/null; then
	echo "gosx docs deploy order test: stale m31labs namespace/path surfaced in deploy script" >&2
	exit 1
fi

echo "gosx docs deploy order test: source and identity gates precede deploy side effects"
