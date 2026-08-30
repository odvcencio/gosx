#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
checker="$repo_root/scripts/check-perf-browser-pin.sh"
workflow="$repo_root/.github/workflows/ci.yml"
tmp_dir="$(mktemp -d)"

cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

mutate() {
	name="$1"
	expression="$2"
	expected="$3"
	mutated="$tmp_dir/${name}.yml"
	output="$tmp_dir/${name}.out"
	sed "$expression" "$workflow" >"$mutated"
	if "$checker" "$mutated" >"$output" 2>&1; then
		echo "check-perf-browser-pin-test: mutation $name unexpectedly passed" >&2
		exit 1
	fi
	if ! grep -F -- "$expected" "$output" >/dev/null; then
		echo "check-perf-browser-pin-test: mutation $name failed without expected diagnostic: $expected" >&2
		cat "$output" >&2
		exit 1
	fi
}

"$checker" "$workflow" >/dev/null

# Model the stable renderer-proof job that PR #308 adds on top of main. The
# prerequisite must compose with it without leaking the numeric perf fixture.
stable_workflow="$tmp_dir/stable-proof.yml"
cp "$workflow" "$stable_workflow"
cat >>"$stable_workflow" <<'EOF'

  scene3d-v1-browser-renderer-proof:
    runs-on: ubuntu-latest
    steps:
      - name: Set up stable Chrome for Testing
        id: chrome
        uses: browser-actions/setup-chrome@v2
        with:
          chrome-version: stable
EOF
"$checker" "$stable_workflow" >/dev/null

stable_drift="$tmp_dir/stable-proof-drift.yml"
sed 's/chrome-version: stable/chrome-version: '\''1688711'\''/' "$stable_workflow" >"$stable_drift"
if "$checker" "$stable_drift" >"$tmp_dir/stable-proof-drift.out" 2>&1; then
	echo "check-perf-browser-pin-test: stable proof pin leakage unexpectedly passed" >&2
	exit 1
fi
if ! grep -F "stable Scene3D renderer proof missing required line:           chrome-version: stable" "$tmp_dir/stable-proof-drift.out" >/dev/null; then
	echo "check-perf-browser-pin-test: stable proof pin leakage lacked a causal diagnostic" >&2
	cat "$tmp_dir/stable-proof-drift.out" >&2
	exit 1
fi

mutate pin-drift \
	"s/chrome-version: '1688711'/chrome-version: latest/" \
	"pinned perf browser setup missing required line:           chrome-version: '1688711'"

mutate budget-uses-floating \
	'/^      - name: Perf budget gate$/,/^      - name: Upload perf budget failure diagnostics$/ s/CHROME_PATH: \${{ steps.perf-chrome.outputs.chrome-path }}/CHROME_PATH: \${{ steps.chrome.outputs.chrome-path }}/' \
	'perf budget gate missing required line:           CHROME_PATH: ${{ steps.perf-chrome.outputs.chrome-path }}'

mutate driver-uses-pinned \
	's/CHROME_PATH: \${{ steps.chrome.outputs.chrome-path }}/CHROME_PATH: \${{ steps.perf-chrome.outputs.chrome-path }}/' \
	'perf driver browser tests missing required line:           CHROME_PATH: ${{ steps.chrome.outputs.chrome-path }}'

mutate missing-receipt \
	'/build\/perf-browser-identity.txt/d' \
	'pinned perf browser identity missing required line:             build/perf-browser-identity.txt'

mutate timeout-prefix \
	's/timeout-minutes: 30/timeout-minutes: 300/' \
	'browser-tests job missing required line:     timeout-minutes: 30'

mutate pinned-action-prefix \
	's#browser-actions/setup-chrome@v2#browser-actions/setup-chrome@v20#' \
	'pinned perf browser setup missing required line:         uses: browser-actions/setup-chrome@v2'

mutate latest-action-prefix \
	's#browser-actions/setup-chrome@v1#browser-actions/setup-chrome@v10#' \
	'latest browser setup missing required line:         uses: browser-actions/setup-chrome@v1'

mutate budget-ignore-failure \
	's/run: make perf-budget-ci/run: make perf-budget-ci || true/' \
	'perf budget gate missing required line:         run: make perf-budget-ci'

mutate identity-ignore-failure \
	's#            build/perf-browser-identity.txt#            build/perf-browser-identity.txt || true#' \
	'pinned perf browser identity missing required line:             build/perf-browser-identity.txt'

mutate artifact-condition-suppressed \
	's/if: failure()/if: failure() \&\& false/' \
	'perf failure diagnostic upload missing required line:         if: failure()'

mutate retention-prefix \
	's/retention-days: 7/retention-days: 70/' \
	'perf failure diagnostic upload missing required line:           retention-days: 7'

mutate identity-continue-on-error \
	'/^      - name: Verify pinned perf browser identity$/a\
        continue-on-error: true' \
	'pinned perf browser identity contains forbidden control: continue-on-error'

mutate budget-continue-on-error \
	'/^      - name: Perf budget gate$/a\
        continue-on-error: true' \
	'perf budget gate contains forbidden control: continue-on-error'

mutate artifact-continue-on-error \
	'/^      - name: Upload perf budget failure diagnostics$/a\
        continue-on-error: true' \
	'perf failure diagnostic upload contains forbidden control: continue-on-error'

mutate identity-skipped \
	'/^      - name: Verify pinned perf browser identity$/a\
        if: false' \
	'pinned perf browser identity contains forbidden control: if'

mutate budget-skipped \
	'/^      - name: Perf budget gate$/a\
        if: false' \
	'perf budget gate contains forbidden control: if'

# A same-named step in another job must not shadow the governed browser-tests
# step. The checker used to extract the first matching step in the workflow.
shadowed_latest="$tmp_dir/shadowed-latest.yml"
awk '
	$0 == "  browser-tests:" {
		print "  shadow-browser:"
		print "    runs-on: ubuntu-latest"
		print "    steps:"
		print "      - name: Set up Chrome"
		print "        id: chrome"
		print "        uses: browser-actions/setup-chrome@v1"
		in_browser = 1
	}
	in_browser && $0 == "        uses: browser-actions/setup-chrome@v1" {
		print "        uses: browser-actions/setup-chrome@v10"
		in_browser = 0
		next
	}
	{ print }
' "$workflow" >"$shadowed_latest"
if "$checker" "$shadowed_latest" >"$tmp_dir/shadowed-latest.out" 2>&1; then
	echo "check-perf-browser-pin-test: shadowed latest action drift unexpectedly passed" >&2
	exit 1
fi
if ! grep -F "latest browser setup missing required line:         uses: browser-actions/setup-chrome@v1" "$tmp_dir/shadowed-latest.out" >/dev/null; then
	echo "check-perf-browser-pin-test: shadowed latest action drift lacked a causal diagnostic" >&2
	cat "$tmp_dir/shadowed-latest.out" >&2
	exit 1
fi

echo "check-perf-browser-pin-test: ok"
