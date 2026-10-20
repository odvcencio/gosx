package wire

import "testing"

func TestRatchetRouteRaiseReason(t *testing.T) {
	base := Budget{Schema: BudgetSchema, Apps: map[string]AppBudget{"app": {
		"/": {Limits: map[string]int64{MetricTotalWireBytes: 1000, MetricJSWireBytes: 500}, Require: []string{PolicyNoCookie}},
	}}}
	for _, reason := range []string{"interim headroom", "", " \n\t"} {
		t.Run(reason, func(t *testing.T) {
			head := Budget{Schema: BudgetSchema, Apps: map[string]AppBudget{"app": {
				"/": {Limits: map[string]int64{MetricTotalWireBytes: 1100, MetricJSWireBytes: 600}, RaiseReason: reason},
			}}}
			data, err := head.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			head, err = ParseBudget(data)
			if err != nil {
				t.Fatal(err)
			}
			findings := Ratchet(base, head)
			want := 3
			if reason == "interim headroom" {
				want = 1
			}
			if len(findings) != want {
				t.Fatalf("findings = %v, want %d", findings, want)
			}
			if reason == "interim headroom" && findings[0].Subject != PolicyNoCookie {
				t.Fatalf("route reason excused a dropped policy: %v", findings)
			}
		})
	}
}
