package perfbrowserpin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRepositoryWorkflow(t *testing.T) {
	workflow := repositoryWorkflow(t)
	if err := Validate(workflow); err != nil {
		t.Fatalf("Validate(repository workflow): %v", err)
	}
}

func TestStructuralMutationsFailClosed(t *testing.T) {
	base := string(repositoryWorkflow(t))
	tests := []struct {
		name   string
		mutate func(*testing.T, string) string
		want   string
	}{
		{
			name: "numeric pin drift",
			mutate: replace(
				"          chrome-version: '1688711'",
				"          chrome-version: latest",
			),
			want: "pinned perf browser setup.with.chrome-version",
		},
		{
			name: "browser checkout redirects to main",
			mutate: replaceBrowserOnce(
				"      - name: Check out repository\n        uses: actions/checkout@v4\n",
				"      - name: Check out repository\n        uses: actions/checkout@v4\n        with:\n          ref: main\n",
			),
			want: "repository checkout: unexpected field \"with\"",
		},
		{
			name: "budget uses floating browser",
			mutate: replace(
				"      - name: Perf budget gate\n        env:\n          CHROME_PATH: ${{ steps.perf-chrome.outputs.chrome-path }}",
				"      - name: Perf budget gate\n        env:\n          CHROME_PATH: ${{ steps.chrome.outputs.chrome-path }}",
			),
			want: "perf budget gate.env.CHROME_PATH",
		},
		{
			name: "driver uses pinned browser",
			mutate: replace(
				"      - name: Perf driver browser tests\n        env:\n          CHROME_PATH: ${{ steps.chrome.outputs.chrome-path }}",
				"      - name: Perf driver browser tests\n        env:\n          CHROME_PATH: ${{ steps.perf-chrome.outputs.chrome-path }}",
			),
			want: "perf driver browser tests.env.CHROME_PATH",
		},
		{
			name: "identity receipt path drift",
			mutate: replace(
				"            \"$PERF_CHROME_VERSION\" \\\n            build/perf-browser-identity.txt",
				"            \"$PERF_CHROME_VERSION\" \\\n            build/not-the-governed-receipt.txt",
			),
			want: "pinned perf browser identity.run",
		},
		{
			name:   "timeout prefix",
			mutate: replace("    timeout-minutes: 30", "    timeout-minutes: 300"),
			want:   "browser-tests job.timeout-minutes",
		},
		{
			name: "pinned action prefix",
			mutate: replace(
				"      - name: Set up pinned Chromium for perf budget\n        id: perf-chrome\n        uses: browser-actions/setup-chrome@v2",
				"      - name: Set up pinned Chromium for perf budget\n        id: perf-chrome\n        uses: browser-actions/setup-chrome@v20",
			),
			want: "pinned perf browser setup.uses",
		},
		{
			name:   "latest action prefix",
			mutate: replace("        uses: browser-actions/setup-chrome@v1", "        uses: browser-actions/setup-chrome@v10"),
			want:   "latest browser setup.uses",
		},
		{
			name:   "budget ignores failure",
			mutate: replace("        run: make perf-budget-ci", "        run: make perf-budget-ci || true"),
			want:   "perf budget gate.run",
		},
		{
			name: "identity ignores failure",
			mutate: replace(
				"            build/perf-browser-identity.txt\n\n      - name: Perf budget gate",
				"            build/perf-browser-identity.txt || true\n\n      - name: Perf budget gate",
			),
			want: "pinned perf browser identity.run",
		},
		{
			name:   "artifact condition suppressed",
			mutate: replace("        if: failure()", "        if: failure() && false"),
			want:   "perf failure diagnostic upload.if",
		},
		{
			name: "retention prefix",
			mutate: replace(
				"          if-no-files-found: ignore\n          retention-days: 7\n\n  # test:",
				"          if-no-files-found: ignore\n          retention-days: 70\n\n  # test:",
			),
			want: "perf failure diagnostic upload.with.retention-days",
		},
		{
			name: "identity continue on error",
			mutate: insertAfter(
				"      - name: Verify pinned perf browser identity\n",
				"        continue-on-error: true\n",
			),
			want: "pinned perf browser identity: unexpected field \"continue-on-error\"",
		},
		{
			name: "budget continue on error",
			mutate: insertAfter(
				"      - name: Perf budget gate\n",
				"        continue-on-error: true\n",
			),
			want: "perf budget gate: unexpected field \"continue-on-error\"",
		},
		{
			name: "artifact continue on error",
			mutate: insertAfter(
				"      - name: Upload perf budget failure diagnostics\n",
				"        continue-on-error: true\n",
			),
			want: "perf failure diagnostic upload: unexpected field \"continue-on-error\"",
		},
		{
			name: "identity skipped",
			mutate: insertAfter(
				"      - name: Verify pinned perf browser identity\n",
				"        if: false\n",
			),
			want: "pinned perf browser identity: unexpected field \"if\"",
		},
		{
			name: "budget skipped",
			mutate: insertAfter(
				"      - name: Perf budget gate\n",
				"        if: false\n",
			),
			want: "perf budget gate: unexpected field \"if\"",
		},
		{
			name:   "earlier job shadow",
			mutate: shadowLatestSetup,
			want:   "latest browser setup.uses",
		},
		{
			name: "pin displaced into env decoy",
			mutate: replace(
				"        with:\n          chrome-version: '1688711'\n\n      - name: Verify pinned perf browser identity",
				"        with:\n          chrome-version: latest\n        env:\n          chrome-version: '1688711'\n\n      - name: Verify pinned perf browser identity",
			),
			want: "pinned perf browser setup: unexpected field \"env\"",
		},
		{
			name: "quoted identity skip",
			mutate: insertAfter(
				"      - name: Verify pinned perf browser identity\n",
				"        \"if\": false\n",
			),
			want: "pinned perf browser identity: unexpected field \"if\"",
		},
		{
			name: "quoted budget continue on error",
			mutate: insertAfter(
				"      - name: Perf budget gate\n",
				"        \"continue-on-error\": true\n",
			),
			want: "perf budget gate: unexpected field \"continue-on-error\"",
		},
		{
			name: "browser job skipped",
			mutate: insertAfter(
				"  browser-tests:\n",
				"    if: false\n",
			),
			want: "browser-tests job: unexpected field \"if\"",
		},
		{
			name: "browser job continue on error",
			mutate: insertAfter(
				"  browser-tests:\n",
				"    continue-on-error: true\n",
			),
			want: "browser-tests job: unexpected field \"continue-on-error\"",
		},
		{
			name: "identity custom shell masks exit",
			mutate: insertAfter(
				"      - name: Verify pinned perf browser identity\n",
				"        shell: bash -c '{0}; exit 0'\n",
			),
			want: "pinned perf browser identity: unexpected field \"shell\"",
		},
		{
			name: "budget custom shell masks exit",
			mutate: insertAfter(
				"      - name: Perf budget gate\n",
				"        shell: bash -c '{0}; exit 0'\n",
			),
			want: "perf budget gate: unexpected field \"shell\"",
		},
		{
			name:   "artifact path displaced into env decoy",
			mutate: displaceArtifactReceipt,
			want:   "perf failure diagnostic upload: unexpected field \"env\"",
		},
		{
			name:   "artifact retention displaced into env decoy",
			mutate: displaceArtifactRetention,
			want:   "perf failure diagnostic upload: unexpected field \"env\"",
		},
		{
			name: "aggregate browser need removed",
			mutate: replace(
				"      - browser-tests\n",
				"",
			),
			want: "aggregate test job.needs",
		},
		{
			name: "latest driver skipped",
			mutate: insertAfter(
				"      - name: Perf driver browser tests\n",
				"        if: false\n",
			),
			want: "perf driver browser tests: unexpected field \"if\"",
		},
		{
			name: "browser docs skipped",
			mutate: insertAfter(
				"      - name: Browser docs E2E gate\n",
				"        if: false\n",
			),
			want: "browser docs E2E gate: unexpected field \"if\"",
		},
		{
			name: "pre-driver BASH_ENV injection",
			mutate: insertAfterBrowser(
				"        run: scripts/install-ci-tinygo.sh\n",
				"\n      - name: Poison later shells\n        run: |\n          printf '%s\\n' 'sh() { :; }' 'make() { :; }' > /tmp/fail-open-shell-prelude\n          printf '%s\\n' 'BASH_ENV=/tmp/fail-open-shell-prelude' >> \"$GITHUB_ENV\"\n",
			),
			want: "browser-tests job.steps: got 12 steps, want exact governed roster of 11",
		},
		{
			name: "governed ordering interrupted",
			mutate: insertAfter(
				"        run: make test-perf-browser\n",
				"\n      - name: Unreviewed interposed browser step\n        run: echo interposed\n",
			),
			want: "exact governed roster",
		},
		{
			name: "duplicate governed step name",
			mutate: replace(
				"      - name: Browser docs E2E gate",
				"      - name: Set up Chrome",
			),
			want: "duplicate step name \"Set up Chrome\"",
		},
		{
			name: "aggregate job skipped",
			mutate: insertAfter(
				"  test:\n",
				"    \"if\": false\n",
			),
			want: "aggregate test job: unexpected field \"if\"",
		},
		{
			name: "workflow default shell masks exits",
			mutate: replace(
				"\njobs:\n",
				"\ndefaults:\n  run:\n    shell: bash -c '{0}; exit 0'\n\njobs:\n",
			),
			want: "workflow: forbidden inherited execution field \"defaults\"",
		},
		{
			name: "workflow environment injects shell prelude",
			mutate: replace(
				"\njobs:\n",
				"\nenv:\n  BASH_ENV: /tmp/fail-open-shell-prelude\n\njobs:\n",
			),
			want: "workflow: forbidden inherited execution field \"env\"",
		},
		{
			name: "browser job environment injects shell prelude",
			mutate: insertAfter(
				"  browser-tests:\n",
				"    env:\n      BASH_ENV: /tmp/fail-open-shell-prelude\n",
			),
			want: "browser-tests job: unexpected field \"env\"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := test.mutate(t, base)
			assertCausalRejection(t, mutated, test.want)
		})
	}
}

func TestStableRendererProofComposition(t *testing.T) {
	stable := ensureStableProof(t, string(repositoryWorkflow(t)))
	if err := Validate([]byte(stable)); err != nil {
		t.Fatalf("Validate(stable composition): %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*testing.T, string) string
		want   string
	}{
		{
			name:   "numeric fixture leaks into stable lane",
			mutate: replace("          chrome-version: stable", "          chrome-version: '1688711'"),
			want:   "stable Scene3D renderer proof setup.with.chrome-version",
		},
		{
			name: "stable input displaced into env decoy",
			mutate: replace(
				"        with:\n          chrome-version: stable\n",
				"        with:\n          chrome-version: latest\n        env:\n          chrome-version: stable\n",
			),
			want: "stable Scene3D renderer proof setup: unexpected field \"env\"",
		},
		{
			name: "stable job skipped",
			mutate: insertAfter(
				"  scene3d-v1-browser-renderer-proof:\n",
				"    if: false\n",
			),
			want: "stable Scene3D renderer proof job: unexpected field \"if\"",
		},
		{
			name: "stable checkout redirects to main",
			mutate: replaceStableOnce(
				"      - name: Check out repository\n        uses: actions/checkout@v4\n",
				"      - name: Check out repository\n        uses: actions/checkout@v4\n        with:\n          ref: main\n",
			),
			want: "repository checkout: unexpected field \"with\"",
		},
		{
			name: "stable renderer proof skipped",
			mutate: insertAfter(
				"      - name: Run Scene3D CUBICSPLINE browser renderer proof\n",
				"        if: false\n",
			),
			want: "stable Scene3D renderer proof: unexpected field \"if\"",
		},
		{
			name: "stable job environment injects shell prelude",
			mutate: insertAfter(
				"  scene3d-v1-browser-renderer-proof:\n",
				"    env:\n      BASH_ENV: /tmp/fail-open-shell-prelude\n",
			),
			want: "stable Scene3D renderer proof job: unexpected field \"env\"",
		},
		{
			name: "stable aggregate need removed",
			mutate: replace(
				"      - scene3d-v1-browser-renderer-proof\n",
				"",
			),
			want: "aggregate test job.needs",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertCausalRejection(t, test.mutate(t, stable), test.want)
		})
	}
}

func repositoryWorkflow(t *testing.T) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", ".github", "workflows", "ci.yml")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return source
}

func assertCausalRejection(t *testing.T, mutated, want string) {
	t.Helper()
	if _, err := decode([]byte(mutated)); err != nil {
		t.Fatalf("mutation is not valid YAML: %v", err)
	}
	err := Validate([]byte(mutated))
	if err == nil {
		t.Fatal("mutation unexpectedly passed structural validation")
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("rejection %q does not contain causal diagnostic %q", err, want)
	}
}

func replace(old, replacement string) func(*testing.T, string) string {
	return func(t *testing.T, source string) string {
		t.Helper()
		return replaceOnce(t, source, old, replacement)
	}
}

func insertAfter(anchor, insertion string) func(*testing.T, string) string {
	return replace(anchor, anchor+insertion)
}

func replaceBrowserOnce(old, replacement string) func(*testing.T, string) string {
	return func(t *testing.T, source string) string {
		t.Helper()
		browserStart := strings.Index(source, "  browser-tests:\n")
		if browserStart < 0 {
			t.Fatal("browser-tests job is missing")
		}
		relativeEnd := strings.Index(source[browserStart:], "\n  # test:")
		if relativeEnd < 0 {
			t.Fatal("browser-tests job end is missing")
		}
		browserEnd := browserStart + relativeEnd
		return source[:browserStart] + replaceOnce(t, source[browserStart:browserEnd], old, replacement) + source[browserEnd:]
	}
}

func insertAfterBrowser(anchor, insertion string) func(*testing.T, string) string {
	return replaceBrowserOnce(anchor, anchor+insertion)
}

func replaceStableOnce(old, replacement string) func(*testing.T, string) string {
	return func(t *testing.T, source string) string {
		t.Helper()
		stableStart := strings.Index(source, "  scene3d-v1-browser-renderer-proof:\n")
		if stableStart < 0 {
			t.Fatal("stable Scene3D renderer proof job is missing")
		}
		return source[:stableStart] + replaceOnce(t, source[stableStart:], old, replacement)
	}
}

func replaceOnce(t *testing.T, source, old, replacement string) string {
	t.Helper()
	if got := strings.Count(source, old); got != 1 {
		t.Fatalf("mutation anchor occurs %d times, want 1: %q", got, old)
	}
	return strings.Replace(source, old, replacement, 1)
}

func shadowLatestSetup(t *testing.T, source string) string {
	t.Helper()
	shadow := "  shadow-browser:\n" +
		"    runs-on: ubuntu-latest\n" +
		"    steps:\n" +
		"      - name: Set up Chrome\n" +
		"        id: chrome\n" +
		"        uses: browser-actions/setup-chrome@v1\n\n"
	source = replaceOnce(t, source, "  browser-tests:\n", shadow+"  browser-tests:\n")
	browserStart := strings.Index(source, "  browser-tests:\n")
	if browserStart < 0 {
		t.Fatal("browser-tests job missing after shadow insertion")
	}
	return source[:browserStart] + replaceOnce(t, source[browserStart:],
		"        uses: browser-actions/setup-chrome@v1",
		"        uses: browser-actions/setup-chrome@v10",
	)
}

func displaceArtifactReceipt(t *testing.T, source string) string {
	t.Helper()
	source = replaceOnce(t, source,
		"            build/perf-server.log\n            build/perf-browser-identity.txt\n",
		"            build/perf-server.log\n",
	)
	return replaceOnce(t, source,
		"          retention-days: 7\n\n  # test:",
		"          retention-days: 7\n        env:\n          DECOY: |\n            build/perf-browser-identity.txt\n\n  # test:",
	)
}

func displaceArtifactRetention(t *testing.T, source string) string {
	t.Helper()
	return replaceOnce(t, source,
		"          retention-days: 7\n\n  # test:",
		"          retention-days: 70\n        env:\n          retention-days: 7\n\n  # test:",
	)
}

func ensureStableProof(t *testing.T, source string) string {
	t.Helper()
	if strings.Contains(source, "  scene3d-v1-browser-renderer-proof:\n") {
		return source
	}
	source = replaceOnce(t, source,
		"      - browser-tests\n",
		"      - scene3d-v1-browser-renderer-proof\n      - browser-tests\n",
	)
	return source + `

  scene3d-v1-browser-renderer-proof:
    runs-on: ubuntu-latest
    timeout-minutes: 10

    steps:
      - name: Check out repository
        uses: actions/checkout@v4

      - name: Set up stable Chrome for Testing
        id: chrome
        uses: browser-actions/setup-chrome@v2
        with:
          chrome-version: stable

      - name: Set up Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '22'

      - name: Run Scene3D CUBICSPLINE browser renderer proof
        env:
          GOSX_CHROME_BIN: ${{ steps.chrome.outputs.chrome-path }}
          GOSX_EXPECTED_CHROME_VERSION: ${{ steps.chrome.outputs.chrome-version }}
          GOSX_SCENE3D_CUBIC_WEBGPU_TARGET: private-texture
        run: |
          set -eu
          if [ -z "${RUNNER_TEMP:-}" ] || [ "$RUNNER_TEMP" = "/" ]; then
            echo "unsafe RUNNER_TEMP" >&2
            exit 2
          fi
          artifact_dir="${RUNNER_TEMP}/gosx-cubic-proof"
          rm -rf -- "$artifact_dir"
          mkdir -p "$artifact_dir"
          proof_status=0
          trap 'if [ "$proof_status" -eq 0 ]; then rm -rf -- "$artifact_dir"; fi' EXIT
          set +e
          node client/js/testdata/scene3d-cubic-spline-browser-matrix.cjs "$GITHUB_WORKSPACE" "$artifact_dir"
          proof_status=$?
          set -e
          if [ "$proof_status" -ne 0 ] && [ -f "$artifact_dir/matrix-report.json" ]; then
            cat "$artifact_dir/matrix-report.json"
          fi
          exit "$proof_status"

      - name: Upload Scene3D proof diagnostics
        if: ${{ failure() }}
        uses: actions/upload-artifact@v4
        with:
          name: scene3d-v1-cubic-proof-${{ github.run_id }}-${{ github.run_attempt }}
          path: ${{ runner.temp }}/gosx-cubic-proof
          if-no-files-found: ignore
          retention-days: 7

      - name: Clean Scene3D proof artifacts
        if: ${{ always() }}
        run: |
          set -eu
          if [ -z "${RUNNER_TEMP:-}" ] || [ "$RUNNER_TEMP" = "/" ]; then
            echo "unsafe RUNNER_TEMP" >&2
            exit 2
          fi
          rm -rf -- "${RUNNER_TEMP}/gosx-cubic-proof"
`
}
