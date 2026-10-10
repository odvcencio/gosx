package budget

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// renderPublicMarkdown keeps the review tables and complete canonical record
// inseparable. Validation reconstructs this exact text before publication.
func renderPublicMarkdown(report Report) ([]byte, error) {
	data, err := canonicalPublicJSON(report)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("# Performance budget report\n\n")
	fmt.Fprintf(&out, "Mode: `%s`. Passed: `%t`.\n\n", report.Mode, report.Passed)
	base := report.Info.BaseSHA
	if base == "" {
		base = "unavailable"
	}
	fmt.Fprintf(&out, "Head `%s`; base `%s`; profile `%s`; coefficients `%s`; toolchain `%s`; fixtures `%s`; canonical `%t`; runner `%s`; transport `%s`.\n\n", report.Info.SHA, base, report.Info.ProfileSHA256, report.Info.CoefficientSHA256, report.Info.ToolchainSHA256, report.Info.FixtureSHA256, report.Info.Canonical, report.Info.Runner, report.Info.Transport)
	out.WriteString("| App | Route template | Type/scenario | Base normalized B | Head normalized B | Delta B | Actual wire B | N | Framework/F | App remaining | Headroom | Status |\n| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n")
	rows := append([]Row{}, report.Rows...)
	sort.Slice(rows, func(i, j int) bool { return growthRowKey(rows[i]) < growthRowKey(rows[j]) })
	for _, row := range rows {
		fmt.Fprintf(&out, "| %s | %s | %s/%s/%s | %s | %d | %s | %d | %d | %d/%d | %d | %d | %s (%s) |\n", row.App, row.RouteTemplate, row.PageType, row.Scenario, row.Backend, markdownInteger(row.BaseBytes), row.NormalizedBytes, markdownInteger(row.DeltaBytes), row.WireBytes, row.AllocationBytes, row.FrameworkBytes, row.FrameworkCeilingBytes, row.AppRemainingBytes, row.HeadroomBytes, row.Status, row.ReasonCode)
	}
	out.WriteString("\n| Logical asset | Raw/gzip/Brotli base→head | Delta B | Phase | Changed tracked sources |\n| --- | --- | --- | --- | --- |\n")
	assets := append([]AssetReport{}, report.Assets...)
	sort.Slice(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
	for _, asset := range assets {
		baseSizes, delta := "unavailable", "unavailable"
		if asset.BaseSizes != nil {
			baseSizes = markdownSizes(*asset.BaseSizes)
			delta = fmt.Sprintf("%d/%d/%d", asset.Raw-asset.BaseSizes.Raw, asset.Gzip-asset.BaseSizes.Gzip, asset.Brotli-asset.BaseSizes.Brotli)
		}
		sources := append([]string{}, asset.ChangedSources...)
		sort.Strings(sources)
		fmt.Fprintf(&out, "| %s | %s→%d/%d/%d | %s | %s | %s |\n", asset.ID, baseSizes, asset.Raw, asset.Gzip, asset.Brotli, delta, asset.Phase, strings.Join(sources, ", "))
	}
	out.WriteString("\n| App | Route template | Type | Policy | Observed |\n| --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		policies := append([]PolicyResult{}, row.Policies...)
		sort.Slice(policies, func(i, j int) bool { return policies[i].Name < policies[j].Name })
		for _, policy := range policies {
			fmt.Fprintf(&out, "| %s | %s | %s | %s | %t |\n", row.App, row.RouteTemplate, row.PageType, policy.Name, policy.Passed)
		}
	}
	out.WriteString("\n| Exception | Admission |\n| --- | --- |\n")
	ids := append([]string{}, report.ExceptionIDs...)
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(&out, "| %s | admitted |\n", id)
	}
	out.WriteString("\n| Acknowledgment | Scope | Metric | Delta | Issue | Disposition | Expires |\n| --- | --- | --- | ---: | ---: | --- | --- |\n")
	for _, ack := range report.Acknowledgments {
		expires := "none"
		if ack.Expires != nil {
			expires = *ack.Expires
		}
		fmt.Fprintf(&out, "| %s | %s | %s | %d | #%d | %s | %s |\n", ack.Kind, strings.ReplaceAll(ack.Scope, "|", "\\|"), ack.Metric, ack.Delta, ack.Issue, ack.Disposition, expires)
	}
	fmt.Fprintf(&out, "\nCoverage: routes %d/%d; assets %d/%d; reachability `%s`.\n", report.Coverage.RoutesMeasured, report.Coverage.RoutesExpected, report.Coverage.AssetsMeasured, report.Coverage.AssetsExpected, report.Coverage.Reachability)
	for _, violation := range report.Violations {
		fmt.Fprintf(&out, "\nViolation `%s`: %d.\n", violation.ReasonCode, violation.Count)
	}
	out.WriteString("\n```json\n")
	out.Write(data)
	out.WriteString("```\n")
	return out.Bytes(), nil
}
func markdownInteger(value *int64) string {
	if value == nil {
		return "unavailable"
	}
	return strconv.FormatInt(*value, 10)
}
func markdownSizes(sizes SizeTriple) string {
	return fmt.Sprintf("%d/%d/%d", sizes.Raw, sizes.Gzip, sizes.Brotli)
}
