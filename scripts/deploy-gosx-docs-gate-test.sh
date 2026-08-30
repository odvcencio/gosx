#!/usr/bin/env sh
set -eu

for command_name in cp git grep mkdir mktemp python3 rm sed sleep; do
	if ! command -v "$command_name" >/dev/null 2>&1; then
		echo "gosx docs deploy gate test: required command is unavailable: ${command_name}" >&2
		exit 1
	fi
done

script_dir="$(CDPATH= cd "$(dirname "$0")" && pwd)"
repo_source="$(CDPATH= cd "${script_dir}/.." && pwd)"
tmp_dir="$(mktemp -d)"
cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

write_file() {
	file_path="$1"
	shift
	mkdir -p "$(dirname "$file_path")"
	printf '%s\n' "$@" >"$file_path"
}

write_fake_tools() {
	tools_dir="$1"
	mkdir -p "$tools_dir"
	write_file "${tools_dir}/go" \
		'#!/usr/bin/env sh' \
		'set -eu' \
		'printf "%s\n" "go:$*" >>"${GOSX_FAKE_DEPLOY_LOG}"' \
		'if [ "$#" -ge 2 ] && [ "$1" = "env" ] && [ "$2" = "GOHOSTOS" ]; then echo linux; exit 0; fi' \
		'if [ "$#" -ge 2 ] && [ "$1" = "env" ] && [ "$2" = "GOHOSTARCH" ]; then echo amd64; exit 0; fi' \
		'if [ "$#" -ge 1 ] && [ "$1" = "version" ]; then echo "go version go1.25.5 linux/amd64"; exit 0; fi' \
		'if [ "$#" -ge 1 ] && [ "$1" = "build" ]; then' \
		'  out=""' \
		'  while [ "$#" -gt 0 ]; do' \
		'    if [ "$1" = "-o" ]; then out="$2"; shift 2; continue; fi' \
		'    shift' \
		'  done' \
		'  if [ -z "$out" ]; then echo "fake go: missing -o" >&2; exit 1; fi' \
		'  cat >"$out" <<'"'"'GOSX_FAKE_CLI'"'"'' \
		'#!/usr/bin/env sh' \
		'set -eu' \
		'printf "%s\n" "gosx:$*" >>"${GOSX_FAKE_DEPLOY_LOG}"' \
		'if [ "$#" -ge 1 ] && [ "$1" = "version" ]; then echo "gosx v0.53.9"; exit 0; fi' \
		'if [ "$#" -ge 2 ] && [ "$1" = "build" ] && [ "$2" = "--prod" ]; then' \
		'  dist="examples/gosx-docs/dist"' \
		'  mkdir -p "${dist}/server"' \
		'  printf "{}\n" >"${dist}/build.json"' \
		'  printf "#!/usr/bin/env sh\n" >"${dist}/run.sh"' \
		'  cp "${GOSX_FAKE_IDENTITY_APP_SRC}" "${dist}/server/app"' \
		'  chmod +x "${dist}/server/app"' \
		'  case "${GOSX_FAKE_TOCTOU:-}" in' \
		'    local-commit)' \
		'      printf "%s\n" "local clean commit" > toctou-local.txt' \
		'      git add toctou-local.txt' \
		'      git commit -m "local clean docs toctou" >/dev/null' \
		'      ;;' \
		'    branch-switch)' \
		'      git switch -c docs-toctou >/dev/null 2>&1' \
		'      ;;' \
		'    remote-advance)' \
		'      advance_dir="$(mktemp -d)"' \
		'      git clone "$(git remote get-url origin)" "${advance_dir}/repo" >/dev/null 2>&1' \
		'      git -C "${advance_dir}/repo" config user.email "docs-gate@example.invalid"' \
		'      git -C "${advance_dir}/repo" config user.name "Docs Gate"' \
		'      printf "%s\n" "remote advance" >>"${advance_dir}/repo/file.txt"' \
		'      git -C "${advance_dir}/repo" commit -am "remote advance" >/dev/null' \
		'      git -C "${advance_dir}/repo" push origin main >/dev/null' \
		'      rm -rf "${advance_dir}"' \
		'      ;;' \
		'  esac' \
		'  exit 0' \
		'fi' \
		'echo "fake gosx: unexpected args: $*" >&2' \
		'exit 1' \
		'GOSX_FAKE_CLI' \
		'  chmod +x "$out"' \
		'  exit 0' \
		'fi' \
		'echo "fake go: unexpected args: $*" >&2' \
		'exit 1'
	chmod +x "${tools_dir}/go"
	write_file "${tools_dir}/tinygo" \
		'#!/usr/bin/env sh' \
		'set -eu' \
		'printf "%s\n" "tinygo:$*" >>"${GOSX_FAKE_DEPLOY_LOG}"' \
		'if [ "$#" -ge 1 ] && [ "$1" = "version" ]; then echo "tinygo version 0.41.1 linux/amd64"; exit 0; fi' \
		'echo "fake tinygo: unexpected args: $*" >&2' \
		'exit 1'
	chmod +x "${tools_dir}/tinygo"
	write_file "${tools_dir}/docker" \
		'#!/usr/bin/env sh' \
		'set -eu' \
		'case "$1" in' \
		'  version) printf "%s\n" "docker-read:version" >>"${GOSX_FAKE_DEPLOY_LOG}"; echo "linux/amd64" ;;' \
		'  build) printf "%s\n" "docker-mutation:build" >>"${GOSX_FAKE_DEPLOY_LOG}" ;;' \
		'  push) printf "%s\n" "registry-mutation:push" >>"${GOSX_FAKE_DEPLOY_LOG}"; echo "digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ;;' \
		'  image) printf "%s\n" "docker-read:image inspect" >>"${GOSX_FAKE_DEPLOY_LOG}"; echo "linux/amd64" ;;' \
		'  buildx) printf "%s\n" "registry-read:imagetools" >>"${GOSX_FAKE_DEPLOY_LOG}"; echo "\"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"" ;;' \
		'  *) echo "fake docker: unexpected args: $*" >&2; exit 1 ;;' \
		'esac'
	chmod +x "${tools_dir}/docker"
	write_file "${tools_dir}/kubectl" \
		'#!/usr/bin/env sh' \
		'set -eu' \
		'if [ "$1" = "get" ]; then' \
		'  case "$4" in' \
		'    deployment/gosx-docs)' \
		'      printf "%s\n" "kubectl-read:deployment" >>"${GOSX_FAKE_DEPLOY_LOG}"' \
		'      printf "%s\n" "{\"metadata\":{\"uid\":\"uid-1\",\"generation\":1,\"annotations\":{}},\"spec\":{\"template\":{\"spec\":{\"containers\":[]}}}}"' \
		'      exit 0' \
		'      ;;' \
		'    secret/gosx-docs)' \
		'      printf "%s\n" "kubectl-read:secret" >>"${GOSX_FAKE_DEPLOY_LOG}"' \
		'      printf "%s\n" "{\"data\":{\"session-secret\":\"cmVhbC1jbHVzdGVyLXNlY3JldA==\"}}"' \
		'      exit 0' \
		'      ;;' \
		'  esac' \
		'fi' \
		'printf "%s\n" "kubectl-mutation:$*" >>"${GOSX_FAKE_DEPLOY_LOG}"' \
		'echo "fake kubectl: unexpected or mutating args: $*" >&2' \
		'exit 1'
	chmod +x "${tools_dir}/kubectl"
}

setup_repo() {
	label="$1"
	remote_dir="${tmp_dir}/${label}-remote.git"
	repo_dir="${tmp_dir}/${label}-repo"
	git init --bare --initial-branch=main "$remote_dir" >/dev/null
	git init --initial-branch=main "$repo_dir" >/dev/null
	git -C "$repo_dir" config user.email "docs-gate@example.invalid"
	git -C "$repo_dir" config user.name "Docs Gate"
	git -C "$repo_dir" remote add origin "$remote_dir"
	mkdir -p "${repo_dir}/scripts/testdata" "${repo_dir}/examples/gosx-docs" "${repo_dir}/deploy"
	cp "${repo_source}/scripts/deploy-gosx-docs.sh" "${repo_dir}/scripts/deploy-gosx-docs.sh"
	cp "${repo_source}/scripts/check-gosx-docs-deploy-source.sh" "${repo_dir}/scripts/check-gosx-docs-deploy-source.sh"
	cp "${repo_source}/scripts/check-gosx-docs-built-identity.sh" "${repo_dir}/scripts/check-gosx-docs-built-identity.sh"
	cp "${repo_source}/scripts/testdata/fake-gosx-docs-built-identity-app.py" "${repo_dir}/scripts/testdata/fake-gosx-docs-built-identity-app.py"
	write_file "${repo_dir}/scripts/deploy-gosx-docs-transaction.sh" '# transaction stub for pre-mutation gate tests'
	write_file "${repo_dir}/scripts/deploy-gosx-docs-public.sh" '# public verification stub for pre-mutation gate tests'
	write_file "${repo_dir}/examples/gosx-docs/main.go" 'package main'
	write_file "${repo_dir}/examples/gosx-docs/Dockerfile.runtime" 'FROM scratch'
	write_file "${repo_dir}/deploy/gosx-docs.yaml" 'image: __IMAGE_DIGEST__' 'revision: __GIT_REVISION__' 'builtAt: __BUILT_AT__'
	write_file "${repo_dir}/file.txt" 'base'
	git -C "$repo_dir" add .
	git -C "$repo_dir" commit -m "base" >/dev/null
	git -C "$repo_dir" push -u origin main >/dev/null
	printf '%s\n' "$repo_dir"
}

run_deploy() {
	label="$1"
	repo_dir="$2"
	shift 2
	tools_dir="${tmp_dir}/${label}-tools"
	log_file="${tmp_dir}/${label}.log"
	out_file="${tmp_dir}/${label}.out"
	fake_go_root="${tmp_dir}/${label}-go-root"
	write_fake_tools "$tools_dir"
	mkdir -p "${fake_go_root}/bin"
	cp "${tools_dir}/go" "${fake_go_root}/bin/go"
	if (cd "$repo_dir" && \
		PATH="${tools_dir}:$PATH" \
		GO="${tools_dir}/go" \
		DOCKER="${tools_dir}/docker" \
		KUBECTL="${tools_dir}/kubectl" \
		TINYGO="${tools_dir}/tinygo" \
		GOSX_TINYGO_GOROOT="$fake_go_root" \
		GOSX_FAKE_DEPLOY_LOG="$log_file" \
		GOSX_FAKE_IDENTITY_APP_SRC="${repo_dir}/scripts/testdata/fake-gosx-docs-built-identity-app.py" \
		"$@" \
		sh scripts/deploy-gosx-docs.sh >"$out_file" 2>&1); then
		printf 'pass:%s:%s:%s\n' "$label" "$log_file" "$out_file"
	else
		printf 'fail:%s:%s:%s\n' "$label" "$log_file" "$out_file"
	fi
}

assert_failed() {
	result="$1"
	label="${result#*:}"
	label="${label%%:*}"
	case "$result" in
		fail:*) ;;
		*)
			echo "gosx docs deploy gate test: ${label} unexpectedly passed" >&2
			exit 1
			;;
	esac
}

result_log() {
	result="$1"
	rest="${result#*:}"
	rest="${rest#*:}"
	printf '%s\n' "${rest%%:*}"
}

result_out() {
	result="$1"
	rest="${result#*:}"
	rest="${rest#*:}"
	printf '%s\n' "${rest#*:}"
}

assert_no_side_effects() {
	log_file="$1"
	if [ -s "$log_file" ] && grep -E 'go:|tinygo:|docker-|registry-|kubectl-' "$log_file" >/dev/null; then
		echo "gosx docs deploy gate test: source gate failure reached side-effect-capable commands" >&2
		cat "$log_file" >&2
		exit 1
	fi
}

assert_no_mutations() {
	log_file="$1"
	if [ -s "$log_file" ] && grep -E 'docker-mutation|registry-mutation|kubectl-mutation' "$log_file" >/dev/null; then
		echo "gosx docs deploy gate test: gate failure reached a deploy mutation" >&2
		cat "$log_file" >&2
		exit 1
	fi
}

assert_output_contains() {
	result="$1"
	want="$2"
	out_file="$(result_out "$result")"
	if ! grep -F "$want" "$out_file" >/dev/null; then
		echo "gosx docs deploy gate test: failure did not explain ${want}" >&2
		cat "$out_file" >&2
		exit 1
	fi
}

repo_fetch="$(setup_repo fetch-failure)"
git -C "$repo_fetch" remote set-url origin "${tmp_dir}/missing.git"
result="$(run_deploy fetch-failure "$repo_fetch" env GOSX_DOCS_DEPLOY_REMOTE="$tmp_dir/nope.git" GOSX_DOCS_DEPLOY_BRANCH=evil GOSX_DOCS_DEPLOY_ALLOW_FETCH=0)"
assert_failed "$result"
assert_output_contains "$result" "deployment source must be checked against a fresh default-branch ref"
assert_no_side_effects "$(result_log "$result")"

repo_env="$(setup_repo env-bypass)"
result="$(run_deploy env-bypass "$repo_env" env GOSX_DOCS_DEPLOY_REMOTE="$tmp_dir/nope.git" GOSX_DOCS_DEPLOY_BRANCH=evil GOSX_DOCS_DEPLOY_ALLOW_FETCH=0 GOSX_FAKE_IDENTITY_APP_MODE=bad-identity)"
assert_failed "$result"
assert_output_contains "$result" "/api/site does not match the intended deployment identity"
if grep -F "evil" "$(result_out "$result")" >/dev/null; then
	echo "gosx docs deploy gate test: entry script honored environment branch override" >&2
	cat "$(result_out "$result")" >&2
	exit 1
fi
assert_no_mutations "$(result_log "$result")"

repo_identity="$(setup_repo identity-failure)"
identity_secret_log="${tmp_dir}/identity-secret.log"
result="$(run_deploy identity-failure "$repo_identity" env GOSX_FAKE_IDENTITY_APP_MODE=bad-identity GOSX_FAKE_IDENTITY_APP_SECRET_LOG="$identity_secret_log")"
assert_failed "$result"
assert_output_contains "$result" "/api/site does not match the intended deployment identity"
assert_no_mutations "$(result_log "$result")"
if grep -F "real-cluster-secret" "$identity_secret_log" >/dev/null; then
	echo "gosx docs deploy gate test: real cluster secret reached the local identity process" >&2
	cat "$identity_secret_log" >&2
	exit 1
fi
if ! grep -F "gosx-docs-local-identity-" "$identity_secret_log" >/dev/null; then
	echo "gosx docs deploy gate test: local identity process did not receive a disposable secret" >&2
	cat "$identity_secret_log" >&2
	exit 1
fi

for toctou_mode in local-commit branch-switch remote-advance; do
	repo_toctou="$(setup_repo "toctou-${toctou_mode}")"
	result="$(run_deploy "toctou-${toctou_mode}" "$repo_toctou" env GOSX_FAKE_TOCTOU="$toctou_mode")"
	assert_failed "$result"
	assert_output_contains "$result" "gosx docs deploy source:"
	assert_no_mutations "$(result_log "$result")"
	if ! grep -F "gosx:build --prod ./examples/gosx-docs" "$(result_log "$result")" >/dev/null; then
		echo "gosx docs deploy gate test: ${toctou_mode} did not reach the post-build source gate" >&2
		cat "$(result_log "$result")" >&2
		exit 1
	fi
done

echo "gosx docs deploy gate test: fail-closed gate ordering passed"
