package assetpipe

import (
	"sort"
	"strings"

	"m31labs.dev/gosx/assetpipe/variantsel"
)

// SelectVariant picks the best built variant of one asset for a capability set.
//
// # The honesty rule
//
// SelectVariant only ever returns a variant whose State is "built". Plan emits
// variants with an empty State to describe work the pipeline has not done, and
// several of those name block-compressed formats no GoSX build step can produce.
// Returning one of those would put a URI in a response that resolves to a 404.
// The State check is the single line that prevents it, and
// TestSelectVariantRefusesPlannedVariants pins it.
//
// # Ranking
//
// Among the built variants whose required capabilities the set satisfies:
//
//  1. a higher quality tier wins, because the budget gate already removed the
//     tiers this request may not have, and
//  2. at equal quality the smaller file wins, which picks the three-channel
//     form over the four-channel form on a consumer that can upload both.
//
// kind filters by Variant.Kind. An empty kind accepts any.
func SelectVariant(asset Asset, kind string, caps variantsel.Set) (Variant, bool) {
	kind = strings.TrimSpace(strings.ToLower(kind))
	var eligible []Variant
	for _, variant := range asset.Variants {
		if !variant.Exists() {
			continue
		}
		if kind != "" && strings.ToLower(variant.Kind) != kind {
			continue
		}
		if !caps.HasAll(variant.RequiredCapabilities) {
			continue
		}
		eligible = append(eligible, variant)
	}
	if len(eligible) == 0 {
		return Variant{}, false
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		left, right := qualityRank(eligible[i].Quality), qualityRank(eligible[j].Quality)
		if left != right {
			return left > right
		}
		if eligible[i].Bytes != eligible[j].Bytes {
			return eligible[i].Bytes < eligible[j].Bytes
		}
		return eligible[i].URI < eligible[j].URI
	})
	return eligible[0], true
}

// SelectVariantsByKind resolves one variant per kind for a capability set. A
// kind with no satisfiable built variant is absent from the result, which tells
// the caller to keep the source asset.
func SelectVariantsByKind(asset Asset, caps variantsel.Set) map[string]Variant {
	kinds := map[string]struct{}{}
	for _, variant := range asset.Variants {
		if !variant.Exists() {
			continue
		}
		kinds[strings.ToLower(strings.TrimSpace(variant.Kind))] = struct{}{}
	}
	out := map[string]Variant{}
	for kind := range kinds {
		if selected, ok := SelectVariant(asset, kind, caps); ok {
			out[kind] = selected
		}
	}
	return out
}

// qualityRank orders the tier labels the pipeline emits. An unlabelled variant
// ranks below every labelled one, so a tiered set always beats an untiered
// fallback of the same kind.
func qualityRank(quality string) int {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "ultra":
		return 5
	case "high":
		return 4
	case "standard", "medium":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}
