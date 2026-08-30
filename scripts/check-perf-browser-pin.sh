#!/usr/bin/env sh
set -eu

workflow="${1:-.github/workflows/ci.yml}"
repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
identity_verifier="$repo_root/scripts/verify-perf-browser-identity.sh"

fail() {
	echo "check-perf-browser-pin: $*" >&2
	exit 1
}

if [ ! -f "$workflow" ]; then
	fail "workflow not found: $workflow"
fi

extract_job() {
	job="$1"
	awk -v wanted="  ${job}:" '
		$0 == wanted { found = 1 }
		found && emitted && $0 ~ /^  [A-Za-z0-9_-]+:$/ { exit }
		found { print; emitted = 1 }
	' "$workflow"
}

extract_step() {
	step="$1"
	block="$2"
	printf '%s\n' "$block" | awk -v wanted="      - name: ${step}" '
		$0 == wanted { found = 1 }
		found && emitted && $0 ~ /^      - name: / { exit }
		found { print; emitted = 1 }
	'
}

require_line() {
	block="$1"
	needle="$2"
	label="$3"
	if ! printf '%s\n' "$block" | grep -F -x -- "$needle" >/dev/null; then
		fail "$label missing required line: $needle"
	fi
}

forbid_text() {
	block="$1"
	needle="$2"
	label="$3"
	if printf '%s\n' "$block" | grep -F -- "$needle" >/dev/null; then
		fail "$label contains forbidden text: $needle"
	fi
}

forbid_step_key() {
	block="$1"
	key="$2"
	label="$3"
	if printf '%s\n' "$block" | grep -E "^        ${key}:" >/dev/null; then
		fail "$label contains forbidden control: $key"
	fi
}

require_count() {
	block="$1"
	needle="$2"
	want="$3"
	label="$4"
	got="$(printf '%s\n' "$block" | grep -F -c -- "$needle" || true)"
	if [ "$got" -ne "$want" ]; then
		fail "$label count for '$needle' is $got, want $want"
	fi
}

browser_job="$(extract_job browser-tests)"
[ -n "$browser_job" ] || fail "browser-tests job is missing"
require_line "$browser_job" "    timeout-minutes: 30" "browser-tests job"

latest_setup="$(extract_step 'Set up Chrome' "$browser_job")"
[ -n "$latest_setup" ] || fail "latest browser setup step is missing"
require_line "$latest_setup" "        id: chrome" "latest browser setup"
require_line "$latest_setup" "        uses: browser-actions/setup-chrome@v1" "latest browser setup"
forbid_text "$latest_setup" "chrome-version:" "latest browser setup"

driver_tests="$(extract_step 'Perf driver browser tests' "$browser_job")"
[ -n "$driver_tests" ] || fail "perf driver browser test step is missing"
require_line "$driver_tests" '          CHROME_PATH: ${{ steps.chrome.outputs.chrome-path }}' "perf driver browser tests"
forbid_text "$driver_tests" "perf-chrome" "perf driver browser tests"

pinned_setup="$(extract_step 'Set up pinned Chromium for perf budget' "$browser_job")"
[ -n "$pinned_setup" ] || fail "pinned perf browser setup step is missing"
require_line "$pinned_setup" "        id: perf-chrome" "pinned perf browser setup"
require_line "$pinned_setup" "        uses: browser-actions/setup-chrome@v2" "pinned perf browser setup"
require_line "$pinned_setup" "          chrome-version: '1688711'" "pinned perf browser setup"
forbid_step_key "$pinned_setup" "continue-on-error" "pinned perf browser setup"

identity="$(extract_step 'Verify pinned perf browser identity' "$browser_job")"
[ -n "$identity" ] || fail "pinned perf browser identity step is missing"
require_line "$identity" '          PERF_CHROME_PATH: ${{ steps.perf-chrome.outputs.chrome-path }}' "pinned perf browser identity"
require_line "$identity" '          PERF_CHROME_VERSION: ${{ steps.perf-chrome.outputs.chrome-version }}' "pinned perf browser identity"
require_line "$identity" '          sh scripts/verify-perf-browser-identity.sh \' "pinned perf browser identity"
require_line "$identity" '            "$PERF_CHROME_PATH" \' "pinned perf browser identity"
require_line "$identity" '            "$PERF_CHROME_VERSION" \' "pinned perf browser identity"
require_line "$identity" "            build/perf-browser-identity.txt" "pinned perf browser identity"
forbid_step_key "$identity" "if" "pinned perf browser identity"
forbid_step_key "$identity" "continue-on-error" "pinned perf browser identity"

[ -f "$identity_verifier" ] || fail "identity verifier is missing: $identity_verifier"
identity_source="$(cat "$identity_verifier")"
require_line "$identity_source" 'expected_snapshot="1688711"' "identity verifier"
require_line "$identity_source" 'expected_version="154.0.8034.0"' "identity verifier"
require_line "$identity_source" 'if [ "$actual_status" -ne 0 ] || [ "$normalized_product" != "Chromium ${expected_version}" ]; then' "identity verifier"
require_line "$identity_source" '} | tee "$receipt"' "identity verifier"

budget_gate="$(extract_step 'Perf budget gate' "$browser_job")"
[ -n "$budget_gate" ] || fail "perf budget gate step is missing"
require_line "$budget_gate" '          CHROME_PATH: ${{ steps.perf-chrome.outputs.chrome-path }}' "perf budget gate"
require_line "$budget_gate" "        run: make perf-budget-ci" "perf budget gate"
forbid_text "$budget_gate" 'steps.chrome.outputs.chrome-path' "perf budget gate"
forbid_text "$budget_gate" "timeout" "perf budget gate"
forbid_step_key "$budget_gate" "if" "perf budget gate"
forbid_step_key "$budget_gate" "continue-on-error" "perf budget gate"

failure_upload="$(extract_step 'Upload perf budget failure diagnostics' "$browser_job")"
[ -n "$failure_upload" ] || fail "perf failure diagnostic upload step is missing"
require_line "$failure_upload" "        if: failure()" "perf failure diagnostic upload"
require_line "$failure_upload" "        uses: actions/upload-artifact@v4" "perf failure diagnostic upload"
require_line "$failure_upload" "            build/perf-report.json" "perf failure diagnostic upload"
require_line "$failure_upload" "            build/perf-server.log" "perf failure diagnostic upload"
require_line "$failure_upload" "            build/perf-browser-identity.txt" "perf failure diagnostic upload"
require_line "$failure_upload" "          if-no-files-found: ignore" "perf failure diagnostic upload"
require_line "$failure_upload" "          retention-days: 7" "perf failure diagnostic upload"
forbid_step_key "$failure_upload" "continue-on-error" "perf failure diagnostic upload"

require_count "$browser_job" "1688711" 1 "browser-tests pin isolation"
require_count "$browser_job" "154.0.8034.0" 0 "browser-tests version isolation"
require_count "$browser_job" 'steps.perf-chrome.outputs.chrome-path' 2 "pinned perf browser path isolation"
require_count "$browser_job" 'steps.perf-chrome.outputs.chrome-version' 1 "pinned perf browser version isolation"

# PR #308 adds this job on top of main. When present, hold its stable browser
# contract apart from the numeric perf fixture so rebasing this prerequisite
# cannot silently turn the renderer proof into the pinned perf environment.
stable_job="$(extract_job scene3d-v1-browser-renderer-proof)"
if [ -n "$stable_job" ]; then
	stable_setup="$(extract_step 'Set up stable Chrome for Testing' "$stable_job")"
	[ -n "$stable_setup" ] || fail "stable Scene3D renderer proof setup step is missing"
	require_line "$stable_setup" "        uses: browser-actions/setup-chrome@v2" "stable Scene3D renderer proof"
	require_line "$stable_setup" "          chrome-version: stable" "stable Scene3D renderer proof"
	forbid_text "$stable_job" "1688711" "stable Scene3D renderer proof"
	forbid_text "$stable_job" "154.0.8034.0" "stable Scene3D renderer proof"
	forbid_text "$stable_job" "perf-chrome" "stable Scene3D renderer proof"
fi

echo "check-perf-browser-pin: governed perf browser pin and lane separation passed"
