package policy

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fixtureEvaluator struct {
	decisions map[string]Decision
	err       error
	sawSource bool
}

func (f *fixtureEvaluator) Evaluate(_ context.Context, source []byte, facts map[string]any) (Decision, error) {
	f.sawSource = strings.Contains(string(source), "rule JadeCrane") && strings.Contains(string(source), "rule IronFox") && strings.Contains(string(source), "rule CedarTurtle")
	if f.err != nil {
		return Decision{}, f.err
	}
	game := facts["game"].(map[string]any)
	return f.decisions[game["personality"].(string)], nil
}

func TestResolveReturnsBoundedPersonalityPolicies(t *testing.T) {
	evaluator := &fixtureEvaluator{decisions: map[string]Decision{
		"jade-crane":   validDecision("JadeCrane", JadeCrane, 2.4, 2.1, 0.6, 0.8, 0.7, .48, 120, "long-hop", "forward"),
		"iron-fox":     validDecision("IronFox", IronFox, 1.5, 1.1, 2.6, .8, 1.2, .64, 140, "blocking", "deny"),
		"cedar-turtle": validDecision("CedarTurtle", CedarTurtle, 1.1, .8, .4, 2.5, 2.2, .18, 55, "compact", "forgiving"),
	}}
	resolved := map[Personality]SearchPolicy{}
	for _, personality := range []Personality{JadeCrane, IronFox, CedarTurtle} {
		result := Resolve(context.Background(), evaluator, Facts{Personality: personality, Phase: "midgame"})
		if result.Fallback || result.Policy.Validate() != nil {
			t.Fatalf("%s: %+v", personality, result)
		}
		resolved[personality] = result.Policy
	}
	if !evaluator.sawSource {
		t.Fatal("adapter did not receive embedded Arbiter source")
	}
	if resolved[JadeCrane].ProgressWeight <= resolved[IronFox].ProgressWeight || resolved[IronFox].MobilityDenial <= resolved[JadeCrane].MobilityDenial || resolved[CedarTurtle].CampSafety <= resolved[JadeCrane].CampSafety {
		t.Fatalf("personalities are not measurably distinct: %+v", resolved)
	}
}

func TestResolveFallsBackOnMissingFailureAndInvalidEnvelope(t *testing.T) {
	for name, evaluator := range map[string]SourceEvaluator{
		"missing": nil,
		"failure": &fixtureEvaluator{err: errors.New("VM unavailable")},
		"invalid": &fixtureEvaluator{decisions: map[string]Decision{"jade-crane": validDecision("bad", JadeCrane, 99, 1, 1, 1, 1, .5, 100, "long-hop", "forward")}},
	} {
		t.Run(name, func(t *testing.T) {
			result := Resolve(context.Background(), evaluator, Facts{Personality: JadeCrane})
			if !result.Fallback || result.Reason == "" || result.Policy.Validate() != nil {
				t.Fatalf("fallback = %+v", result)
			}
		})
	}
}

func TestSourceDeclaresOnlyPolicyOutcomes(t *testing.T) {
	source := string(Source())
	for _, want := range []string{"feature game", "then SearchPolicy", "checkers-policy.v1"} {
		if !strings.Contains(source, want) {
			t.Fatalf("source missing %q", want)
		}
	}
	for _, forbidden := range []string{"legal_move", "selected_move", "board[", "apply_move"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Arbiter source bypasses Go legality via %q", forbidden)
		}
	}
}

func validDecision(rule string, p Personality, progress, hop, mobility, camp, compact, risk float64, budget int, opening, tie string) Decision {
	return Decision{Rule: rule, Action: "SearchPolicy", Params: map[string]any{
		"version": "checkers-policy.v1", "progress_weight": progress, "hop_weight": hop, "mobility_denial": mobility,
		"camp_safety": camp, "compactness": compact, "risk_tolerance": risk, "budget_ms": budget,
		"opening_preference": opening, "tie_break": tie, "explanation_label": string(p) + " governed policy",
	}}
}
