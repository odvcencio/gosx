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
if ! grep -F "stable Scene3D renderer proof missing required text: chrome-version: stable" "$tmp_dir/stable-proof-drift.out" >/dev/null; then
	echo "check-perf-browser-pin-test: stable proof pin leakage lacked a causal diagnostic" >&2
	cat "$tmp_dir/stable-proof-drift.out" >&2
	exit 1
fi

mutate pin-drift \
	"s/chrome-version: '1688711'/chrome-version: latest/" \
	"pinned perf browser setup missing required text:           chrome-version: '1688711'"

mutate budget-uses-floating \
	'/^      - name: Perf budget gate$/,/^      - name: Upload perf budget failure diagnostics$/ s/CHROME_PATH: \${{ steps.perf-chrome.outputs.chrome-path }}/CHROME_PATH: \${{ steps.chrome.outputs.chrome-path }}/' \
	'perf budget gate missing required text:           CHROME_PATH: ${{ steps.perf-chrome.outputs.chrome-path }}'

mutate driver-uses-pinned \
	's/CHROME_PATH: \${{ steps.chrome.outputs.chrome-path }}/CHROME_PATH: \${{ steps.perf-chrome.outputs.chrome-path }}/' \
	'perf driver browser tests missing required text:           CHROME_PATH: ${{ steps.chrome.outputs.chrome-path }}'

mutate missing-receipt \
	'/build\/perf-browser-identity.txt/d' \
	'pinned perf browser identity missing required text:             build/perf-browser-identity.txt'

echo "check-perf-browser-pin-test: ok"
