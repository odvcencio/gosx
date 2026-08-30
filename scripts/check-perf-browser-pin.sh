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
	awk -v wanted="      - name: ${step}" '
		$0 == wanted { found = 1 }
		found && emitted && $0 ~ /^      - name: / { exit }
		found { print; emitted = 1 }
	' "$workflow"
}

require_text() {
	block="$1"
	needle="$2"
	label="$3"
	if ! printf '%s\n' "$block" | grep -F -- "$needle" >/dev/null; then
		fail "$label missing required text: $needle"
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
require_text "$browser_job" "    timeout-minutes: 30" "browser-tests job"

latest_setup="$(extract_step 'Set up Chrome')"
[ -n "$latest_setup" ] || fail "latest browser setup step is missing"
require_text "$latest_setup" "        id: chrome" "latest browser setup"
require_text "$latest_setup" "        uses: browser-actions/setup-chrome@v1" "latest browser setup"
forbid_text "$latest_setup" "chrome-version:" "latest browser setup"

driver_tests="$(extract_step 'Perf driver browser tests')"
[ -n "$driver_tests" ] || fail "perf driver browser test step is missing"
require_text "$driver_tests" '          CHROME_PATH: ${{ steps.chrome.outputs.chrome-path }}' "perf driver browser tests"
forbid_text "$driver_tests" "perf-chrome" "perf driver browser tests"

pinned_setup="$(extract_step 'Set up pinned Chromium for perf budget')"
[ -n "$pinned_setup" ] || fail "pinned perf browser setup step is missing"
require_text "$pinned_setup" "        id: perf-chrome" "pinned perf browser setup"
require_text "$pinned_setup" "        uses: browser-actions/setup-chrome@v2" "pinned perf browser setup"
require_text "$pinned_setup" "          chrome-version: '1688711'" "pinned perf browser setup"

identity="$(extract_step 'Verify pinned perf browser identity')"
[ -n "$identity" ] || fail "pinned perf browser identity step is missing"
require_text "$identity" '          PERF_CHROME_PATH: ${{ steps.perf-chrome.outputs.chrome-path }}' "pinned perf browser identity"
require_text "$identity" '          PERF_CHROME_VERSION: ${{ steps.perf-chrome.outputs.chrome-version }}' "pinned perf browser identity"
require_text "$identity" '          sh scripts/verify-perf-browser-identity.sh \' "pinned perf browser identity"
require_text "$identity" '            "$PERF_CHROME_PATH" \' "pinned perf browser identity"
require_text "$identity" '            "$PERF_CHROME_VERSION" \' "pinned perf browser identity"
require_text "$identity" "            build/perf-browser-identity.txt" "pinned perf browser identity"

[ -f "$identity_verifier" ] || fail "identity verifier is missing: $identity_verifier"
identity_source="$(cat "$identity_verifier")"
require_text "$identity_source" 'expected_snapshot="1688711"' "identity verifier"
require_text "$identity_source" 'expected_version="154.0.8034.0"' "identity verifier"
require_text "$identity_source" '"$normalized_product" != "Chromium ${expected_version}"' "identity verifier"
require_text "$identity_source" '} | tee "$receipt"' "identity verifier"

budget_gate="$(extract_step 'Perf budget gate')"
[ -n "$budget_gate" ] || fail "perf budget gate step is missing"
require_text "$budget_gate" '          CHROME_PATH: ${{ steps.perf-chrome.outputs.chrome-path }}' "perf budget gate"
require_text "$budget_gate" "        run: make perf-budget-ci" "perf budget gate"
forbid_text "$budget_gate" 'steps.chrome.outputs.chrome-path' "perf budget gate"
forbid_text "$budget_gate" "timeout" "perf budget gate"

failure_upload="$(extract_step 'Upload perf budget failure diagnostics')"
[ -n "$failure_upload" ] || fail "perf failure diagnostic upload step is missing"
require_text "$failure_upload" "        if: failure()" "perf failure diagnostic upload"
require_text "$failure_upload" "        uses: actions/upload-artifact@v4" "perf failure diagnostic upload"
require_text "$failure_upload" "            build/perf-report.json" "perf failure diagnostic upload"
require_text "$failure_upload" "            build/perf-server.log" "perf failure diagnostic upload"
require_text "$failure_upload" "            build/perf-browser-identity.txt" "perf failure diagnostic upload"
require_text "$failure_upload" "          if-no-files-found: ignore" "perf failure diagnostic upload"
require_text "$failure_upload" "          retention-days: 7" "perf failure diagnostic upload"

require_count "$browser_job" "1688711" 1 "browser-tests pin isolation"
require_count "$browser_job" "154.0.8034.0" 0 "browser-tests version isolation"
require_count "$browser_job" 'steps.perf-chrome.outputs.chrome-path' 2 "pinned perf browser path isolation"
require_count "$browser_job" 'steps.perf-chrome.outputs.chrome-version' 1 "pinned perf browser version isolation"

# PR #308 adds this job on top of main. When present, hold its stable browser
# contract apart from the numeric perf fixture so rebasing this prerequisite
# cannot silently turn the renderer proof into the pinned perf environment.
stable_job="$(extract_job scene3d-v1-browser-renderer-proof)"
if [ -n "$stable_job" ]; then
	require_text "$stable_job" "browser-actions/setup-chrome@v2" "stable Scene3D renderer proof"
	require_text "$stable_job" "chrome-version: stable" "stable Scene3D renderer proof"
	forbid_text "$stable_job" "1688711" "stable Scene3D renderer proof"
	forbid_text "$stable_job" "154.0.8034.0" "stable Scene3D renderer proof"
	forbid_text "$stable_job" "perf-chrome" "stable Scene3D renderer proof"
fi

echo "check-perf-browser-pin: governed perf browser pin and lane separation passed"
