package schema

import "testing"

func TestTargetLocalOverlayRequiresTargetAndFiniteOffset(t *testing.T) {
	valid := ValidateJSON([]byte(`{"objects":[{"id":"seat","kind":"box"}],"html":[{"id":"bubble","target":"seat","targetOffset":[0,1,0],"mode":"dom","html":"Ready"}]}`), Options{})
	if !valid.Valid {
		t.Fatalf("valid offset: %+v", valid.Diagnostics)
	}
	invalid := ValidateJSON([]byte(`{"html":[{"id":"bubble","targetOffset":[0,1,0],"mode":"dom","html":"Ready"}]}`), Options{})
	if invalid.Valid {
		t.Fatal("target-local offset without a target accepted")
	}
}
