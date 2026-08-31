package perfbrowserpin

import (
	"os"
	"os/exec"
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
			name: "driver uses ungoverned browser output",
			mutate: replace(
				"      - name: Perf driver browser tests\n        env:\n          CHROME_PATH: ${{ steps.chrome.outputs.chrome-path }}",
				"      - name: Perf driver browser tests\n        env:\n          CHROME_PATH: ${{ steps.chrome.outputs.chrome-version }}",
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
			name:   "aggregate browser need removed",
			mutate: removeBrowserAggregateContract,
			want:   "aggregate test job.needs",
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
			name: "aggregate always weakened",
			mutate: replace(
				"    if: ${{ always() }}\n    runs-on: ubuntu-latest\n    needs:",
				"    if: ${{ success() }}\n    runs-on: ubuntu-latest\n    needs:",
			),
			want: "aggregate test job.if",
		},
		{
			name: "aggregate always omitted",
			mutate: replace(
				"  test:\n    if: ${{ always() }}\n    runs-on:",
				"  test:\n    runs-on:",
			),
			want: "aggregate test job: missing field \"if\"",
		},
		{
			name: "aggregate always made conditional",
			mutate: replace(
				"    if: ${{ always() }}\n    runs-on: ubuntu-latest\n    needs:",
				"    if: ${{ always() && needs.browser-tests.result == 'success' }}\n    runs-on: ubuntu-latest\n    needs:",
			),
			want: "aggregate test job.if",
		},
		{
			name: "aggregate needs reordered",
			mutate: replace(
				"      - go-tests\n      - go-race-tests\n",
				"      - go-race-tests\n      - go-tests\n",
			),
			want: "aggregate test job.needs",
		},
		{
			name: "aggregate result binding omitted",
			mutate: replace(
				"          GO_RACE_TESTS_RESULT: ${{ needs.go-race-tests.result }}\n",
				"",
			),
			want: "aggregate dependency assertion.env: missing field \"GO_RACE_TESTS_RESULT\"",
		},
		{
			name: "aggregate result binding redirected",
			mutate: replace(
				"          GO_TESTS_RESULT: ${{ needs.go-tests.result }}\n",
				"          GO_TESTS_RESULT: ${{ needs.browser-tests.result }}\n",
			),
			want: "aggregate dependency assertion.env.GO_TESTS_RESULT",
		},
		{
			name: "aggregate failure exit weakened",
			mutate: replace(
				"                exit 1\n                ;;\n",
				"                exit 0\n                ;;\n",
			),
			want: "aggregate dependency assertion.run",
		},
		{
			name: "aggregate skipped result allowed",
			mutate: replace(
				"              *=success) ;;\n",
				"              *=success|*=skipped) ;;\n",
			),
			want: "aggregate dependency assertion.run",
		},
		{
			name: "aggregate assertion reordered",
			mutate: replace(
				"            \"go-tests=$GO_TESTS_RESULT\" \\\n            \"go-race-tests=$GO_RACE_TESTS_RESULT\" \\\n",
				"            \"go-race-tests=$GO_RACE_TESTS_RESULT\" \\\n            \"go-tests=$GO_TESTS_RESULT\" \\\n",
			),
			want: "aggregate dependency assertion.run",
		},
		{
			name: "aggregate assertion continue on error",
			mutate: insertAfter(
				"      - name: All test jobs passed\n",
				"        continue-on-error: true\n",
			),
			want: "aggregate dependency assertion: unexpected field \"continue-on-error\"",
		},
		{
			name: "aggregate assertion skipped",
			mutate: insertAfter(
				"      - name: All test jobs passed\n",
				"        if: false\n",
			),
			want: "aggregate dependency assertion: unexpected field \"if\"",
		},
		{
			name: "aggregate assertion custom shell",
			mutate: insertAfter(
				"      - name: All test jobs passed\n",
				"        shell: bash -c '{0}; exit 0'\n",
			),
			want: "aggregate dependency assertion: unexpected field \"shell\"",
		},
		{
			name:   "aggregate extra bypass step",
			mutate: insertAggregateBypassStep,
			want:   "aggregate test job.steps: got 2 steps, want exact governed roster of 1",
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
	assertActionlintValid(t, stable)
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
			mutate: replaceStableOnce("          chrome-version: stable", "          chrome-version: '1688711'"),
			want:   "stable Scene3D renderer proof setup.with.chrome-version",
		},
		{
			name: "stable input displaced into env decoy",
			mutate: replaceStableOnce(
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
			name:   "stable aggregate need removed",
			mutate: removeStableAggregateContract,
			want:   "aggregate test job.needs",
		},
		{
			name: "stable aggregate need reordered",
			mutate: replace(
				"      - scene3d-v1-browser-renderer-proof\n      - scene3d-v1-adapter-proof\n",
				"      - scene3d-v1-adapter-proof\n      - scene3d-v1-browser-renderer-proof\n",
			),
			want: "aggregate test job.needs",
		},
		{
			name: "stable aggregate result omitted",
			mutate: replace(
				"          SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT: ${{ needs.scene3d-v1-browser-renderer-proof.result }}\n",
				"",
			),
			want: "aggregate dependency assertion.env: missing field \"SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT\"",
		},
		{
			name: "stable aggregate result redirected",
			mutate: replace(
				"          SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT: ${{ needs.scene3d-v1-browser-renderer-proof.result }}\n",
				"          SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT: ${{ needs.browser-tests.result }}\n",
			),
			want: "aggregate dependency assertion.env.SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT",
		},
		{
			name: "stable aggregate skipped result allowed",
			mutate: replace(
				"              *=success) ;;\n",
				"              *=success|scene3d-v1-browser-renderer-proof=skipped) ;;\n",
			),
			want: "aggregate dependency assertion.run",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertCausalRejection(t, test.mutate(t, stable), test.want)
		})
	}
}

func TestAdapterProofComposition(t *testing.T) {
	base := string(repositoryWorkflow(t))
	assertActionlintValid(t, base)
	if err := Validate([]byte(base)); err != nil {
		t.Fatalf("Validate(adapter composition): %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*testing.T, string) string
		want   string
	}{
		{
			name:   "adapter job and aggregate removed together",
			mutate: removeAdapterJobAndAggregate,
			want:   "workflow.jobs: scene3d-v1-adapter-proof job is missing",
		},
		{
			name: "adapter setup action regresses",
			mutate: replaceAdapterOnce(
				"        uses: browser-actions/setup-chrome@v2",
				"        uses: browser-actions/setup-chrome@v1",
			),
			want: "Scene3D adapter proof browser setup.uses",
		},
		{
			name: "adapter stable input displaced into env decoy",
			mutate: replaceAdapterOnce(
				"        with:\n          chrome-version: stable\n",
				"        with:\n          chrome-version: latest\n        env:\n          chrome-version: stable\n",
			),
			want: "Scene3D adapter proof browser setup: unexpected field \"env\"",
		},
		{
			name: "adapter job skipped",
			mutate: insertAfter(
				"  scene3d-v1-adapter-proof:\n",
				"    if: false\n",
			),
			want: "Scene3D adapter proof job: unexpected field \"if\"",
		},
		{
			name: "adapter checkout redirects to main",
			mutate: replaceAdapterOnce(
				"      - name: Check out repository\n        uses: actions/checkout@v4\n",
				"      - name: Check out repository\n        uses: actions/checkout@v4\n        with:\n          ref: main\n",
			),
			want: "repository checkout: unexpected field \"with\"",
		},
		{
			name: "adapter proof skipped",
			mutate: insertAfter(
				"      - name: Run Scene3D adapter hydrate browser proof\n",
				"        if: false\n",
			),
			want: "Scene3D adapter proof: unexpected field \"if\"",
		},
		{
			name: "adapter expected browser version omitted",
			mutate: replaceAdapterOnce(
				"          GOSX_EXPECTED_CHROME_VERSION: ${{ steps.chrome.outputs.chrome-version }}\n",
				"",
			),
			want: "Scene3D adapter proof.env: missing field \"GOSX_EXPECTED_CHROME_VERSION\"",
		},
		{
			name: "adapter upload condition suppressed",
			mutate: replaceAdapterOnce(
				"        if: ${{ failure() }}",
				"        if: ${{ failure() && false }}",
			),
			want: "Scene3D adapter diagnostic upload.if",
		},
		{
			name: "adapter cleanup condition weakened",
			mutate: replaceAdapterOnce(
				"        if: ${{ always() }}",
				"        if: ${{ success() }}",
			),
			want: "Scene3D adapter artifact cleanup.if",
		},
		{
			name: "adapter extra shell step",
			mutate: replaceAdapterOnce(
				"      - name: Clean Scene3D adapter proof artifacts\n",
				"      - name: Unreviewed adapter prelude\n        run: echo bypass\n\n      - name: Clean Scene3D adapter proof artifacts\n",
			),
			want: "Scene3D adapter proof job.steps: got 8 steps, want exact governed roster of 7",
		},
		{
			name:   "adapter aggregate need removed",
			mutate: removeAdapterAggregateContract,
			want:   "aggregate test job.needs",
		},
		{
			name: "adapter aggregate result redirected",
			mutate: replace(
				"          SCENE3D_V1_ADAPTER_PROOF_RESULT: ${{ needs.scene3d-v1-adapter-proof.result }}\n",
				"          SCENE3D_V1_ADAPTER_PROOF_RESULT: ${{ needs.browser-tests.result }}\n",
			),
			want: "aggregate dependency assertion.env.SCENE3D_V1_ADAPTER_PROOF_RESULT",
		},
		{
			name: "adapter skipped result allowed",
			mutate: replace(
				"              *=success) ;;\n",
				"              *=success|scene3d-v1-adapter-proof=skipped) ;;\n",
			),
			want: "aggregate dependency assertion.run",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertCausalRejection(t, test.mutate(t, base), test.want)
		})
	}
}

func TestAggregateRunFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("aggregate shell contract runs on the governed ubuntu-latest job")
	}
	for _, hasStableJob := range []bool{false, true} {
		needs := aggregateNeeds(hasStableJob)
		name := "base"
		if hasStableJob {
			name = "stable-composition"
		}
		t.Run(name+"/success", func(t *testing.T) {
			output, err := runAggregate(t, needs, "", "")
			if err != nil {
				t.Fatalf("aggregate success: %v\n%s", err, output)
			}
		})
		for _, need := range needs {
			for _, result := range []string{"failure", "cancelled", "skipped", ""} {
				need, result := need, result
				t.Run(name+"/"+need+"/"+result, func(t *testing.T) {
					output, err := runAggregate(t, needs, need, result)
					if err == nil {
						t.Fatalf("aggregate unexpectedly accepted %s=%q\n%s", need, result, output)
					}
					if want := "required test job did not succeed: " + need + "=" + result; !strings.Contains(output, want) {
						t.Fatalf("aggregate output %q does not contain %q", output, want)
					}
				})
			}
		}
	}
}

func runAggregate(t *testing.T, needs []string, changedNeed, changedResult string) (string, error) {
	t.Helper()
	command := exec.Command("sh", "-c", aggregateRun(needs))
	command.Env = append([]string{}, os.Environ()...)
	for _, need := range needs {
		result := "success"
		if need == changedNeed {
			result = changedResult
		}
		command.Env = append(command.Env, aggregateResultEnv(need)+"="+result)
	}
	output, err := command.CombinedOutput()
	return string(output), err
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
	assertActionlintValid(t, mutated)
	err := Validate([]byte(mutated))
	if err == nil {
		t.Fatal("mutation unexpectedly passed structural validation")
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("rejection %q does not contain causal diagnostic %q", err, want)
	}
}

func assertActionlintValid(t *testing.T, workflow string) {
	t.Helper()
	actionlint, err := exec.LookPath("actionlint")
	if err != nil {
		return
	}
	command := exec.Command(actionlint, "-")
	command.Stdin = strings.NewReader(workflow)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("mutation must remain actionlint-valid: %v\n%s", err, output)
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
		stableEnd := len(source)
		if relativeEnd := strings.Index(source[stableStart:], "\n  # scene3d-v1-adapter-proof:"); relativeEnd >= 0 {
			stableEnd = stableStart + relativeEnd
		}
		return source[:stableStart] + replaceOnce(t, source[stableStart:stableEnd], old, replacement) + source[stableEnd:]
	}
}

func replaceAdapterOnce(old, replacement string) func(*testing.T, string) string {
	return func(t *testing.T, source string) string {
		t.Helper()
		adapterStart := strings.Index(source, "  scene3d-v1-adapter-proof:\n")
		if adapterStart < 0 {
			t.Fatal("Scene3D adapter proof job is missing")
		}
		adapterEnd := len(source)
		if relativeEnd := strings.Index(source[adapterStart:], "\n  # browser-tests:"); relativeEnd >= 0 {
			adapterEnd = adapterStart + relativeEnd
		}
		return source[:adapterStart] + replaceOnce(t, source[adapterStart:adapterEnd], old, replacement) + source[adapterEnd:]
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

func removeBrowserAggregateContract(t *testing.T, source string) string {
	t.Helper()
	source = replaceOnce(t, source, "      - browser-tests\n", "")
	source = replaceOnce(t, source,
		"          BROWSER_TESTS_RESULT: ${{ needs.browser-tests.result }}\n",
		"",
	)
	previous := "            \"wasm-tests=$WASM_TESTS_RESULT\" \\\n"
	if strings.Contains(source, "            \"scene3d-v1-browser-renderer-proof=$SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT\" \\\n") {
		previous = "            \"scene3d-v1-browser-renderer-proof=$SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT\" \\\n"
	}
	if strings.Contains(source, "            \"scene3d-v1-adapter-proof=$SCENE3D_V1_ADAPTER_PROOF_RESULT\" \\\n") {
		previous = "            \"scene3d-v1-adapter-proof=$SCENE3D_V1_ADAPTER_PROOF_RESULT\" \\\n"
	}
	source = replaceOnce(t, source,
		previous+"            \"browser-tests=$BROWSER_TESTS_RESULT\"\n",
		strings.TrimSuffix(previous, " \\\n")+"\n",
	)
	return replaceOnce(t, source, ", and browser-tests all passed\"\n", " all passed\"\n")
}

func insertAggregateBypassStep(t *testing.T, source string) string {
	t.Helper()
	anchor := "          echo \"" + strings.Join(aggregateNeeds(strings.Contains(source, "  "+stableJobName+":\n"))[:len(aggregateNeeds(strings.Contains(source, "  "+stableJobName+":\n")))-1], ", ") + ", and browser-tests all passed\"\n"
	return replaceOnce(t, source, anchor, anchor+"\n      - name: Mask aggregate result\n        run: echo masked\n")
}

func removeStableAggregateContract(t *testing.T, source string) string {
	t.Helper()
	source = replaceOnce(t, source, "      - scene3d-v1-browser-renderer-proof\n", "")
	source = replaceOnce(t, source,
		"          SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT: ${{ needs.scene3d-v1-browser-renderer-proof.result }}\n",
		"",
	)
	source = replaceOnce(t, source,
		"            \"scene3d-v1-browser-renderer-proof=$SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT\" \\\n",
		"",
	)
	return replaceOnce(t, source,
		"wasm-tests, scene3d-v1-browser-renderer-proof, scene3d-v1-adapter-proof, and browser-tests all passed\"\n",
		"wasm-tests, scene3d-v1-adapter-proof, and browser-tests all passed\"\n",
	)
}

func removeAdapterAggregateContract(t *testing.T, source string) string {
	t.Helper()
	source = replaceOnce(t, source, "      - scene3d-v1-adapter-proof\n", "")
	source = replaceOnce(t, source,
		"          SCENE3D_V1_ADAPTER_PROOF_RESULT: ${{ needs.scene3d-v1-adapter-proof.result }}\n",
		"",
	)
	source = replaceOnce(t, source,
		"            \"scene3d-v1-adapter-proof=$SCENE3D_V1_ADAPTER_PROOF_RESULT\" \\\n",
		"",
	)
	return replaceOnce(t, source,
		"wasm-tests, scene3d-v1-browser-renderer-proof, scene3d-v1-adapter-proof, and browser-tests all passed\"\n",
		"wasm-tests, scene3d-v1-browser-renderer-proof, and browser-tests all passed\"\n",
	)
}

func removeAdapterJobAndAggregate(t *testing.T, source string) string {
	t.Helper()
	source = removeAdapterAggregateContract(t, source)
	start := strings.Index(source, "  # scene3d-v1-adapter-proof:")
	end := strings.Index(source, "\n  # browser-tests:")
	if start < 0 || end < 0 || end <= start {
		t.Fatal("Scene3D adapter proof job block is missing")
	}
	return source[:start] + source[end:]
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
	source = replaceOnce(t, source,
		"          WASM_TESTS_RESULT: ${{ needs.wasm-tests.result }}\n          BROWSER_TESTS_RESULT: ${{ needs.browser-tests.result }}\n",
		"          WASM_TESTS_RESULT: ${{ needs.wasm-tests.result }}\n          SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT: ${{ needs.scene3d-v1-browser-renderer-proof.result }}\n          BROWSER_TESTS_RESULT: ${{ needs.browser-tests.result }}\n",
	)
	source = replaceOnce(t, source,
		"            \"wasm-tests=$WASM_TESTS_RESULT\" \\\n            \"browser-tests=$BROWSER_TESTS_RESULT\"\n",
		"            \"wasm-tests=$WASM_TESTS_RESULT\" \\\n            \"scene3d-v1-browser-renderer-proof=$SCENE3D_V1_BROWSER_RENDERER_PROOF_RESULT\" \\\n            \"browser-tests=$BROWSER_TESTS_RESULT\"\n",
	)
	source = replaceOnce(t, source,
		"wasm-tests, and browser-tests all passed\"\n",
		"wasm-tests, scene3d-v1-browser-renderer-proof, and browser-tests all passed\"\n",
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
