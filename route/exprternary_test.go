package route

import "testing"

func TestSplitTopLevelTernary(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantCond string
		wantCons string
		wantAlt  string
		wantOK   bool
	}{
		{
			name:     "plain",
			src:      `props.Flag ? "yes" : "no"`,
			wantCond: `props.Flag`,
			wantCons: `"yes"`,
			wantAlt:  `"no"`,
			wantOK:   true,
		},
		{
			name:   "no ternary",
			src:    `props.A + props.B`,
			wantOK: false,
		},
		{
			name:   "question mark inside string literal is not a ternary",
			src:    `"what?"`,
			wantOK: false,
		},
		{
			name:     "colon inside string literal does not close early",
			src:      `flag ? "a:b" : "c"`,
			wantCond: `flag`,
			wantCons: `"a:b"`,
			wantAlt:  `"c"`,
			wantOK:   true,
		},
		{
			name:   "parenthesized ternary in condition position is skipped",
			src:    `(a ? b : c) : rest`,
			wantOK: false,
		},
		{
			name:     "right-associative chain keeps the alternative as one unit",
			src:      `a ? b : c ? d : e`,
			wantCond: `a`,
			wantCons: `b`,
			wantAlt:  `c ? d : e`,
			wantOK:   true,
		},
		{
			name:     "parenthesized nested ternary in the consequence",
			src:      `a ? (b ? c : d) : e`,
			wantCond: `a`,
			wantCons: `(b ? c : d)`,
			wantAlt:  `e`,
			wantOK:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond, cons, alt, ok := splitTopLevelTernary(tc.src)
			if ok != tc.wantOK {
				t.Fatalf("splitTopLevelTernary(%q) ok = %v, want %v (cond=%q cons=%q alt=%q)", tc.src, ok, tc.wantOK, cond, cons, alt)
			}
			if !ok {
				return
			}
			if cond != tc.wantCond || cons != tc.wantCons || alt != tc.wantAlt {
				t.Fatalf("splitTopLevelTernary(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.src, cond, cons, alt, tc.wantCond, tc.wantCons, tc.wantAlt)
			}
		})
	}
}

// TestEvalFileExprChainedTernary proves splitTopLevelTernary's
// right-associative matching rule end to end: `a ? b : c ? d : e` must
// select "d" when a is falsy and c is truthy, exercising the alternative's
// own recursive re-split through compiledFileExpr.
func TestEvalFileExprChainedTernary(t *testing.T) {
	resetFileExprCache()
	t.Cleanup(resetFileExprCache)

	env := fileRenderEnv{values: map[string]any{
		"a": false,
		"c": true,
	}}
	const src = `a ? "b" : c ? "d" : "e"`
	if got := evalFileExpr(src, env); got != "d" {
		t.Fatalf("evalFileExpr(%q) = %#v, want \"d\"", src, got)
	}

	env2 := fileRenderEnv{values: map[string]any{
		"a": false,
		"c": false,
	}}
	if got := evalFileExpr(src, env2); got != "e" {
		t.Fatalf("evalFileExpr(%q) with a,c false = %#v, want \"e\"", src, got)
	}
}
