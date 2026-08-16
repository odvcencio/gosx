package strictcomponent

import (
	"strings"
	"testing"
)

func TestValidateServerExpressionAcceptsV041Surface(t *testing.T) {
	for _, source := range []string{
		`"text"`,
		"0",
		"42",
		"1.5",
		".5",
		"true",
		"false",
		"props.Title",
		"(props.Title)",
		"(props).Title",
	} {
		t.Run(source, func(t *testing.T) {
			if err := ValidateServerExpression(source); err != nil {
				t.Fatalf("ValidateServerExpression(%q) = %v, want nil", source, err)
			}
		})
	}
}

func TestValidateServerExpressionAcceptsConcatChains(t *testing.T) {
	for _, source := range []string{
		`"a" + props.X`,
		`props.X + "a"`,
		`"a" + props.X + "b"`,
		`("a") + (props.X)`,
		`"tone-" + props.Tone`,
		`"Remove " + props.Name + " from roster"`,
	} {
		t.Run(source, func(t *testing.T) {
			if err := ValidateServerExpression(source); err != nil {
				t.Fatalf("ValidateServerExpression(%q) = %v, want nil", source, err)
			}
		})
	}
}

func TestValidateServerExpressionRejectsWithExactMessages(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   string
	}{
		{
			source: "props.A + props.B",
			want:   "strict concatenation requires at least one string literal operand",
		},
		{
			source: `"a" + "b"`,
			want:   "strict concatenation requires at least one props field operand; fold literal-only chains by hand",
		},
		{
			source: "1 + props.X",
			want:   "\"+\" operand `1` is not a string literal; the strict server renderer does not perform numeric addition",
		},
		{
			source: `props.X - "a"`,
			want:   `binary operator "-" is not supported by the strict server renderer; only string concatenation with "+" is renderable`,
		},
		{
			source: `"a" + props.X()`,
			want:   "\"+\" operand `props.X()` is not renderable; strict concatenation accepts string literals and props field selectors only",
		},
		{
			source: `"a" + x`,
			want:   "\"+\" operand `x` is not renderable; strict concatenation accepts string literals and props field selectors only",
		},
		{
			source: `"a" + (props.Count + 1)`,
			want:   "\"+\" operand `props.Count + 1` is not renderable; strict concatenation accepts string literals and props field selectors only",
		},
		{
			source: "props.A.B.C.D",
			want:   "selector must be one field directly on props; nested selector chains cannot preserve Go nil-pointer behavior",
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			err := ValidateServerExpression(tc.source)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ValidateServerExpression(%q) = %v, want %q", tc.source, err, tc.want)
			}
		})
	}
}

func TestValidateServerExpressionRejectsNonAddOperatorsWithNewMessage(t *testing.T) {
	for _, tc := range []struct {
		source string
		op     string
	}{
		{"props.A * props.B", "*"},
		{"props.A % props.B", "%"},
		{"props.A && props.B", "&&"},
		{"props.A > props.B", ">"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			err := ValidateServerExpression(tc.source)
			want := `binary operator "` + tc.op + `" is not supported by the strict server renderer; only string concatenation with "+" is renderable`
			if err == nil || err.Error() != want {
				t.Fatalf("ValidateServerExpression(%q) = %v, want %q", tc.source, err, want)
			}
		})
	}
}

func TestServerPropFieldUnaffectedByConcatSupport(t *testing.T) {
	for _, tc := range []struct {
		source    string
		wantField string
		wantOK    bool
	}{
		{"props.Title", "Title", true},
		{"(props.Title)", "Title", true},
		{"(props).Title", "Title", true},
		{`"a" + props.X`, "", false},
		{"props", "", false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			field, ok := ServerPropField(tc.source)
			if field != tc.wantField || ok != tc.wantOK {
				t.Fatalf("ServerPropField(%q) = (%q, %v), want (%q, %v)", tc.source, field, ok, tc.wantField, tc.wantOK)
			}
		})
	}
}

func TestServerConcatPropFields(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   []string
		wantOK bool
	}{
		{`"a" + props.X`, []string{"X"}, true},
		{`props.X + "a"`, []string{"X"}, true},
		{`"a" + props.X + "b"`, []string{"X"}, true},
		{`"Remove " + props.Name + " from roster"`, []string{"Name"}, true},
		{`props.A + props.B`, []string{"A", "B"}, true}, // fields extracted even though the syntax validator rejects the chain overall
		{"props.Title", nil, false},
		{`"a" + "b"`, nil, true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			fields, ok := ServerConcatPropFields(tc.source)
			if ok != tc.wantOK || !equalStrings(fields, tc.want) {
				t.Fatalf("ServerConcatPropFields(%q) = (%v, %v), want (%v, %v)", tc.source, fields, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestValidateServerCondExpressionAcceptsBoolShapes(t *testing.T) {
	for _, tc := range []struct {
		source      string
		wantField   string
		wantNegated bool
	}{
		{"props.Ready", "Ready", false},
		{"props.Ready == false", "Ready", true},
		{"(props.Ready) == (false)", "Ready", true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			field, negated, err := ValidateServerCondExpression(tc.source)
			if err != nil {
				t.Fatalf("ValidateServerCondExpression(%q) = %v, want nil error", tc.source, err)
			}
			if field != tc.wantField || negated != tc.wantNegated {
				t.Fatalf("ValidateServerCondExpression(%q) = (%q, %v), want (%q, %v)", tc.source, field, negated, tc.wantField, tc.wantNegated)
			}
		})
	}
}

func TestValidateServerCondExpressionRejectsWithExactMessages(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   string
	}{
		{
			source: "props.Ready == true",
			want:   `comparison "== true" is not supported in strict cond; write the field bare`,
		},
		{
			source: "props.Ready != false",
			want:   `strict cond must be a bool props field or a bool props field compared with "== false"; got "props.Ready != false"`,
		},
		{
			source: "!props.Ready",
			want:   `strict cond must be a bool props field or a bool props field compared with "== false"; got "!props.Ready"`,
		},
		{
			source: "props.A == props.B",
			want:   `strict cond must be a bool props field or a bool props field compared with "== false"; got "props.A == props.B"`,
		},
		{
			source: "props.Score > 0",
			want:   `strict cond must be a bool props field or a bool props field compared with "== false"; got "props.Score > 0"`,
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			_, _, err := ValidateServerCondExpression(tc.source)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ValidateServerCondExpression(%q) = %v, want %q", tc.source, err, tc.want)
			}
		})
	}
}

func TestServerExpressionPropFields(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   []string
	}{
		{"props.Title", []string{"Title"}},
		{`"a" + props.X`, []string{"X"}},
		{`"a" + props.X + props.Y`, []string{"X", "Y"}},
		{"props.Ready == false", []string{"Ready"}},
		{"props.A == props.B", []string{"A", "B"}},
		{"props.A.B", []string{"A"}}, // the syntax validator rejects the chain overall; the root field still gets tracked for the explicit-supply rule
		{`"a" + "b"`, nil},
		{"props.X()", []string{"X"}}, // shape-invalid overall, but the read still matters for tracking
	} {
		t.Run(tc.source, func(t *testing.T) {
			got := ServerExpressionPropFields(tc.source)
			if !equalStrings(got, tc.want) {
				t.Fatalf("ServerExpressionPropFields(%q) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
}

func TestValidateServerCondExpressionRejectsInvalidGo(t *testing.T) {
	_, _, err := ValidateServerCondExpression("props.(")
	if err == nil || !strings.Contains(err.Error(), "invalid Go expression") {
		t.Fatalf("ValidateServerCondExpression(malformed) = %v", err)
	}
}
