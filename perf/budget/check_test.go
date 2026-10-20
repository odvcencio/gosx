package budget

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func gateOptions(t *testing.T) CheckOptions {
	t.Helper()
	file, profile, coefficients := deriveInputs(t)
	file, err := Derive(file, profile, coefficients)
	if err != nil {
		t.Fatal(err)
	}
	head, base := publicTestReport(t), publicTestReport(t)
	artifact := strings.Repeat("1", 64)
	for _, report := range []*Report{head, base} {
		report.Info.ProfileSHA256, report.Info.CoefficientSHA256 = file.Profile.SHA256, file.Coefficients.SHA256
		report.Info.ToolchainSHA256, report.Info.FixtureSHA256 = file.Toolchain.SHA256, file.Fixtures.SHA256
		report.Info.ArtifactSHA256 = &artifact
		report.Rows[0].Policies = []PolicyResult{{"html-compressed", true}, {"assets-compressed", true}, {inlineExecutablePolicy, true}}
		report.Rows[0].PhaseBytes = PhaseBytes{Critical: 1024, Startup: 1024}
		report.Rows[0].AllocationBytes = file.PageTypes["island"].Allocation.TotalBytes
		report.Rows[0].FrameworkCeilingBytes = file.PageTypes["island"].Allocation.FrameworkBytes
		report.Coverage = ByteCoverage{RoutesExpected: 1, RoutesMeasured: 1, AssetsExpected: int64(len(report.Assets)), AssetsMeasured: int64(len(report.Assets)), Reachability: "known"}
	}
	head.Info.SHA, base.Info.SHA = strings.Repeat("a", 40), strings.Repeat("b", 40)
	head.Info.BaseSHA, head.Info.BaseArtifactSHA256 = base.Info.SHA, artifact
	return CheckOptions{File: file, Profile: profile, Coefficients: coefficients, Head: head, Base: base, Now: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}
}
func hasGateViolation(report *Report, code string) bool {
	for _, v := range report.Violations {
		if v.ReasonCode == code {
			return true
		}
	}
	return false
}
func setGateBytes(report *Report, n, f int64) {
	report.Rows[0].NormalizedBytes, report.Rows[0].FrameworkBytes, report.Rows[0].AppBytes = n, f, n-f
	report.Rows[0].PhaseBytes.Critical, report.Rows[0].PhaseBytes.Startup = n, 0
}
func TestCheckUsesConfiguredCapsAndPreservesInputs(t *testing.T) {
	opts := gateOptions(t)
	before, _ := json.Marshal(opts)
	// Incoming cap and status claims carry no authority.
	opts.Head.Rows[0].AllocationBytes, opts.Head.Rows[0].FrameworkCeilingBytes = 9007199254740991, 9007199254740991
	opts.Head.Rows[0].ModelStatus = "certified"
	supplied, _ := json.Marshal(opts)
	out, err := Check(opts)
	after, _ := json.Marshal(opts)
	page := opts.File.PageTypes["island"]
	if err != nil || !out.Passed || CheckExitCode(out, err) != 0 || out.Mode != "enforce" || out.Rows[0].AllocationBytes != page.Allocation.TotalBytes || out.Rows[0].FrameworkCeilingBytes != page.Allocation.FrameworkBytes || out.Rows[0].ModelStatus != "illustrative" || out.Rows[0].ModelMicros == nil || !reflect.DeepEqual(supplied, after) || reflect.DeepEqual(before, supplied) {
		t.Fatal("cap authority or snapshot changed", out, err)
	}
	if out.Rows[0].BaseBytes == nil || *out.Rows[0].BaseBytes != 2048 || out.Rows[0].DeltaBytes == nil || *out.Rows[0].DeltaBytes != 0 {
		t.Fatal("base projection incorrect")
	}
	*out.Rows[0].ModelMicros = 0
	out.Rows[0].Policies[0].Passed = false
	if !opts.Head.Rows[0].Policies[0].Passed {
		t.Fatal("output aliases input")
	}
}
func TestCheckEachBudgetFailureAndReportOnlyExit(t *testing.T) {
	for _, cause := range []string{"allocation", "framework-share", "after-ready", "after-share", "policy", "missing-policy", "inline-guardrail", "missing-route", "extra-route", "duplicate-row", "backend", "scenario", "unknown-reachability", "asset-coverage", "derivation", "growth-ack"} {
		t.Run(cause, func(t *testing.T) {
			for _, reportOnly := range []bool{false, true} {
				opts := gateOptions(t)
				opts.ReportOnly = reportOnly
				page := opts.File.PageTypes["island"]
				code := cause
				switch cause {
				case "allocation":
					setGateBytes(opts.Head, page.Allocation.TotalBytes+1, 1024)
				case "framework-share":
					setGateBytes(opts.Head, page.Allocation.FrameworkBytes+1, page.Allocation.FrameworkBytes+1)
				case "after-ready":
					opts.Head.Rows[0].PhaseBytes.AfterReady = page.AfterReadyAllocation.TotalBytes + 1
					code = "allocation"
				case "after-share":
					opts.Head.Rows[0].PhaseBytes.AfterReady = page.AfterReadyAllocation.FrameworkBytes + 1
					opts.Head.Assets[0].Brotli = opts.Head.Rows[0].PhaseBytes.AfterReady
					opts.Head.Assets[0].Owner = "framework"
					code = "framework-share"
				case "policy":
					opts.Head.Rows[0].Policies[0].Passed = false
				case "missing-policy":
					opts.Head.Rows[0].Policies = opts.Head.Rows[0].Policies[1:]
					code = "policy"
				case "inline-guardrail":
					opts.Head.Rows[0].Policies[2].Passed = false
					code = "policy"
				case "missing-route":
					opts.Head.Rows = []Row{}
					opts.Head.Coverage.RoutesMeasured = 0
					code = "capability"
				case "extra-route":
					extra := opts.Head.Rows[0]
					extra.RouteTemplate = "/extra/"
					opts.Head.Rows = append(opts.Head.Rows, extra)
					code = "capability"
				case "duplicate-row":
					opts.Head.Rows = append(opts.Head.Rows, opts.Head.Rows[0])
					code = "capability"
				case "backend":
					opts.Head.Rows[0].Backend = "webgpu"
					code = "capability"
				case "scenario":
					opts.Head.Rows[0].Scenario = "soft"
					code = "capability"
				case "unknown-reachability":
					opts.Head.Coverage.Reachability = "unknown"
				case "asset-coverage":
					opts.Head.Coverage.AssetsExpected++
					code = "capability"
				case "derivation":
					page.Allocation.InputSHA256 = strings.Repeat("0", 64)
					opts.File.PageTypes["island"] = page
				case "growth-ack":
					opts.Head.Assets[0].Raw += 4096
				}
				out, err := Check(opts)
				expected := 1
				if reportOnly {
					expected = 0
				}
				if err != nil || out.Passed || !hasGateViolation(out, code) || CheckExitCode(out, err) != expected {
					t.Fatal("budget failure missing or report-only hid it", cause, out, err)
				}
				if cause == "extra-route" && len(out.Rows) != 1 {
					t.Fatal("unregistered route escaped into public rows")
				}
			}
		})
	}
}
func TestCheckToolErrorsNeverBecomeReportOnlySuccess(t *testing.T) {
	for _, cause := range []string{"nil-head", "nil-base", "zero-now", "schema", "unknown-policy", "counts", "profile", "coefficient-bind", "noncanonical", "missing-artifact", "wrong-base", "wrong-artifact", "wrong-epoch", "wrong-pins", "missing-base-row"} {
		opts := gateOptions(t)
		opts.ReportOnly = true
		switch cause {
		case "nil-head":
			opts.Head = nil
		case "nil-base":
			opts.Base = nil
		case "zero-now":
			opts.Now = time.Time{}
		case "schema":
			opts.Head.Rows[0].FrameworkBytes = -1
		case "unknown-policy":
			opts.Head.Rows[0].Policies = append(opts.Head.Rows[0].Policies, PolicyResult{"other-policy", true})
		case "counts":
			opts.Head.Rows[0].PhaseBytes.Startup++
		case "profile":
			opts.Profile.Networks.P75.DownBytesPerSec = 0
		case "coefficient-bind":
			opts.Coefficients.ProfileSHA256 = strings.Repeat("0", 64)
		case "noncanonical":
			opts.Head.Info.Canonical = false
		case "missing-artifact":
			opts.Base.Info.ArtifactSHA256 = nil
		case "wrong-base":
			opts.Head.Info.BaseSHA = strings.Repeat("c", 40)
		case "wrong-artifact":
			opts.Head.Info.BaseArtifactSHA256 = strings.Repeat("2", 64)
		case "wrong-epoch":
			value := strings.Repeat("2", 64)
			opts.Base.Info.EpochSHA256 = &value
		case "wrong-pins":
			opts.Base.Info.ToolchainSHA256 = strings.Repeat("2", 64)
		case "missing-base-row":
			opts.Base.Rows = []Row{}
		}
		out, err := Check(opts)
		var input *InputError
		if !errors.As(err, &input) || CheckExitCode(out, err) != 2 || strings.Contains(err.Error(), "counter") {
			t.Fatal("tool error suppressed or values leaked", cause, out, err)
		}
	}
	if CheckExitCode(nil, nil) != 2 {
		t.Fatal("missing result passed")
	}
}
func TestCheckApprovedTotalAllowanceGoesOnlyToApp(t *testing.T) {
	opts := gateOptions(t)
	page := opts.File.PageTypes["island"]
	e := admissionException()
	e.Extra = 1024
	opts.File.Exceptions = []Exception{e}
	opts.Approvals = testExceptionProofs(t, opts.File.Exceptions)
	setGateBytes(opts.Head, page.Allocation.TotalBytes+1024, page.Allocation.FrameworkBytes)
	opts.Base.Rows = append([]Row{}, opts.Head.Rows...)
	opts.Base.Rows[0].Policies = append([]PolicyResult{}, opts.Head.Rows[0].Policies...)
	out, err := Check(opts)
	if err != nil || !out.Passed || len(out.ExceptionIDs) != 1 || out.Rows[0].FrameworkCeilingBytes != page.Allocation.FrameworkBytes || out.Rows[0].AllocationBytes != page.Allocation.TotalBytes || out.Rows[0].HeadroomBytes != -1024 {
		t.Fatal("app-only allowance failed", out, err)
	}
	setGateBytes(opts.Head, page.Allocation.TotalBytes+1024, page.Allocation.FrameworkBytes+1)
	out, err = Check(opts)
	if err != nil || out.Passed || !hasGateViolation(out, "framework-share") {
		t.Fatal("total allowance waived framework share", out, err)
	}
	opts = gateOptions(t)
	opts.File.Exceptions = []Exception{e}
	out, err = Check(opts)
	if err != nil || out.Passed || out.Mode != "report-only" || !hasGateViolation(out, "exception") || CheckExitCode(out, err) != 0 {
		t.Fatal("missing verifier certified enforcement", out, err)
	}
}
func TestCheckPolicyAllowanceAndGrowthFooterAreSeparate(t *testing.T) {
	opts := gateOptions(t)
	e := admissionException()
	e.Metric, e.Policy, e.Extra, e.ApprovedRole = "policy", "html-compressed", 0, "owner"
	opts.File.Exceptions = []Exception{e}
	opts.Approvals = testExceptionProofs(t, opts.File.Exceptions)
	opts.Head.Rows[0].Policies[0].Passed = false
	out, err := Check(opts)
	if err != nil || !out.Passed {
		t.Fatal("approved named policy was ignored", out, err)
	}
	opts.Head.Rows[0].Policies[2].Passed = false
	out, err = Check(opts)
	if err != nil || out.Passed || !hasGateViolation(out, "policy") {
		t.Fatal("policy exception waived executable guardrail", out, err)
	}
	opts = gateOptions(t)
	opts.Head.Assets[0].Raw += 4096
	id := opts.Head.Assets[0].ID
	footer := "Perf-Budget: scope=asset:" + id + " metric=raw delta=+4096B disposition=permanent issue=#7; because=Support the selected feature."
	opts.Trailers, err = ParseTrailers("Improve output\n\n" + footer)
	if err != nil {
		t.Fatal(err)
	}
	out, err = Check(opts)
	if err != nil || !out.Passed || len(out.Acknowledgments) != 1 {
		t.Fatal("exact growth footer ignored", out, err)
	}
	setGateBytes(opts.Head, opts.File.PageTypes["island"].Allocation.TotalBytes+1, 0)
	out, err = Check(opts)
	if err != nil || out.Passed || !hasGateViolation(out, "allocation") {
		t.Fatal("footer waived a cap", out, err)
	}
}
func TestCheckConservativePhaseOwnerBoundAndIntegerLimits(t *testing.T) {
	row := Row{App: "fixture", PhaseBytes: PhaseBytes{AfterReady: 9007199254740991}}
	assets := []AssetReport{{Owner: "framework", Brotli: 9007199254740991}, {Owner: "framework", Brotli: 9007199254740991}}
	if afterFrameworkBound(row, assets) != row.PhaseBytes.AfterReady {
		t.Fatal("owner bound overflowed")
	}
	opts := gateOptions(t)
	page := opts.File.PageTypes["island"]
	assets = []AssetReport{{ID: "framework/js/core.js", Owner: "framework", Brotli: page.AfterReadyAllocation.FrameworkBytes}}
	row.PhaseBytes.AfterReady = page.AfterReadyAllocation.FrameworkBytes + 1
	if afterFrameworkBound(row, assets) != page.AfterReadyAllocation.FrameworkBytes {
		t.Fatal("owner bound exceeded inventory")
	}
}

func TestCheckMeasuredDesktopModelStaysAdvisory(t *testing.T) {
	opts := gateOptions(t)
	set := &opts.Coefficients.Sets[0]
	prediction := int64(200000)
	set.PredictionErrorPPM = &prediction
	for i := range set.Entries {
		entry := &set.Entries[i]
		entry.Status, entry.Method, entry.NVisits, entry.NBlocks = "measured", "isolated-fit", 30, 2
		value := *entry.Value
		entry.CI95 = [2]*int64{&value, &value}
	}
	derived, err := Derive(opts.File, opts.Profile, opts.Coefficients)
	if err != nil {
		t.Fatal(err)
	}
	opts.File = derived
	out, err := Check(opts)
	if err != nil || !out.Passed || out.Rows[0].ModelStatus != "advisory" || out.Rows[0].ModelMicros == nil {
		t.Fatal("desktop evidence became certified or unavailable", out, err)
	}
}
func TestCheckOnlyRequestedRegisteredBackendIsRequired(t *testing.T) {
	opts := gateOptions(t)
	for _, backend := range []string{"webgpu", "webgl2"} {
		name := "scene3d/js-" + backend
		page := opts.File.PageTypes["island"]
		page.Backend, page.CoefficientSet = backend, "scene-"+backend
		set := opts.Coefficients.Sets[0]
		set.ID, set.Backend = page.CoefficientSet, backend
		opts.Coefficients.Sets = append(opts.Coefficients.Sets, set)
		opts.File.PageTypes[name] = page
		opts.File.Routes[0].PageTypes = append(opts.File.Routes[0].PageTypes, name)
	}
	derived, err := Derive(opts.File, opts.Profile, opts.Coefficients)
	if err != nil {
		t.Fatal(err)
	}
	opts.File = derived
	for _, report := range []*Report{opts.Head, opts.Base} {
		report.Info.Backend = "webgpu"
		row := report.Rows[0]
		row.PageType, row.Backend = "scene3d/js-webgpu", "webgpu"
		report.Rows = append(report.Rows, row)
	}
	out, err := Check(opts)
	if err != nil || !out.Passed || len(out.Rows) != 2 {
		t.Fatal("opposite backend was required", out, err)
	}
	opts.Head.Rows[1].PageType, opts.Head.Rows[1].Backend = "scene3d/js-webgl2", "webgl2"
	out, err = Check(opts)
	if err != nil || out.Passed || !hasGateViolation(out, "capability") {
		t.Fatal("fallback claimed requested-backend coverage", out, err)
	}
}
