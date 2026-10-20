#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
verifier="$repo_root/scripts/verify-perf-browser-identity.sh"
tmp_dir="$(mktemp -d)"

cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

write_browser() {
	path="$1"
	product="$2"
	status="$3"
	cat >"$path" <<EOF
#!/usr/bin/env sh
printf '%s\\n' '$product'
exit $status
EOF
	chmod 700 "$path"
}

run_case() {
	name="$1"
	browser="$2"
	action_version="$3"
	want_status="$4"
	want_error="$5"
	receipt="$tmp_dir/${name}.receipt"
	stdout="$tmp_dir/${name}.stdout"
	stderr="$tmp_dir/${name}.stderr"

	if sh "$verifier" "$browser" "$action_version" "$receipt" >"$stdout" 2>"$stderr"; then
		got_status=0
	else
		got_status=$?
	fi
	if [ "$got_status" -ne "$want_status" ]; then
		echo "verify-perf-browser-identity-test: $name got status $got_status, want $want_status" >&2
		cat "$stdout" "$stderr" >&2
		exit 1
	fi
	if [ ! -s "$receipt" ]; then
		echo "verify-perf-browser-identity-test: $name did not preserve an identity receipt" >&2
		exit 1
	fi
	if ! grep -F "configuredSnapshot=1688711" "$receipt" >/dev/null ||
		! grep -F "actionVersion=${action_version}" "$receipt" >/dev/null ||
		! grep -F "path=${browser}" "$receipt" >/dev/null; then
		echo "verify-perf-browser-identity-test: $name receipt lost governed identity fields" >&2
		cat "$receipt" >&2
		exit 1
	fi
	if [ -n "$want_error" ] && ! grep -F "$want_error" "$stderr" >/dev/null; then
		echo "verify-perf-browser-identity-test: $name missing diagnostic: $want_error" >&2
		cat "$stdout" "$stderr" >&2
		exit 1
	fi
}

good="$tmp_dir/chromium-good"
wrong_family="$tmp_dir/chromium-wrong-family"
wrong_version="$tmp_dir/chromium-wrong-version"
failed="$tmp_dir/chromium-failed"
write_browser "$good" "Chromium 154.0.8034.0 " 0
write_browser "$wrong_family" "NotChromium 154.0.8034.0" 0
write_browser "$wrong_version" "Chromium 154.0.8035.0" 0
write_browser "$failed" "browser failed to start" 9

run_case success "$good" "154.0.8034.0" 0 ""
run_case action-version-mismatch "$good" "154.0.8035.0" 1 "action version mismatch"
run_case wrong-family "$wrong_family" "154.0.8034.0" 1 "CLI identity mismatch"
run_case wrong-version "$wrong_version" "154.0.8034.0" 1 "CLI identity mismatch"
run_case cli-failure "$failed" "154.0.8034.0" 1 "CLI identity mismatch: status 9"
run_case empty-path "" "154.0.8034.0" 1 "CLI identity mismatch: status 127"

if ! grep -F "cliProduct=Chromium 154.0.8034.0 " "$tmp_dir/success.receipt" >/dev/null ||
	! grep -F "cliProductNormalized=Chromium 154.0.8034.0" "$tmp_dir/success.receipt" >/dev/null ||
	! grep -F "cliStatus=0" "$tmp_dir/success.receipt" >/dev/null; then
	echo "verify-perf-browser-identity-test: successful receipt lost exact CLI identity" >&2
	exit 1
fi

echo "verify-perf-browser-identity-test: ok"
