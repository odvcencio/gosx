// Package policy defines the governed search-policy boundary for checkers.
// Arbiter chooses bounded evaluation/search posture; Go still generates legal
// moves, searches them, and validates the selected move.
package policy

import (
	"context"
	_ "embed"
	"fmt"
	"math"
	"strings"
)

//go:embed checkers-policy.arb
var arbiterSource []byte

type Personality string

const (
	JadeCrane   Personality = "jade-crane"
	IronFox     Personality = "iron-fox"
	CedarTurtle Personality = "cedar-turtle"
)

type Facts struct {
	Personality       Personality
	Phase             string
	CampOccupancy     float64
	MobileThermalTier string
	PriorTurnLatency  int
}

type SearchPolicy struct {
	Version           string
	Personality       Personality
	ProgressWeight    float64
	HopWeight         float64
	MobilityDenial    float64
	CampSafety        float64
	Compactness       float64
	RiskTolerance     float64
	BudgetMS          int
	OpeningPreference string
	TieBreak          string
	ExplanationLabel  string
}

type Decision struct {
	Rule   string
	Action string
	Params map[string]any
}

// SourceEvaluator is the deliberately small adapter implemented by the host
// that links m31labs.dev/arbiter. GoSX does not currently depend on that module.
type SourceEvaluator interface {
	Evaluate(context.Context, []byte, map[string]any) (Decision, error)
}

type Resolution struct {
	Policy   SearchPolicy
	Rule     string
	Fallback bool
	Reason   string
}

func Source() []byte { return append([]byte(nil), arbiterSource...) }

func Resolve(ctx context.Context, evaluator SourceEvaluator, facts Facts) Resolution {
	if evaluator == nil {
		return fallback(facts.Personality, "Arbiter evaluator unavailable")
	}
	decision, err := evaluator.Evaluate(ctx, Source(), map[string]any{"game": map[string]any{
		"personality": string(facts.Personality), "phase": facts.Phase, "camp_occupancy": facts.CampOccupancy,
		"mobile_thermal_tier": facts.MobileThermalTier, "prior_turn_latency_ms": facts.PriorTurnLatency,
	}})
	if err != nil {
		return fallback(facts.Personality, "Arbiter evaluation failed: "+err.Error())
	}
	policy, err := policyFromDecision(decision, facts.Personality)
	if err != nil {
		return fallback(facts.Personality, "invalid Arbiter policy: "+err.Error())
	}
	return Resolution{Policy: policy, Rule: decision.Rule}
}

func SafeDefault(personality Personality) SearchPolicy {
	if !validPersonality(personality) {
		personality = CedarTurtle
	}
	return SearchPolicy{Version: "checkers-policy.v1", Personality: personality, ProgressWeight: 1.4, HopWeight: 1.0,
		MobilityDenial: 0.7, CampSafety: 1.2, Compactness: 1.0, RiskTolerance: 0.35, BudgetMS: 80,
		OpeningPreference: "camp-evacuation", TieBreak: "stable", ExplanationLabel: "Safe balanced policy"}
}

func fallback(personality Personality, reason string) Resolution {
	return Resolution{Policy: SafeDefault(personality), Fallback: true, Reason: reason}
}

func policyFromDecision(decision Decision, personality Personality) (SearchPolicy, error) {
	if decision.Action != "SearchPolicy" {
		return SearchPolicy{}, fmt.Errorf("action %q is not SearchPolicy", decision.Action)
	}
	p := SearchPolicy{Version: stringParam(decision.Params, "version"), Personality: personality,
		ProgressWeight: numberParam(decision.Params, "progress_weight"), HopWeight: numberParam(decision.Params, "hop_weight"),
		MobilityDenial: numberParam(decision.Params, "mobility_denial"), CampSafety: numberParam(decision.Params, "camp_safety"),
		Compactness: numberParam(decision.Params, "compactness"), RiskTolerance: numberParam(decision.Params, "risk_tolerance"),
		BudgetMS: int(numberParam(decision.Params, "budget_ms")), OpeningPreference: stringParam(decision.Params, "opening_preference"),
		TieBreak: stringParam(decision.Params, "tie_break"), ExplanationLabel: stringParam(decision.Params, "explanation_label")}
	if err := p.Validate(); err != nil {
		return SearchPolicy{}, err
	}
	return p, nil
}

func (p SearchPolicy) Validate() error {
	if p.Version != "checkers-policy.v1" || !validPersonality(p.Personality) {
		return fmt.Errorf("unknown version or personality")
	}
	for name, value := range map[string]float64{"progress": p.ProgressWeight, "hop": p.HopWeight, "mobility": p.MobilityDenial, "camp": p.CampSafety, "compactness": p.Compactness} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 4 {
			return fmt.Errorf("%s weight %.3f outside [0,4]", name, value)
		}
	}
	if p.RiskTolerance < 0 || p.RiskTolerance > 1 || p.BudgetMS < 10 || p.BudgetMS > 500 {
		return fmt.Errorf("risk or budget outside bounds")
	}
	if !oneOf(p.OpeningPreference, "camp-evacuation", "long-hop", "blocking", "compact") || !oneOf(p.TieBreak, "stable", "forward", "deny", "forgiving") {
		return fmt.Errorf("unknown preference or tie break")
	}
	if len(strings.TrimSpace(p.ExplanationLabel)) == 0 || len(p.ExplanationLabel) > 96 {
		return fmt.Errorf("invalid explanation label")
	}
	return nil
}

func validPersonality(p Personality) bool { return p == JadeCrane || p == IronFox || p == CedarTurtle }
func oneOf(v string, values ...string) bool {
	for _, candidate := range values {
		if v == candidate {
			return true
		}
	}
	return false
}
func stringParam(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return strings.TrimSpace(value)
}
func numberParam(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return math.NaN()
}
