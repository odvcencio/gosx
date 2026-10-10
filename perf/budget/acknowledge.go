package budget

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GrowthRequirement describes one measured increase that crossed its tripwire.
// Byte requirements keep asset and aggregate scopes separate. Timing scope is
// the exact six-component tested cell and Delta is its ceil(HL) in ms.
type GrowthRequirement struct {
	Kind, Scope, Metric string
	Delta               int64
}
type AcknowledgeOptions struct {
	Required []GrowthRequirement
	Trailers Trailers
	File     File
	Assets   []AssetReport
	Now      time.Time
}

// GrowthTripwire is strictly exceeded before an acknowledgment is required.
// Dividing before rounding avoids overflow for large integer allocations.
func GrowthTripwire(allocation int64) (int64, error) {
	if allocation < 0 {
		return 0, ackFailure("/allocation")
	}
	fraction := allocation / 100
	if allocation%100 != 0 {
		fraction++
	}
	return max(1024, fraction), nil
}
func ackFailure(pointer string) error {
	return &InputError{Code: "growth-ack", Reference: "acknowledgments", Pointer: pointer}
}

// RequiredByteGrowth compares admitted canonical reports at single-byte
// precision. Asset allocations are the smallest consuming ceilings supplied
// by the caller; an unmapped compatibility artifact uses the 1,024 B floor.
func RequiredByteGrowth(head, base Report, assetAllocations map[string]int64) ([]GrowthRequirement, error) {
	required := map[string]GrowthRequirement{}
	add := func(scope, metric string, old, current, allocation int64) error {
		if old < 0 || current < 0 {
			return ackFailure("/sizes")
		}
		threshold, err := GrowthTripwire(allocation)
		if err != nil {
			return err
		}
		delta := current - old
		if delta > threshold {
			key := ackKey("budget", scope, metric)
			if delta > required[key].Delta {
				required[key] = GrowthRequirement{"budget", scope, metric, delta}
			}
		}
		return nil
	}
	baseAssets := map[string]AssetReport{}
	for _, asset := range base.Assets {
		if old, ok := baseAssets[asset.ID]; ok && (old.SHA256 != asset.SHA256 || old.Owner != asset.Owner || old.Raw != asset.Raw || old.Gzip != asset.Gzip || old.Brotli != asset.Brotli) {
			return nil, ackFailure("/base/assets")
		}
		baseAssets[asset.ID] = asset
	}
	seen := map[string]AssetReport{}
	for _, asset := range head.Assets {
		if !safePath(asset.ID) {
			return nil, ackFailure("/head/assets")
		}
		if old, ok := seen[asset.ID]; ok && (old.SHA256 != asset.SHA256 || old.Owner != asset.Owner || old.Raw != asset.Raw || old.Gzip != asset.Gzip || old.Brotli != asset.Brotli) {
			return nil, ackFailure("/head/assets")
		}
		seen[asset.ID] = asset
		old := baseAssets[asset.ID]
		for _, metric := range []struct {
			name         string
			old, current int64
		}{{"raw", old.Raw, asset.Raw}, {"gzip", old.Gzip, asset.Gzip}, {"brotli", old.Brotli, asset.Brotli}} {
			if err := add("asset:"+asset.ID, metric.name, metric.old, metric.current, assetAllocations[asset.ID]); err != nil {
				return nil, err
			}
		}
	}
	rows := map[string]Row{}
	for _, row := range base.Rows {
		key := growthRowKey(row)
		if _, ok := rows[key]; ok {
			return nil, ackFailure("/base/rows")
		}
		rows[key] = row
	}
	seenRows := map[string]bool{}
	for _, row := range head.Rows {
		key := growthRowKey(row)
		if seenRows[key] {
			return nil, ackFailure("/head/rows")
		}
		seenRows[key] = true
		old, ok := rows[key]
		if !ok {
			return nil, ackFailure("/base/rows")
		}
		scope := "route:" + row.App + ":" + row.RouteTemplate
		if !validTrailerScope(scope) {
			return nil, ackFailure("/head/rows")
		}
		for _, metric := range []struct {
			name                     string
			old, current, allocation int64
		}{{"totalBytes", old.NormalizedBytes, row.NormalizedBytes, row.AllocationBytes}, {"frameworkBytes", old.FrameworkBytes, row.FrameworkBytes, row.FrameworkCeilingBytes}} {
			if err := add(scope, metric.name, metric.old, metric.current, metric.allocation); err != nil {
				return nil, err
			}
		}
	}
	keys := make([]string, 0, len(required))
	for key := range required {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]GrowthRequirement, 0, len(keys))
	for _, key := range keys {
		out = append(out, required[key])
	}
	return out, nil
}
func growthRowKey(row Row) string {
	return strings.Join([]string{row.App, row.RouteTemplate, row.PageType, row.Scenario, row.Backend}, "|")
}

func TimingGrowth(cell Cell, hl float64) (GrowthRequirement, error) {
	scope := strings.Join([]string{cell.App, cell.RouteTemplate, cell.PageType, cell.Scenario, cell.Backend, cell.Metric}, "|")
	if _, ok := timingCell(scope); !ok || cell.Unit != "ms" || math.IsNaN(hl) || math.IsInf(hl, 0) || hl <= 0 || math.Ceil(hl) > 9007199254740991 {
		return GrowthRequirement{}, ackFailure("/timing")
	}
	return GrowthRequirement{"timing", scope, cell.Metric, int64(math.Ceil(hl))}, nil
}

// ValidateAcknowledgments checks the current required set and registered
// scopes. It returns only public fields and never modifies a cap or exception.
func ValidateAcknowledgments(opts AcknowledgeOptions) ([]Ack, error) {
	out := []Ack{}
	if opts.Now.IsZero() {
		return nil, ackFailure("/now")
	}
	now := opts.Now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	assets := map[string]bool{}
	for _, asset := range opts.Assets {
		assets[asset.ID] = true
	}
	registered := func(ack Ack) bool {
		scope := ack.Scope
		if ack.Kind == "timing" {
			cell, ok := timingCell(scope)
			if !ok || cell.Metric != ack.Metric {
				return false
			}
			for _, route := range opts.File.Routes {
				if route.App == cell.App && route.RouteTemplate == cell.RouteTemplate {
					for _, name := range route.PageTypes {
						if name == cell.PageType {
							return true
						}
					}
				}
			}
			return false
		}
		kind, target, _ := strings.Cut(scope, ":")
		switch kind {
		case "asset":
			return assets[target]
		case "type":
			_, ok := opts.File.PageTypes[target]
			return ok
		case "route":
			for _, route := range opts.File.Routes {
				if route.App+":"+route.RouteTemplate == target {
					return true
				}
			}
		}
		return false
	}
	provided := map[string]Ack{}
	for i, ack := range opts.Trailers.Entries {
		pointer := "/entries/" + strconv.Itoa(i)
		data, _ := json.Marshal(ack)
		var checked Ack
		if decodeInput(data, "Ack", &checked) != nil || ack.Delta <= 0 || ack.ReasonCode != "growth-ack" || !registered(ack) {
			return nil, ackFailure(pointer)
		}
		if ack.Kind == "budget" {
			if !validTrailerScope(ack.Scope) || !budgetMetric(ack.Metric) || (ack.Disposition == "temporary") != (ack.Expires != nil) || ack.Disposition == "timing" {
				return nil, ackFailure(pointer)
			}
		} else if ack.Disposition != "timing" || ack.Expires != nil {
			return nil, ackFailure(pointer)
		}
		if ack.Expires != nil {
			expiry, err := time.Parse("2006-01-02", *ack.Expires)
			if err != nil || !expiry.After(today) || expiry.After(today.AddDate(0, 0, 30)) {
				return nil, ackFailure(pointer + "/expires")
			}
			copy := *ack.Expires
			ack.Expires = &copy
		}
		key := ackKey(ack.Kind, ack.Scope, ack.Metric)
		if _, ok := provided[key]; ok {
			return nil, ackFailure(pointer)
		}
		provided[key] = ack
		out = append(out, ack)
	}
	seen := map[string]bool{}
	for i, requirement := range opts.Required {
		key := ackKey(requirement.Kind, requirement.Scope, requirement.Metric)
		ack, ok := provided[key]
		if requirement.Delta <= 0 || seen[key] || !ok || ack.Delta < requirement.Delta {
			return nil, ackFailure("/required/" + strconv.Itoa(i))
		}
		seen[key] = true
	}
	return out, nil
}
func budgetMetric(metric string) bool {
	switch metric {
	case "raw", "gzip", "brotli", "totalBytes", "frameworkBytes":
		return true
	}
	return false
}
