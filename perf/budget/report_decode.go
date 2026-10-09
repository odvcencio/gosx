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

const maxPublicIntegerSample = 1<<53 - 1

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
	value, err := decodeInputValue(data, definition)
	if err != nil {
		return nil, inputReference(err, referenceLabel(definition), "")
	}
	if err := validateRawRecordSamples(value, definition); err != nil {
		return nil, inputReference(err, referenceLabel(definition), "")
	}
	if err := decodeValidatedInput(value, record); err != nil {
		return nil, inputReference(err, referenceLabel(definition), "")
	}
	if err := validateRecordDomains(record); err != nil {
		return nil, inputReference(err, referenceLabel(definition), "")
	}
	return record, nil
}

// The shape has already been validated; samples still contain json.Number.
func validateRawRecordSamples(value any, definition string) error {
	if definition != "SeriesPoint" && definition != "PairReport" {
		return nil
	}
	root := value.(map[string]any)
	check := func(row map[string]any, field, pointer string) error {
		cell := row["cell"].(map[string]any)
		for i, sample := range row[field].([]any) {
			if !validRawSample(sample.(json.Number), cell["unit"].(string), cell["metric"].(string)) {
				return invalidInput(pointer + "/" + field + "/" + strconv.Itoa(i))
			}
		}
		return nil
	}
	if definition == "SeriesPoint" {
		return check(root, "samples", "")
	}
	for i, value := range root["cells"].([]any) {
		row, pointer := value.(map[string]any), "/cells/"+strconv.Itoa(i)
		for _, field := range []string{"baseSamples", "headSamples"} {
			if err := check(row, field, pointer); err != nil {
				return err
			}
		}
	}
	return nil
}

func validRawSample(value json.Number, unit, metric string) bool {
	text := string(value)
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	exponent := 0
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		rawExponent := text[i+1:]
		parsed, err := strconv.Atoi(rawExponent)
		// A nonzero bounded sample cannot need an exponent larger than its
		// entire input. Clamp before arithmetic, without allocating powers.
		if err != nil || parsed > maxInputBytes || parsed < -maxInputBytes {
			parsed = maxInputBytes + 1
			if strings.HasPrefix(rawExponent, "-") {
				parsed = -parsed
			}
		}
		exponent, text = parsed, text[:i]
	}
	fractionDigits := 0
	if i := strings.IndexByte(text, '.'); i >= 0 {
		fractionDigits = len(text) - i - 1
		text = text[:i] + text[i+1:]
	}
	digits := strings.TrimLeft(text, "0")
	if digits == "" {
		return true
	}
	if negative {
		return false
	}
	trimmed := strings.TrimRight(digits, "0")
	scale := exponent - fractionDigits + len(digits) - len(trimmed)
	digits = trimmed
	if unit == "B" || unit == "count" {
		if scale < 0 || len(digits)+scale > 16 {
			return false
		}
		integer, err := strconv.ParseUint(digits+strings.Repeat("0", scale), 10, 64)
		return err == nil && integer <= maxPublicIntegerSample
	}
	if metric == "dropped_frame_rate" {
		return len(digits)+scale <= 0 || digits == "1" && scale == 0
	}
	return true
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
			return math.Trunc(value) == value && value <= maxPublicIntegerSample
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
			for _, entry := range []struct {
				field  string
				values []float64
			}{
				{"baseSamples", row.BaseSamples}, {"headSamples", row.HeadSamples},
			} {
				for j, value := range entry.values {
					if !sample(value, row.Cell) {
						return invalidInput(p + "/" + entry.field + "/" + strconv.Itoa(j))
					}
				}
			}
			for _, entry := range []struct {
				field    string
				interval Interval
			}{
				{"adjustedInterval", row.AdjustedInterval}, {"descriptiveInterval", row.DescriptiveInterval},
			} {
				interval := entry.interval
				if interval.Bounded != (interval.Lower != nil && interval.Upper != nil) || interval.Lower != nil && interval.Upper != nil && *interval.Lower > *interval.Upper {
					return invalidInput(p + "/" + entry.field)
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
		(cell.Unit != "B" && cell.Unit != "count" || value <= maxPublicIntegerSample)
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
