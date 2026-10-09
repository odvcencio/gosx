package budget

import (
	"encoding/json"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

var publicRecordDefinitions = map[string]string{
	"gosx.budget-report/v1": "Report", "gosx.perf-series/v1": "SeriesPoint",
	"gosx.perf-pair/v1": "PairReport", "gosx.perf-field/v1": "FieldSnapshot", "gosx.perf-run/v1": "RunStatus",
}

// DecodeRecord checks a closed public record's shape and numeric domains.
// Catalog membership must be checked before publishing these records.
func DecodeRecord(r io.Reader) (any, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxInputBytes+1))
	if err != nil || len(data) > maxInputBytes {
		return nil, inputReference(invalidInput(""), "public-record", "")
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, inputReference(invalidInput(""), "public-record", "")
	}
	definition, ok := publicRecordDefinitions[header.Schema]
	if !ok {
		return nil, inputReference(invalidInput("/schema"), "public-record", "")
	}
	var record any
	switch definition {
	case "Report":
		record = new(Report)
	case "SeriesPoint":
		record = new(SeriesPoint)
	case "PairReport":
		record = new(PairReport)
	case "FieldSnapshot":
		record = new(FieldSnapshot)
	case "RunStatus":
		record = new(RunStatus)
	}
	if err := decodeInput(data, definition, record); err != nil {
		return nil, err
	}
	if err := validateRecordDomains(record); err != nil {
		return nil, inputReference(err, referenceLabel(definition), "")
	}
	return record, nil
}

func validateRecordDomains(record any) error {
	info := func(i PublicInfo) error {
		if i.ChromeProduct != "" && i.ChromeProduct != "154.0.8034.0" {
			return invalidInput("/info/chromeProduct")
		}
		if i.ChromeSnapshot != "" && i.ChromeSnapshot != "1688711" {
			return invalidInput("/info/chromeSnapshot")
		}
		if i.BenchmarkIndexRounded != nil && *i.BenchmarkIndexRounded%50 != 0 {
			return invalidInput("/info/benchmarkIndexRounded")
		}
		return nil
	}
	cell := func(c Cell, pointer string) error {
		family, backend, known := pageTypeVariant(c.PageType)
		if !known || !validRoute(c.RouteTemplate) || c.Unit != metricUnit(c.Metric) {
			return invalidInput(pointer)
		}
		if backend != "none" && c.Backend != backend || !strings.HasPrefix(family, "scene3d/") && !strings.HasPrefix(family, "game/") && c.Backend != "none" {
			return invalidInput(pointer + "/backend")
		}
		return nil
	}
	sample := func(value float64, c Cell) bool {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return false
		}
		if c.Metric == "dropped_frame_rate" && value > 1 {
			return false
		}
		if c.Unit == "B" || c.Unit == "count" {
			return math.Trunc(value) == value && value <= 9007199254740991
		}
		return true
	}
	switch r := record.(type) {
	case *Report:
		if err := info(r.Info); err != nil {
			return err
		}
		if r.Coverage.Reachability == "known" && (r.Coverage.RoutesMeasured > r.Coverage.RoutesExpected || r.Coverage.AssetsMeasured > r.Coverage.AssetsExpected) {
			return invalidInput("/coverage")
		}
		for i, row := range r.Rows {
			p := "/rows/" + strconv.Itoa(i)
			if !knownPageType(row.PageType) || !validRoute(row.RouteTemplate) || row.FrameworkBytes+row.AppBytes != row.NormalizedBytes {
				return invalidInput(p)
			}
		}
	case *SeriesPoint:
		if err := info(r.Info); err != nil {
			return err
		}
		if err := cell(r.Cell, "/cell"); err != nil {
			return err
		}
		at, err := time.Parse(time.RFC3339, r.At)
		if err != nil || at.Second() != 0 || at.Nanosecond() != 0 {
			return invalidInput("/at")
		}
		_, offset := at.Zone()
		if offset != 0 {
			return invalidInput("/at")
		}
		if int64(len(r.Samples)) != r.N {
			return invalidInput("/n")
		}
		for i, value := range r.Samples {
			if !sample(value, r.Cell) {
				return invalidInput("/samples/" + strconv.Itoa(i))
			}
		}
		if !validPublicSummary(r.Median, r.Cell) || !validPublicSummary(r.P75, r.Cell) || !finiteNonnegative(r.MAD) {
			return invalidInput("/median")
		}
	case *PairReport:
		if err := info(r.Info); err != nil {
			return err
		}
		if int64(len(r.Cells)) > r.FamilySize {
			return invalidInput("/familySize")
		}
		for i, row := range r.Cells {
			p := "/cells/" + strconv.Itoa(i)
			if err := cell(row.Cell, p+"/cell"); err != nil {
				return err
			}
			if (row.PNumerator == nil) != (row.PDenominator == nil) {
				if row.PNumerator == nil {
					return invalidInput(p + "/pNumerator")
				}
				return invalidInput(p + "/pDenominator")
			}
			if row.PNumerator != nil {
				numerator, ok := new(big.Int).SetString(*row.PNumerator, 10)
				if !ok || numerator.Sign() < 0 {
					return invalidInput(p + "/pNumerator")
				}
				denominator, ok := new(big.Int).SetString(*row.PDenominator, 10)
				if !ok || denominator.Sign() <= 0 {
					return invalidInput(p + "/pDenominator")
				}
				if numerator.Cmp(denominator) > 0 {
					return invalidInput(p + "/pNumerator")
				}
			}
			if len(row.BaseSamples) != len(row.HeadSamples) || int64(len(row.BaseSamples)) != row.Pairs {
				return invalidInput(p + "/pairs")
			}
			for j, value := range row.BaseSamples {
				if !sample(value, row.Cell) || !sample(row.HeadSamples[j], row.Cell) {
					return invalidInput(p + "/baseSamples/" + strconv.Itoa(j))
				}
			}
			for _, interval := range []Interval{row.AdjustedInterval, row.DescriptiveInterval} {
				if interval.Bounded != (interval.Lower != nil && interval.Upper != nil) || interval.Lower != nil && interval.Upper != nil && *interval.Lower > *interval.Upper {
					return invalidInput(p + "/adjustedInterval")
				}
			}
		}
	case *FieldSnapshot:
		start, err := time.Parse(time.RFC3339, r.WindowStart)
		end, endErr := time.Parse(time.RFC3339, r.WindowEnd)
		if err != nil || endErr != nil || !end.After(start) {
			return invalidInput("/windowEnd")
		}
		for i, vital := range r.Vitals {
			p := "/vitals/" + strconv.Itoa(i)
			if !validRoute(vital.RouteTemplate) || !validPublicHistogram(vital.Bounds, vital.CumulativeCounts, vital.Count) {
				return invalidInput(p)
			}
		}
		for i, hub := range r.Hubs {
			p := "/hubs/" + strconv.Itoa(i)
			for _, entry := range []struct {
				field, unit string
				histogram   *NumericHistogram
			}{
				{"queueDepth", "count", hub.QueueDepth}, {"rtt", "ms", hub.RTT},
			} {
				histogram := entry.histogram
				if histogram == nil {
					continue
				}
				if histogram.Unit != entry.unit {
					return invalidInput(p + "/" + entry.field + "/unit")
				}
				if !validPublicHistogram(histogram.Bounds, histogram.CumulativeCounts, histogram.Count) {
					return invalidInput(p + "/" + entry.field)
				}
			}
		}
	case *RunStatus:
		if err := info(r.Info); err != nil {
			return err
		}
		if r.CompletedCells > r.PlannedCells || r.Status == "complete" && r.CompletedCells != r.PlannedCells || strings.HasPrefix(r.Status, "skipped-") && r.CompletedCells != 0 {
			return invalidInput("/completedCells")
		}
	default:
		return invalidInput("/schema")
	}
	return nil
}

func finiteNonnegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func validPublicSummary(value float64, cell Cell) bool {
	// Integer observations can have fractional medians and percentiles.
	return finiteNonnegative(value) && (cell.Metric != "dropped_frame_rate" || value <= 1) &&
		(cell.Unit != "B" && cell.Unit != "count" || value <= 9007199254740991)
}

func validPublicHistogram(bounds []float64, counts []int64, count int64) bool {
	if len(counts) != len(bounds)+1 || len(counts) == 0 || counts[len(counts)-1] != count {
		return false
	}
	for i, bound := range bounds {
		if !finiteNonnegative(bound) || i > 0 && bound <= bounds[i-1] {
			return false
		}
	}
	for i, value := range counts {
		if value < 0 || value > count || i > 0 && value < counts[i-1] {
			return false
		}
	}
	return true
}
