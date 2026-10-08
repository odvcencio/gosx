package budget

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"time"
)

type CheckOptions struct {
	Approvals    []TrustedApproval
	File         File
	Profile      Profile
	Coefficients Coefficients
	Head, Base   *Report
	Trailers     Trailers
	Now          time.Time
	ReportOnly   bool
}

// Check evaluates canonical measurements against immutable configured caps.
// Budget failures are retained in the returned report, including report-only
// mode. Invalid inputs and provenance return typed errors in either mode.
func Check(opts CheckOptions) (*Report, error) {
	if opts.Now.IsZero() {
		return nil, checkInput("invalid-input", "check", "/now")
	}
	for _, input := range []struct {
		name  string
		value any
	}{{"Budget", opts.File}, {"Profile", opts.Profile}, {"Coefficients", opts.Coefficients}} {
		if err := validateTyped(input.name, input.value); err != nil {
			return nil, inputReference(err, referenceLabel(input.name), "")
		}
	}
	if err := opts.Profile.validate(); err != nil {
		return nil, inputReference(err, "profile", "")
	}
	if err := opts.Coefficients.validate(); err != nil {
		return nil, inputReference(err, "coefficients", "")
	}
	if opts.Coefficients.ProfileSHA256 != opts.File.Profile.SHA256 || opts.Coefficients.Reference != opts.Profile.Reference {
		return nil, checkInput("invalid-input", "coefficients", "/profileSHA256")
	}
	if err := opts.File.validate(opts.Profile, opts.Coefficients); err != nil {
		return nil, inputReference(err, "budget", "")
	}
	head, err := checkReportInput(opts.Head, "head")
	if err != nil {
		return nil, err
	}
	base, err := checkReportInput(opts.Base, "base")
	if err != nil {
		return nil, err
	}
	if err := checkProvenance(opts.File, head.Info, base.Info); err != nil {
		return nil, err
	}
	exceptions, err := EvaluateExceptions(ExceptionOptions{File: opts.File, Approvals: opts.Approvals, Now: opts.Now})
	if err != nil {
		return nil, err
	}
	out := head
	out.Mode, out.Passed = "enforce", true
	if opts.ReportOnly || exceptions.ApprovalUnavailable {
		out.Mode = "report-only"
	}
	out.ExceptionIDs, out.Acknowledgments, out.Violations = []string{}, []Ack{}, []CountReason{}
	failures := map[string]int64{}
	fail := func(code string) { failures[code]++; out.Passed = false }
	if VerifyDerivation(opts.File, opts.Profile, opts.Coefficients) != nil {
		fail("derivation")
	}
	for _, decision := range exceptions.Decisions {
		if !decision.Admitted {
			fail("exception")
		} else {
			out.ExceptionIDs = append(out.ExceptionIDs, decision.ID)
		}
	}
	expected := expectedCheckRows(opts.File, head.Info.Backend)
	seen, routes := map[string]bool{}, map[string]bool{}
	rows := []Row{}
	for _, row := range head.Rows {
		key := growthRowKey(row)
		if _, registered := expected[key]; !registered || seen[key] {
			fail("capability")
			continue
		}
		seen[key], routes[row.App+"|"+row.RouteTemplate] = true, true
		page := opts.File.PageTypes[row.PageType]
		allowance := exceptions.Allowance(row.App, row.RouteTemplate, row.PageType)
		row.Status, row.ReasonCode = "pass", "ok"
		row.AllocationBytes, row.FrameworkCeilingBytes = page.Allocation.TotalBytes, page.Allocation.FrameworkBytes
		row.HeadroomBytes = row.AllocationBytes - row.NormalizedBytes
		row.AppRemainingBytes = max(0, row.AllocationBytes-row.FrameworkCeilingBytes-row.AppBytes)
		rowFail := func(code string) {
			fail(code)
			if row.Status != "fail" {
				row.Status, row.ReasonCode = "fail", code
			}
		}
		if row.NormalizedBytes > row.AllocationBytes+allowance.TotalBytes || row.PhaseBytes.AfterReady > page.AfterReadyAllocation.TotalBytes {
			rowFail("allocation")
		}
		if row.FrameworkBytes > row.FrameworkCeilingBytes {
			rowFail("framework-share")
		}
		// Public phase totals do not carry route-specific after-ready ownership.
		// Bound that ownership conservatively by the whole inventory; never infer
		// free framework bytes from a missing owner breakdown.
		if afterFrameworkBound(row, head.Assets) > page.AfterReadyAllocation.FrameworkBytes {
			rowFail("framework-share")
		}
		for range requiredPolicyFailures(row.PageType, page, row.Policies, allowance.Policies) {
			rowFail("policy")
		}
		model, modelErr := newModel(opts.File, opts.Profile, opts.Coefficients, row.PageType, false)
		row.ModelStatus, row.ModelMicros = "unknown", nil
		if modelErr == nil && row.NormalizedBytes <= maxSearchBytes {
			predicted, costErr := model.cost(row.NormalizedBytes)
			if costErr == nil {
				for _, goal := range page.Goals {
					if goal.Metric == page.PrimaryMetric {
						full, _ := new(big.Rat).SetString(goal.Max.String())
						predicted.Add(predicted, new(big.Rat).Sub(full.Mul(full, ratio(1000, 1)), model.window))
					}
				}
				elapsed, err := ceilMicros(predicted)
				if err != nil {
					return nil, err
				}
				row.ModelMicros = &elapsed
				row.ModelStatus = "illustrative"
				if model.status == "proxy-measured" {
					row.ModelStatus = "advisory"
				}
			}
		}
		rows = append(rows, row)
	}
	for key := range expected {
		if !seen[key] {
			fail("capability")
		}
	}
	out.Rows = rows
	sort.Slice(out.Rows, func(i, j int) bool { return growthRowKey(out.Rows[i]) < growthRowKey(out.Rows[j]) })
	expectedRoutes := map[string]bool{}
	for _, rule := range opts.File.Routes {
		expectedRoutes[rule.App+"|"+rule.RouteTemplate] = true
	}
	if head.Coverage.RoutesExpected != int64(len(expectedRoutes)) || head.Coverage.RoutesMeasured != int64(len(routes)) || head.Coverage.AssetsExpected != head.Coverage.AssetsMeasured || head.Coverage.AssetsMeasured != int64(len(head.Assets)) {
		fail("capability")
	}
	out.Coverage.RoutesExpected, out.Coverage.RoutesMeasured = int64(len(expectedRoutes)), int64(len(routes))
	if head.Coverage.Reachability != "known" {
		fail("unknown-reachability")
	}
	baseRows := map[string]Row{}
	for _, row := range base.Rows {
		key := growthRowKey(row)
		if _, duplicate := baseRows[key]; duplicate {
			return nil, checkInput("invalid-input", "base", "/rows")
		}
		baseRows[key] = row
	}
	for i := range out.Rows {
		old, ok := baseRows[growthRowKey(out.Rows[i])]
		if !ok {
			return nil, checkInput("wrong-fixture", "base", "/rows")
		}
		out.Rows[i].BaseBytes = new(int64)
		*out.Rows[i].BaseBytes = old.NormalizedBytes
		out.Rows[i].DeltaBytes = new(int64)
		*out.Rows[i].DeltaBytes = out.Rows[i].NormalizedBytes - old.NormalizedBytes
	}
	allocations := checkAssetAllocations(opts.File, head.Assets)
	required, err := RequiredByteGrowth(*out, *base, allocations)
	if err != nil {
		return nil, inputReference(err, "growth", "")
	}
	acknowledgments, err := ValidateAcknowledgments(AcknowledgeOptions{Required: required, Trailers: opts.Trailers, File: opts.File, Assets: head.Assets, Now: opts.Now})
	if err != nil {
		var input *InputError
		if !errors.As(err, &input) || input.Code != "growth-ack" {
			return nil, inputReference(err, "acknowledgments", "")
		}
		fail("growth-ack")
	} else {
		out.Acknowledgments = acknowledgments
	}
	codes := make([]string, 0, len(failures))
	for code := range failures {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		out.Violations = append(out.Violations, CountReason{code, failures[code]})
	}
	sort.Strings(out.ExceptionIDs)
	return out, nil
}
func checkInput(code, reference, pointer string) error {
	return &InputError{Code: code, Reference: reference, Pointer: pointer}
}
func checkReportInput(report *Report, reference string) (*Report, error) {
	if report == nil {
		return nil, checkInput("invalid-input", reference, "")
	}
	data, err := json.Marshal(report)
	if err != nil {
		return nil, checkInput("invalid-input", reference, "")
	}
	decoded, err := DecodeRecord(bytes.NewReader(data))
	if err != nil {
		return nil, inputReference(err, reference, "")
	}
	out, ok := decoded.(*Report)
	if !ok {
		return nil, checkInput("invalid-input", reference, "/schema")
	}
	for i, row := range out.Rows {
		for j, policy := range row.Policies {
			if !knownPolicyResult(policy.Name) {
				return nil, checkInput("invalid-input", reference, "/rows/"+strconv.Itoa(i)+"/policies/"+strconv.Itoa(j)+"/name")
			}
		}
		if row.PhaseBytes.Critical+row.PhaseBytes.Startup != row.NormalizedBytes {
			return nil, checkInput("invalid-input", reference, pointerChild("/rows", intStringCheck(i))+"/phaseBytes")
		}
	}
	return out, nil
}
func intStringCheck(i int) string { return strconv.Itoa(i) }
func checkProvenance(file File, head, base PublicInfo) error {
	for _, input := range []struct {
		name string
		info PublicInfo
	}{{"head", head}, {"base", base}} {
		if !input.info.Canonical {
			return checkInput("noncanonical", input.name, "/info/canonical")
		}
		if input.info.ProfileSHA256 != file.Profile.SHA256 || input.info.CoefficientSHA256 != file.Coefficients.SHA256 || input.info.ToolchainSHA256 != file.Toolchain.SHA256 || input.info.FixtureSHA256 != file.Fixtures.SHA256 {
			return checkInput("wrong-fixture", input.name, "/info")
		}
		if input.info.ArtifactSHA256 == nil {
			return checkInput("noncanonical", input.name, "/info/artifactSHA256")
		}
	}
	if head.BaseSHA != base.SHA {
		return checkInput("wrong-fixture", "base", "/info/sha")
	}
	if head.BaseArtifactSHA256 != *base.ArtifactSHA256 || !reflect.DeepEqual(head.EpochSHA256, base.EpochSHA256) || head.Transport != base.Transport {
		return checkInput("wrong-fixture", "base", "/info")
	}
	return nil
}
func expectedCheckRows(file File, backend string) map[string]bool {
	expected := map[string]bool{}
	for _, rule := range file.Routes {
		for _, name := range rule.PageTypes {
			page := file.PageTypes[name]
			if backend != "" && backend != "none" && page.Backend != "none" && page.Backend != backend {
				continue
			}
			row := Row{App: rule.App, RouteTemplate: rule.RouteTemplate, PageType: name, Scenario: rule.Scenario, Backend: page.Backend}
			expected[growthRowKey(row)] = true
		}
	}
	return expected
}
func checkAssetAllocations(file File, assets []AssetReport) map[string]int64 {
	allocations := map[string]int64{}
	for _, asset := range assets {
		if asset.Phase == "dormant" {
			continue
		}
		for _, rule := range file.Routes {
			if asset.Owner == "app" && asset.App != rule.App {
				continue
			}
			for _, name := range rule.PageTypes {
				page := file.PageTypes[name]
				cap := page.Allocation.FrameworkBytes
				if asset.Owner == "app" {
					cap = page.Allocation.TotalBytes - page.Allocation.FrameworkBytes
				}
				old, ok := allocations[asset.ID]
				if !ok || cap < old {
					allocations[asset.ID] = cap
				}
			}
		}
	}
	return allocations
}
func afterFrameworkBound(row Row, assets []AssetReport) int64 {
	var total int64
	for _, asset := range assets {
		if asset.Owner == "framework" || asset.App == row.App && asset.Kind == "html" {
			if asset.Brotli >= row.PhaseBytes.AfterReady-total {
				return row.PhaseBytes.AfterReady
			}
			total += asset.Brotli
		}
	}
	return min(total, row.PhaseBytes.AfterReady)
}
func ceilMicros(value *big.Rat) (int64, error) {
	integer, remainder := new(big.Int), new(big.Int)
	integer.QuoRem(value.Num(), value.Denom(), remainder)
	if remainder.Sign() > 0 {
		integer.Add(integer, big.NewInt(1))
	}
	if !integer.IsInt64() {
		return 0, checkInput("invalid-input", "model", "/modelMicros")
	}
	return integer.Int64(), nil
}

// CheckExitCode keeps report-only budget failures separate from tool errors.
func CheckExitCode(report *Report, err error) int {
	if err != nil || report == nil {
		return 2
	}
	if report.Passed || report.Mode == "report-only" {
		return 0
	}
	return 1
}
