package schema

import (
	"encoding/json"
	"testing"
)

func TestTickHealthPortableJSON(t *testing.T) {
	want := TickHealth{Available: true, Samples: 100, P50MS: 1.2, P99MS: 60, MaxMS: 60, Overruns: 2, BudgetMS: 33.3, OverflowSamples: 1}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got TickHealth
	if err := json.Unmarshal(b, &got); err != nil || got != want {
		t.Fatal(got, err)
	}
	var empty TickHealth
	if empty.Available || empty.Samples != 0 {
		t.Fatal(empty)
	}
}
