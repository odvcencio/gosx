package schema

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/telemetryerr"
)

func TestFieldsCanonicalJSON(t *testing.T) {
	f := Fields{{Name: "z", Type: FieldEnum, Enum: "<>&\"\\\n\x00é"}, {Name: "a", Type: FieldFloat, Float: 1e20}}
	b, err := f.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":1e20,"z":"<>&\"\\\n\u0000é"}`
	if string(b) != want || !json.Valid(b) {
		t.Fatalf("canonical fields %s", b)
	}
	if f[0].Name != "z" {
		t.Fatal("encoding mutated the view's order")
	}
}

func TestFieldsAlteredViewsStayBounded(t *testing.T) {
	cycle := Fields{{Name: "o", Type: FieldObject}}
	cycle[0].Object = cycle
	cases := []Fields{cycle, {{Name: "n", Type: FieldFloat, Float: math.NaN()}}, {{Name: "a", Type: FieldInts, Ints: make([]int64, 33)}}, {{Name: "n"}, {Name: "n"}}, {{Name: "n", Type: 255}}, {{Name: "private-key"}}, {{Name: "n", Type: FieldEnum, Enum: strings.Repeat("x", 65)}}, {{Name: "n", Type: FieldEnum, Enum: "\xff"}}}
	for _, f := range cases {
		if b, err := f.MarshalJSON(); err == nil || len(b) > 4096 || !(errors.Is(err, telemetryerr.ErrInvalidOptions) || errors.Is(err, telemetryerr.ErrFieldBudget)) {
			t.Fatalf("invalid altered view accepted: %v %v", f, err)
		}
	}
}
