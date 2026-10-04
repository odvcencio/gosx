package route

import (
	"m31labs.dev/gosx/ir"
	"math"
	"strings"
	"testing"
)

func TestFileRenderFiltersURLsAndDataDrivenNames(t *testing.T) {
	for _, name := range []string{"href", "src", "action", "formaction", "xlink:href", "poster"} {
		for _, attr := range []ir.Attr{{Name: name, Kind: ir.AttrStatic, Value: "javascript:example"}, {Name: name, Kind: ir.AttrExpr, Expr: "value"}} {
			var b strings.Builder
			renderFileAttr(&b, attr, fileRenderEnv{values: map[string]any{"value": "javascript:example"}}, "")
			if b.String() != " "+name+`="#"` {
				t.Fatalf("unsafe URL: %s", b.String())
			}
		}
	}
	attrs := map[string]any{"onload": "example", "STYLE": "color:red", "x y": "value", "data-ok": "ok", "href": "javascript:example"}
	var b strings.Builder
	renderFileSpreadAttrs(&b, attrs, "")
	if b.String() != ` data-ok="ok" href="#"` {
		t.Fatalf("unsafe spread: %s", b.String())
	}
	got := defaultRenderedComponent("Unknown", attrs, "")
	if got != `<div data-gosx-component="Unknown" data-ok="ok" href="#"></div>` {
		t.Fatalf("unsafe fallback: %s", got)
	}
	r := &fileProgramRenderer{}
	b.Reset()
	r.renderResolvedAttrs(&b, "a", []RenderAttr{{Name: "href", Value: "javascript:example"}})
	if b.String() != ` href="#"` {
		t.Fatalf("unsafe profile: %s", b.String())
	}
	resolved := resolveFileAttrs([]ir.Attr{{Kind: ir.AttrSpread, Expr: "attrs"}}, fileRenderEnv{values: map[string]any{"attrs": attrs}}, "")
	if len(resolved) != 2 {
		t.Fatalf("profile received unsafe spread: %#v", resolved)
	}
}

func TestEqualityPreservesIdentifiersAndRequiresExactNumericStrings(t *testing.T) {
	type identifier int64
	for _, tc := range []struct {
		left, right any
		want        bool
	}{
		{int64(0), "abc", false}, {int64(0), "", false}, {0, false, false},
		{int64(9007199254740993), int64(9007199254740992), false},
		{int64(9007199254740993), "9007199254740993", true},
		{int64(9007199254740993), "9007199254740992", false},
		{uint64(math.MaxUint64), "18446744073709551615", true},
		{int64(-1), uint64(math.MaxUint64), false}, {identifier(42), uint64(42), true},
		{float64(1.5), "1.5", true}, {42, "42oops", false}, {0, "1e-99999999", false},
		{math.NaN(), math.NaN(), false},
	} {
		if got := equalValues(tc.left, tc.right); got != tc.want {
			t.Errorf("%v == %v: %v want %v", tc.left, tc.right, got, tc.want)
		}
	}
	if got := evalFileExpr("9007199254740993 == 9007199254740992", fileRenderEnv{}); got != false {
		t.Fatalf("rounded integer literals: %v", got)
	}
}
