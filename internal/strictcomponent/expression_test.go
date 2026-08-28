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
		`"a" + props.A.B`,
		`"a" + props.A.B.C`,
	} {
		t.Run(source, func(t *testing.T) {
			if err := ValidateServerExpression(source); err != nil {
				t.Fatalf("ValidateServerExpression(%q) = %v, want nil", source, err)
			}
		})
	}
}

// TestValidateServerExpressionAcceptsNestedSelectors covers the widened
// selector rule this change adds: a props-rooted field chain of any depth is
// a valid shape. The lowerer (ir/lower.go), not this schema-blind validator,
// enforces the three-hop resolution cap and every same-file-struct type
// rule — see resolveStrictSelectorPath and its tests in strict_component_test.go.
func TestValidateServerExpressionAcceptsNestedSelectors(t *testing.T) {
	for _, source := range []string{
		"props.A.B",
		"props.A.B.C",
		"(props.A).B",
		"(props.A.B)",
		// Deeper than the lowerer's three-hop cap: still a valid *shape*.
		// TestCompileStrictServerRejectsSelectorTooDeep (root package)
		// proves the lowerer rejects it with full component context.
		"props.A.B.C.D",
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
			source: "x.Field",
			want:   "selector must be a field chain rooted at props, with every step a plain field access; anything else cannot preserve Go nil-pointer behavior",
		},
		{
			source: "props.A[0].B",
			want:   "selector must be a field chain rooted at props, with every step a plain field access; anything else cannot preserve Go nil-pointer behavior",
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
		// ServerPropField is ServerPropPath's length-1 case: a nested path
		// is a valid ServerPropPath result but not a valid ServerPropField
		// result.
		{"props.A.B", "", false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			field, ok := ServerPropField(tc.source)
			if field != tc.wantField || ok != tc.wantOK {
				t.Fatalf("ServerPropField(%q) = (%q, %v), want (%q, %v)", tc.source, field, ok, tc.wantField, tc.wantOK)
			}
		})
	}
}

func TestServerPropPath(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   []string
		wantOK bool
	}{
		{"props.Title", []string{"Title"}, true},
		{"(props.Title)", []string{"Title"}, true},
		{"(props).Title", []string{"Title"}, true},
		{"props.A.B", []string{"A", "B"}, true},
		{"props.A.B.C", []string{"A", "B", "C"}, true},
		{"(props.A).B.C", []string{"A", "B", "C"}, true},
		{"props.A.B.C.D", []string{"A", "B", "C", "D"}, true},
		{"props", nil, false},
		{"x.Field", nil, false},
		{`"a" + props.X`, nil, false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			path, ok := ServerPropPath(tc.source)
			if ok != tc.wantOK || !equalStrings(path, tc.want) {
				t.Fatalf("ServerPropPath(%q) = (%v, %v), want (%v, %v)", tc.source, path, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestServerConcatPropPaths(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   [][]string
		wantOK bool
	}{
		{`"a" + props.X`, [][]string{{"X"}}, true},
		{`props.X + "a"`, [][]string{{"X"}}, true},
		{`"a" + props.X + "b"`, [][]string{{"X"}}, true},
		{`"Remove " + props.Name + " from roster"`, [][]string{{"Name"}}, true},
		{`props.A + props.B`, [][]string{{"A"}, {"B"}}, true}, // paths extracted even though the syntax validator rejects the chain overall
		{`"a" + props.A.B`, [][]string{{"A", "B"}}, true},
		{"props.Title", nil, false},
		{`"a" + "b"`, nil, true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			paths, ok := ServerConcatPropPaths(tc.source)
			if ok != tc.wantOK || !equalPathLists(paths, tc.want) {
				t.Fatalf("ServerConcatPropPaths(%q) = (%v, %v), want (%v, %v)", tc.source, paths, ok, tc.want, tc.wantOK)
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

func equalPathLists(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalStrings(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestValidateServerCondExpressionAcceptsBoolShapes(t *testing.T) {
	for _, tc := range []struct {
		source      string
		wantPath    []string
		wantNegated bool
	}{
		{"props.Ready", []string{"Ready"}, false},
		{"props.Ready == false", []string{"Ready"}, true},
		{"(props.Ready) == (false)", []string{"Ready"}, true},
		{"props.A.Ready", []string{"A", "Ready"}, false},
		{"props.A.Ready == false", []string{"A", "Ready"}, true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			path, negated, err := ValidateServerCondExpression(tc.source)
			if err != nil {
				t.Fatalf("ValidateServerCondExpression(%q) = %v, want nil error", tc.source, err)
			}
			if !equalStrings(path, tc.wantPath) || negated != tc.wantNegated {
				t.Fatalf("ValidateServerCondExpression(%q) = (%v, %v), want (%v, %v)", tc.source, path, negated, tc.wantPath, tc.wantNegated)
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

func TestServerExpressionPropPaths(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   [][]string
	}{
		{"props.Title", [][]string{{"Title"}}},
		{`"a" + props.X`, [][]string{{"X"}}},
		{`"a" + props.X + props.Y`, [][]string{{"X"}, {"Y"}}},
		{"props.Ready == false", [][]string{{"Ready"}}},
		{"props.A == props.B", [][]string{{"A"}, {"B"}}},
		// A nested chain registers as one maximal path, not also a spurious
		// bare read of its own non-scalar inner selector (props.A alone,
		// whose type is a struct, would otherwise fail the renderer-scalar
		// gate even though the author never wrote a bare props.A anywhere).
		{"props.A.B", [][]string{{"A", "B"}}},
		{"props.A.B.C", [][]string{{"A", "B", "C"}}},
		{`"a" + props.A.B`, [][]string{{"A", "B"}}},
		{`"a" + "b"`, nil},
		{"props.X()", [][]string{{"X"}}}, // shape-invalid overall, but the read still matters for tracking
	} {
		t.Run(tc.source, func(t *testing.T) {
			got := ServerExpressionPropPaths(tc.source)
			if !equalPathLists(got, tc.want) {
				t.Fatalf("ServerExpressionPropPaths(%q) = %v, want %v", tc.source, got, tc.want)
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

// rowScope is the Scope a strict <Each of={...} as="row"> pushes for its
// children (design spec section 2.2); rowIndexScope adds an index="i"
// binding.
var rowScope = Scope{Items: []string{"row"}}
var rowIndexScope = Scope{Items: []string{"row"}, Indices: []string{"i"}}

// TestValidateServerExpressionScopeAcceptsBindingSelectors covers #182's
// widened selector rule: a binding-rooted selector, its parenthesized
// forms, a concat operand, a cond selector, and a bare index reference are
// all accepted with an active Scope.
func TestValidateServerExpressionScopeAcceptsBindingSelectors(t *testing.T) {
	for _, tc := range []struct {
		source string
		scope  Scope
	}{
		{"row.Label", rowScope},
		{"(row).Label", rowScope},
		{`"x-" + row.Tone`, rowScope},
		{"row.Scored", rowScope},
		{"row.Stat.Label", rowScope},
		{"i", rowIndexScope},
	} {
		t.Run(tc.source, func(t *testing.T) {
			if err := ValidateServerExpressionScope(tc.source, tc.scope); err != nil {
				t.Fatalf("ValidateServerExpressionScope(%q, %v) = %v, want nil", tc.source, tc.scope, err)
			}
		})
	}
}

// TestValidateServerExpressionScopeRejectsWithExactMessages covers the
// reject half: a bare binding, an unbound name even though a binding is
// active, a binding used with an empty scope (falls back to the ordinary
// identifier message), and a mixed chain that fails the widened rule for
// an unrelated reason.
func TestValidateServerExpressionScopeRejectsWithExactMessages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		scope  Scope
		want   string
	}{
		{
			name:   "bare item binding",
			source: "row",
			scope:  rowScope,
			want:   `bare row is not supported; select a field on row`,
		},
		{
			name:   "unbound selector root",
			source: "rowKey.X",
			scope:  rowScope,
			want:   `selector must be a field chain rooted at props, with every step a plain field access; anything else cannot preserve Go nil-pointer behavior`,
		},
		{
			name:   "binding used with empty scope",
			source: "item.X",
			scope:  Scope{},
			want:   `selector must be a field chain rooted at props, with every step a plain field access; anything else cannot preserve Go nil-pointer behavior`,
		},
		{
			name:   "row.Label with empty scope",
			source: "row.Label",
			scope:  Scope{},
			want:   `selector must be a field chain rooted at props, with every step a plain field access; anything else cannot preserve Go nil-pointer behavior`,
		},
		{
			name:   "index binding used as a selector root",
			source: "i.Field",
			scope:  rowIndexScope,
			// Syntactically admitted here (an index name is a valid scope
			// root candidate — see Scope's doc comment); the lowerer's
			// walkStrictHops rejects it with schema context, since an
			// index is never a struct. This validator has no schema, so it
			// accepts the shape.
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateServerExpressionScope(tc.source, tc.scope)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ValidateServerExpressionScope(%q, %v) = %v, want nil", tc.source, tc.scope, err)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ValidateServerExpressionScope(%q, %v) = %v, want %q", tc.source, tc.scope, err, tc.want)
			}
		})
	}
}

// TestValidateServerExpressionUnaffectedByScopeGeneralization is the
// signature regression WP-4 requires: the empty-scope wrapper behaves
// byte-identically to the pre-#182/#184 signature for every existing case,
// re-run here through the new Scope-aware entry point at Scope{}.
func TestValidateServerExpressionUnaffectedByScopeGeneralization(t *testing.T) {
	for _, source := range []string{
		`"text"`, "props.Title", "props.A.B.C", `"a" + props.X`,
	} {
		t.Run(source, func(t *testing.T) {
			want := ValidateServerExpression(source)
			got := ValidateServerExpressionScope(source, Scope{})
			if (want == nil) != (got == nil) {
				t.Fatalf("scope wrapper diverged: ValidateServerExpression=%v ValidateServerExpressionScope=%v", want, got)
			}
			if want != nil && want.Error() != got.Error() {
				t.Fatalf("scope wrapper message diverged: %q vs %q", want.Error(), got.Error())
			}
		})
	}
}

func TestServerSelectorPathReportsRoot(t *testing.T) {
	for _, tc := range []struct {
		source   string
		scope    Scope
		wantRoot string
		wantPath []string
		wantOK   bool
	}{
		{"props.Tone", Scope{}, "props", []string{"Tone"}, true},
		{"row.Label", rowScope, "row", []string{"Label"}, true},
		{"row.Stat.Label", rowScope, "row", []string{"Stat", "Label"}, true},
		{"other.Label", rowScope, "", nil, false},
		{"row.Label", Scope{}, "", nil, false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			root, path, ok := ServerSelectorPath(tc.source, tc.scope)
			if ok != tc.wantOK || root != tc.wantRoot || !equalStrings(path, tc.wantPath) {
				t.Fatalf("ServerSelectorPath(%q, %v) = (%q, %v, %v), want (%q, %v, %v)", tc.source, tc.scope, root, path, ok, tc.wantRoot, tc.wantPath, tc.wantOK)
			}
		})
	}
}

func TestServerExpressionRootedPathsFindsBindingReads(t *testing.T) {
	for _, tc := range []struct {
		source string
		scope  Scope
		want   []RootedPath
	}{
		{"row.Label", rowScope, []RootedPath{{Root: "row", Path: []string{"Label"}}}},
		{`"x-" + row.Tone + props.Suffix`, rowScope, []RootedPath{{Root: "row", Path: []string{"Tone"}}, {Root: "props", Path: []string{"Suffix"}}}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			got := ServerExpressionRootedPaths(tc.source, tc.scope)
			if len(got) != len(tc.want) {
				t.Fatalf("ServerExpressionRootedPaths(%q, %v) = %v, want %v", tc.source, tc.scope, got, tc.want)
			}
			for i := range got {
				if got[i].Root != tc.want[i].Root || !equalStrings(got[i].Path, tc.want[i].Path) {
					t.Fatalf("ServerExpressionRootedPaths(%q, %v)[%d] = %v, want %v", tc.source, tc.scope, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestValidateServerCondExpressionScopeAcceptsBindingRoot(t *testing.T) {
	root, path, negated, err := ValidateServerCondExpressionScope("row.Scored == false", rowScope)
	if err != nil {
		t.Fatalf("ValidateServerCondExpressionScope: %v", err)
	}
	if root != "row" || !equalStrings(path, []string{"Scored"}) || !negated {
		t.Fatalf("ValidateServerCondExpressionScope = (%q, %v, %v), want (row, [Scored], true)", root, path, negated)
	}
}
