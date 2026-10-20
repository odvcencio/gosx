package budget

import (
	"math"
	"sort"

	"m31labs.dev/gosx/internal/assetmeasure"
)

// PhaseCost is a verified physical body. RequestIdentity is private; redirects
// have their own identities and must never be deduplicated with final bodies.
type PhaseCost struct {
	RequestIdentity     string
	Phase, Owner        string
	Sizes               assetmeasure.Sizes
	WireBytes, Requests int64
}
type PhaseTotals struct {
	NormalizedBytes, FrameworkBytes, AppBytes, WireBytes, Requests int64
	Phases                                                         PhaseBytes
}

// SumPhases deduplicates whole bodies and reconciles phase and owner dimensions.
// After-ready and dormant inventory remain disjoint from the cold total. A
// repeated body keeps its earliest phase and explicit framework ownership.
func SumPhases(costs []PhaseCost) (PhaseTotals, error) {
	var result PhaseTotals
	type identity struct{ key, hash string }
	bodies := map[identity]PhaseCost{}
	hashes := map[string]string{}
	for _, cost := range costs {
		if cost.RequestIdentity == "" || phaseRank(cost.Phase) > 3 || (cost.Owner != "framework" && cost.Owner != "app") ||
			!shaPattern.MatchString(cost.Sizes.SHA256) || cost.Sizes.Raw < 0 || cost.Sizes.Gzip < 0 || cost.Sizes.Brotli < 0 || cost.WireBytes < 0 || cost.Requests < 0 {
			return result, measureFailure("invalid-input", "/phaseCosts")
		}
		key := identity{cost.RequestIdentity, cost.Sizes.SHA256}
		if prior, ok := hashes[key.key]; ok && prior != key.hash {
			return result, measureFailure("wrong-fixture", "/phaseCosts")
		}
		hashes[key.key] = key.hash
		if prior, ok := bodies[key]; ok {
			if prior.Sizes.Raw != cost.Sizes.Raw || prior.Sizes.Gzip != cost.Sizes.Gzip || prior.Sizes.Brotli != cost.Sizes.Brotli || prior.WireBytes != cost.WireBytes || prior.Requests != cost.Requests {
				return result, measureFailure("wrong-fixture", "/phaseCosts")
			}
			cost.Phase = earlierPhase(cost.Phase, prior.Phase)
			if prior.Owner == "framework" {
				cost.Owner = "framework"
			}
		}
		bodies[key] = cost
	}
	// The stable order also makes overflow/error locations deterministic.
	keys := make([]identity, 0, len(bodies))
	for key := range bodies {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].key < keys[j].key || keys[i].key == keys[j].key && keys[i].hash < keys[j].hash
	})
	for _, key := range keys {
		cost := bodies[key]
		target := &result.Phases.Dormant
		switch cost.Phase {
		case "critical":
			target = &result.Phases.Critical
		case "startup":
			target = &result.Phases.Startup
		case "after-ready":
			target = &result.Phases.AfterReady
		}
		if !addPhaseValue(target, cost.Sizes.Brotli) {
			return PhaseTotals{}, measureFailure("invalid-input", "/phaseCosts")
		}
		if phaseRank(cost.Phase) > 1 {
			continue
		}
		owner := &result.AppBytes
		if cost.Owner == "framework" {
			owner = &result.FrameworkBytes
		}
		for _, addition := range []struct {
			target *int64
			value  int64
		}{{&result.NormalizedBytes, cost.Sizes.Brotli}, {owner, cost.Sizes.Brotli}, {&result.WireBytes, cost.WireBytes}, {&result.Requests, cost.Requests}} {
			if !addPhaseValue(addition.target, addition.value) {
				return PhaseTotals{}, measureFailure("invalid-input", "/phaseCosts")
			}
		}
	}
	return result, nil
}

func addPhaseValue(target *int64, value int64) bool {
	if value < 0 || *target > math.MaxInt64-value {
		return false
	}
	*target += value
	return true
}
