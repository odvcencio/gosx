package budget

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
)

// Pair evidence can require an acknowledgment; it never increases an allocation.
// Analysis remains separate. Inconclusive or unsupported cells provide no
// conclusive timing evidence, including when a point estimate is positive.
func requiredPairTiming(file File, head, base *Report, pair *PairReport) ([]GrowthRequirement, error) {
	out := []GrowthRequirement{}
	if pair == nil {
		return out, nil
	}
	fail := func(pointer string) error {
		return &InputError{Code: "wrong-fixture", Reference: "pair", Pointer: pointer}
	}
	data, err := json.Marshal(pair)
	if err != nil {
		return nil, fail("")
	}
	record, err := DecodeRecord(bytes.NewReader(data))
	if err != nil {
		return nil, inputReference(err, "pair", "")
	}
	checked, ok := record.(*PairReport)
	if !ok {
		return nil, fail("/schema")
	}
	i, h, b := checked.Info, head.Info, base.Info
	if checked.Status == "complete" && int64(len(checked.Cells)) != checked.FamilySize {
		return nil, fail("/cells")
	}
	if !i.Canonical || !i.Headless || !i.Muted || i.SHA != h.SHA || i.BaseSHA != b.SHA || i.ProfileSHA256 != h.ProfileSHA256 || i.CoefficientSHA256 != h.CoefficientSHA256 || i.ToolchainSHA256 != h.ToolchainSHA256 || i.FixtureSHA256 != h.FixtureSHA256 || !reflect.DeepEqual(i.EpochSHA256, h.EpochSHA256) || i.Transport != h.Transport || !reflect.DeepEqual(i.ArtifactSHA256, h.ArtifactSHA256) || i.BaseArtifactSHA256 != h.BaseArtifactSHA256 {
		return nil, fail("/info")
	}
	registered := expectedCheckRows(file, h.Backend)
	seen := map[string]bool{}
	for index, row := range checked.Cells {
		pointer := "/cells/" + strconv.Itoa(index)
		key := growthRowKey(Row{App: row.Cell.App, RouteTemplate: row.Cell.RouteTemplate, PageType: row.Cell.PageType, Scenario: row.Cell.Scenario, Backend: row.Cell.Backend})
		scope := key + "|" + row.Cell.Metric
		if !registered[key] || seen[scope] {
			return nil, fail(pointer + "/cell")
		}
		seen[scope] = true
		if row.Decision != "regression" {
			continue
		}
		declaredLook := false
		for _, look := range checked.Looks {
			declaredLook = declaredLook || row.StoppingLook == look
		}
		if !declaredLook {
			return nil, fail(pointer + "/stoppingLook")
		}
		if checked.Status != "complete" || row.Pairs == 0 || row.HL == nil || *row.HL <= row.Threshold || !row.AdjustedInterval.Bounded || row.AdjustedInterval.Lower == nil || row.AdjustedInterval.Upper == nil || *row.AdjustedInterval.Lower <= row.Threshold || row.PNumerator == nil || row.PDenominator == nil {
			return nil, fail(pointer)
		}
		// Perf-Timing uses milliseconds. Memory/count/ratio evidence is retained
		// by the pair report and cannot be relabeled as a duration acknowledgment.
		if row.Cell.Unit != "ms" {
			continue
		}
		growth, err := TimingGrowth(row.Cell, *row.HL)
		if err != nil {
			return nil, inputReference(err, "pair", pointer)
		}
		out = append(out, growth)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope < out[j].Scope })
	return out, nil
}
